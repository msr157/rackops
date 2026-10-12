package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

func TestCloudTransferQuoteAndRecalculate(t *testing.T) {
	for _, provider := range []string{"aws", "azure", "gcp"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			defer a.Store.Close()
			org := a.Store.DefaultOrgID()
			region := "us-east-1"
			if provider == "azure" {
				region = "eastus"
			} else if provider == "gcp" {
				region = "us-central1"
			}

			planID := "plan-" + provider
			now := time.Now().UTC()
			plan := &CloudReviewPlan{
				ID:           planID,
				OrgID:        org,
				ConnectionID: "conn-" + provider,
				Provider:     provider,
				AccountID:    "acc-" + provider,
				Region:       region,
				Architecture: "amd64",
				CreatedAt:    now,
				ExpiresAt:    now.Add(30 * time.Minute),
				Status:       "blocked",
				CanApprove:   false,
				Inspection: cloud.MachineInspection{
					Provider: provider,
					Region:   region,
				},
				ComputeQuote: &cloud.ComputeQuote{
					Monthly: 50.00,
				},
				DiskQuote: &cloud.DiskQuote{
					Monthly: 10.00,
				},
			}
			a.Store.Mutate(func() {
				a.Store.CloudConnections = map[string]*CloudConnection{
					"conn-" + provider: {
						ID:        "conn-" + provider,
						OrgID:     org,
						Provider:  provider,
						Region:    region,
						AccountID: "acc-" + provider,
					},
				}
				a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{
					planID: plan,
				}
			})

			headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
			reqBody := `{"transferGb": 100}`
			resp := do(a, "POST", "/api/cloud-plans/"+planID+"/transfer-estimate", reqBody, headers)
			if resp.Code != 200 {
				t.Fatalf("transfer estimate failed: %d %s", resp.Code, resp.Body)
			}

			var quote cloud.TransferQuote
			if err := json.Unmarshal(resp.Body.Bytes(), &quote); err != nil {
				t.Fatal(err)
			}
			if quote.Provider != provider || quote.TransferGB != 100 || quote.Monthly <= 0 {
				t.Fatalf("unexpected transfer quote: %+v", quote)
			}

			// Verify that CloudReviewPlan now has total MonthlyCost calculated and CanApprove is true
			a.Store.View(func() {
				updated := a.Store.CloudReviewPlans[planID]
				if updated.MonthlyCost == nil || *updated.MonthlyCost <= 0 {
					t.Fatalf("monthly cost not calculated: %+v", updated)
				}
				if !updated.CanApprove || updated.Status != "ready_for_approval" {
					t.Fatalf("plan not ready for approval: %+v", updated)
				}
				expectedTotal := 50.00 + 10.00 + quote.Monthly
				if *updated.MonthlyCost != expectedTotal {
					t.Fatalf("monthly total mismatch: expected %f, got %f", expectedTotal, *updated.MonthlyCost)
				}
			})
		})
	}
}

func TestMigrationPlanApprovalAndValidation(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	defer a.Store.Close()
	org := a.Store.DefaultOrgID()
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}

	sourceNodeID := "node-source-1"
	projectID := "proj-1"
	serviceID := "svc-app-1"
	now := time.Now().UTC()

	a.Store.Mutate(func() {
		a.Store.Nodes = map[string]*types.Node{
			sourceNodeID: {ID: sourceNodeID, OrgID: org, OS: "linux", Arch: "amd64", Status: types.NodeOnline, LastSeen: now},
		}
		a.Store.Projects = map[string]*types.Project{
			projectID: {ID: projectID, OrgID: org, Name: "Test Proj"},
		}
		a.Store.Services = map[string]*types.Service{
			serviceID: {ID: serviceID, NodeID: sourceNodeID, ProjectID: projectID, Name: "web", ContainerID: "cid-1", UpdatedAt: now},
		}
		a.Store.CloudConnections = map[string]*CloudConnection{
			"conn-aws": {ID: "conn-aws", OrgID: org, Provider: "aws", Region: "us-east-1", AccountID: "acc-1"},
		}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{
			"review-aws": {
				ID:           "review-aws",
				OrgID:        org,
				ConnectionID: "conn-aws",
				Provider:     "aws",
				AccountID:    "acc-1",
				Region:       "us-east-1",
				Architecture: "amd64",
				CreatedAt:    now,
				ExpiresAt:    now.Add(30 * time.Minute),
				Status:       "blocked",
				CanApprove:   false,
			},
		}
	})

	// 1. Create migration draft
	draftReq := map[string]any{
		"id":            "plan-draft-1",
		"cloudReviewId": "review-aws",
		"serviceIds":    []string{serviceID},
	}
	rawDraft, _ := json.Marshal(draftReq)
	rDraft := do(a, "POST", "/api/migration-plans", string(rawDraft), headers)
	if rDraft.Code != 201 {
		t.Fatalf("create draft failed: %d %s", rDraft.Code, rDraft.Body)
	}

	// 2. Attempt to approve while review is blocked -> should fail with 409
	rApproveFail := do(a, "POST", "/api/migration-plans/plan-draft-1/approve", "", headers)
	if rApproveFail.Code != 409 {
		t.Fatalf("expected 409 when approving unready plan, got %d", rApproveFail.Code)
	}

	// 3. Mark review ready with all quotes
	monthly := 69.00
	a.Store.Mutate(func() {
		rev := a.Store.CloudReviewPlans["review-aws"]
		rev.CanApprove = true
		rev.Status = "ready_for_approval"
		rev.MonthlyCost = &monthly
		rev.ComputeQuote = &cloud.ComputeQuote{Monthly: 50.00}
		rev.DiskQuote = &cloud.DiskQuote{Monthly: 10.00}
		rev.TransferQuote = &cloud.TransferQuote{Monthly: 9.00}
	})

	// 4. Now approve the plan -> should succeed with 201
	rApprove := do(a, "POST", "/api/migration-plans/plan-draft-1/approve", "", headers)
	if rApprove.Code != 201 {
		t.Fatalf("approve failed: %d %s", rApprove.Code, rApprove.Body)
	}

	var approveResp map[string]any
	if err := json.Unmarshal(rApprove.Body.Bytes(), &approveResp); err != nil {
		t.Fatal(err)
	}
	jobID, ok := approveResp["jobId"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected jobId in response: %+v", approveResp)
	}

	// Verify plan is approved and job is in Provision stage
	a.Store.View(func() {
		plan := a.Store.MigrationPlans["plan-draft-1"]
		if plan.ApprovedAt.IsZero() || plan.ApprovedBy == "" {
			t.Fatal("plan approval fields not set")
		}
		jobRec := a.Store.MigrationJobs[jobID]
		if jobRec == nil || jobRec.Job.Stage != migration.Provision || jobRec.Job.Status != "ready" {
			t.Fatalf("unexpected job state: %+v", jobRec)
		}
	})

	// 5. Duplicate approval should fail with 409
	rApproveDup := do(a, "POST", "/api/migration-plans/plan-draft-1/approve", "", headers)
	if rApproveDup.Code != 409 {
		t.Fatalf("expected 409 for duplicate approval, got %d", rApproveDup.Code)
	}
}

func TestEndToEndMigrationWorkflow(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	defer a.Store.Close()
	org := a.Store.DefaultOrgID()
	now := time.Now().UTC()

	sourceNodeID := "node-src-local"
	destNodeID := "node-dest-cloud"
	projectID := "proj-main"
	serviceID := "svc-postgres"
	dbName := "testdb"
	nowFixed := now

	a.Store.Mutate(func() {
		a.Store.Nodes = map[string]*types.Node{
			sourceNodeID: {ID: sourceNodeID, OrgID: org, OS: "linux", Arch: "amd64", Status: types.NodeOnline, LastSeen: nowFixed},
			destNodeID:   {ID: destNodeID, OrgID: org, OS: "linux", Arch: "amd64", Status: types.NodeOnline, LastSeen: nowFixed},
		}
		a.Store.Projects = map[string]*types.Project{
			projectID: {ID: projectID, OrgID: org, Name: "Main"},
		}
		a.Store.Services = map[string]*types.Service{
			serviceID: {
				ID:          serviceID,
				NodeID:      sourceNodeID,
				ProjectID:   projectID,
				Name:        "db",
				Kind:        types.KindDatabase,
				Image:       "postgres:16",
				ContainerID: "cid-postgres-source",
				UpdatedAt:   nowFixed,
			},
		}
		monthly := 75.00
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{
			"rev-cloud": {
				ID:           "rev-cloud",
				OrgID:        org,
				Provider:     "aws",
				Region:       "us-east-1",
				Architecture: "amd64",
				CanApprove:   true,
				MonthlyCost:  &monthly,
				CreatedAt:    nowFixed,
				ExpiresAt:    nowFixed.Add(time.Hour),
			},
		}
		a.Store.MigrationPlans = map[string]*MigrationPlan{
			"plan-e2e": {
				ID:                "plan-e2e",
				OrgID:             org,
				CloudReviewID:     "rev-cloud",
				DestinationNodeID: destNodeID, // Pre-assign destination node for local scenario
				Workloads: []types.Service{
					{
						ID:          serviceID,
						NodeID:      sourceNodeID,
						ProjectID:   projectID,
						Name:        "db",
						Kind:        types.KindDatabase,
						Image:       "postgres:16",
						ContainerID: "cid-postgres-source",
						UpdatedAt:   nowFixed,
					},
				},
				CreatedAt:  nowFixed,
				ExpiresAt:  nowFixed.Add(time.Hour),
				ApprovedAt: nowFixed,
				ApprovedBy: "owner@test.local",
			},
		}
	})

	ctx := context.Background()
	jobID := "mig-e2e-1"
	job, err := a.createMigrationJob(ctx, org, jobID, "plan-e2e")
	if err != nil {
		t.Fatalf("createMigrationJob failed: %v", err)
	}
	if job.Stage != migration.Provision {
		t.Fatalf("expected Provision stage, got %s", job.Stage)
	}

	exec := a.migrationExecutor(org, jobID)

	// Step 1: Provision -> Advances to Quiesce
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("advance Provision failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Quiesce {
			t.Fatalf("expected Quiesce stage, got %s", j.Stage)
		}
	})

	// Step 2: Quiesce
	// First Advance dispatches Quiesce task
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Quiesce failed: %v", err)
	}
	// Mark quiesce task completed
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationQuiesce) + ":" + serviceID + ":"
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
	})
	// Second Advance reconciles Quiesce and advances to Backup
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Quiesce failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Backup {
			t.Fatalf("expected Backup stage, got %s", j.Stage)
		}
	})

	// Step 3: Backup
	// First Advance dispatches Backup task and prepares artifact
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Backup failed: %v", err)
	}
	var artID string
	a.Store.View(func() {
		for _, art := range a.Store.BackupArtifacts {
			if art.JobID == jobID {
				artID = art.ID
			}
		}
	})
	if artID == "" {
		t.Fatal("backup artifact was not created")
	}
	// Complete backup task and update artifact state to ready
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationBackup) + ":" + serviceID + ":" + artID
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
		a.Store.BackupArtifacts[artID].State = "ready"
	})
	// Second Advance reconciles Backup and advances to Transfer
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Backup failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Transfer {
			t.Fatalf("expected Transfer stage, got %s", j.Stage)
		}
	})

	// Step 4: Transfer
	// First Advance dispatches Transfer tasks
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Transfer failed: %v", err)
	}
	// Mark transfer complete on destination
	a.Store.Mutate(func() {
		a.Store.BackupArtifacts[artID].State = "destination_received"
		a.Store.BackupArtifacts[artID].DestinationNodeID = destNodeID
	})
	// Reconcile Transfer and advance to Restore
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Transfer failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Restore {
			t.Fatalf("expected Restore stage, got %s", j.Stage)
		}
	})

	// Step 5: Restore
	// First Advance dispatches Restore task to destination node
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Restore failed: %v", err)
	}
	// Mark restore task complete
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationRestore) + ":" + serviceID + ":" + artID
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
		a.Store.BackupArtifacts[artID].State = "restored"
	})
	// Reconcile Restore and advance to Validate
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Restore failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Validate {
			t.Fatalf("expected Validate stage, got %s", j.Stage)
		}
	})

	// Step 6: Validate
	// First Advance dispatches Validate task
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Validate failed: %v", err)
	}
	// Mark validate task complete
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationValidate) + ":" + serviceID + ":" + artID
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
		a.Store.BackupArtifacts[artID].State = "validated"
	})
	// Reconcile Validate and advance to Cutover
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Validate failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Cutover {
			t.Fatalf("expected Cutover stage, got %s", j.Stage)
		}
	})

	// Step 7: Cutover
	// First Advance dispatches Cutover task and updates service placement
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch Cutover failed: %v", err)
	}
	// Verify service node was updated to destination node
	a.Store.View(func() {
		svc := a.Store.Services[serviceID]
		if svc.NodeID != destNodeID {
			t.Fatalf("service NodeID was not updated to destination node: %s", svc.NodeID)
		}
	})
	// Complete cutover task
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationCutover) + ":" + serviceID + ":"
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
	})
	// Reconcile Cutover and advance to EnableWrites
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile Cutover failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.EnableWrites {
			t.Fatalf("expected EnableWrites stage, got %s", j.Stage)
		}
	})

	// Step 8: EnableWrites
	// First Advance persists DestinationWritesPossible = true before issuing command
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("dispatch EnableWrites failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if !j.DestinationWritesPossible {
			t.Fatal("DestinationWritesPossible boundary was not persisted")
		}
	})
	// Mark enable writes task complete
	a.Store.Mutate(func() {
		taskID := jobID + ":" + string(types.TaskMigrationEnableWrites) + ":" + serviceID + ":"
		a.Store.TaskOperations[taskID] = &TaskOperation{
			State:   "completed",
			Outcome: "succeeded",
		}
	})
	// Reconcile EnableWrites and advance to Complete
	if err := a.advanceMigration(ctx, org, jobID, exec); err != nil {
		t.Fatalf("reconcile EnableWrites failed: %v", err)
	}
	a.Store.View(func() {
		j := a.Store.MigrationJobs[jobID].Job
		if j.Stage != migration.Complete || j.Status != "completed" {
			t.Fatalf("expected completed job, got stage %s status %s", j.Stage, j.Status)
		}
	})
	_ = dbName
}
