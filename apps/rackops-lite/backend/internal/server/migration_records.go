package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

// MigrationPlan freezes the application configuration inside encrypted state.
// Never serialize this type directly into API responses: workloads contain secrets.
type MigrationPlan struct {
	ID            string          `json:"id"`
	OrgID         string          `json:"orgId"`
	CloudReviewID string          `json:"cloudReviewId"`
	Workloads     []types.Service `json:"workloads"`
	CreatedAt     time.Time       `json:"createdAt"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	ApprovedAt    time.Time       `json:"approvedAt"`
	ApprovedBy    string          `json:"approvedBy"`
}

type MigrationJobRecord struct {
	Job       migration.Job `json:"job"`
	Version   uint64        `json:"version"`
	CreatedAt time.Time     `json:"createdAt"`
}

// BackupArtifact binds both digests to a job and owned source; SealedKey is
// separately encrypted and never returned by progress/list APIs.
type BackupArtifact struct {
	DestinationNodeID string                     `json:"destinationNodeId,omitempty"`
	State             string                     `json:"state"`
	ID                string                     `json:"id"`
	OrgID             string                     `json:"orgId"`
	JobID             string                     `json:"jobId"`
	ServiceID         string                     `json:"serviceId"`
	SourceNodeID      string                     `json:"sourceNodeId"`
	Kind              string                     `json:"kind"`
	Plaintext         migration.ArtifactDigest   `json:"plaintext"`
	Transfer          migration.TransferManifest `json:"transfer"`
	SealedKey         []byte                     `json:"sealedKey"`
	CreatedAt         time.Time                  `json:"createdAt"`
}

func cloneMigrationJob(job migration.Job) migration.Job {
	copy := job
	copy.Receipts = make(map[migration.Stage]string, len(job.Receipts))
	for stage, receipt := range job.Receipts {
		copy.Receipts[stage] = receipt
	}
	return copy
}

// createMigrationJob reserves all source workloads atomically after approval.
// It remains internal: the public approval/launch flow is not implemented yet.
func (a *API) createMigrationJob(ctx context.Context, orgID, id, planID string) (migration.Job, error) {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return migration.Job{}, err
	}
	if a.Cluster != nil || orgID == "" || id == "" || len(id) > 128 {
		return migration.Job{}, errors.New("invalid migration job")
	}
	var result migration.Job
	var failure error
	created := false
	a.Store.Mutate(func() {
		if old := a.Store.MigrationJobs[id]; old != nil {
			if old.Job.OrgID != orgID || old.Job.PlanID != planID {
				failure = errors.New("migration ID belongs to another plan")
				return
			}
			result = cloneMigrationJob(old.Job)
			return
		}
		plan := a.Store.MigrationPlans[planID]
		if plan == nil || plan.OrgID != orgID || plan.ApprovedAt.IsZero() || plan.ApprovedBy == "" || time.Now().After(plan.ExpiresAt) || len(plan.Workloads) == 0 {
			failure = errors.New("approved migration plan unavailable or expired")
			return
		}
		selected := map[string]bool{}
		for _, snapshot := range plan.Workloads {
			service := a.Store.Services[snapshot.ID]
			if service == nil || service.NodeID != snapshot.NodeID || !service.UpdatedAt.Equal(snapshot.UpdatedAt) {
				failure = errors.New("source workload changed; create and approve a new plan")
				return
			}
			project := a.Store.Projects[service.ProjectID]
			if project == nil || project.OrgID != orgID {
				failure = errors.New("source ownership changed")
				return
			}
			selected[snapshot.ID] = true
		}
		for _, record := range a.Store.MigrationJobs {
			if record.Job.Status == "completed" {
				continue
			}
			other := a.Store.MigrationPlans[record.Job.PlanID]
			if other == nil {
				failure = errors.New("existing migration has missing ownership records")
				return
			}
			for _, source := range other.Workloads {
				if selected[source.ID] {
					failure = errors.New("a source workload already has an active migration")
					return
				}
			}
		}
		now := time.Now().UTC()
		result = migration.Job{ID: id, OrgID: orgID, PlanID: planID, Stage: migration.Provision, Status: "ready", UpdatedAt: now, Receipts: map[migration.Stage]string{}}
		if a.Store.MigrationJobs == nil {
			a.Store.MigrationJobs = map[string]*MigrationJobRecord{}
		}
		a.Store.MigrationJobs[id] = &MigrationJobRecord{Job: cloneMigrationJob(result), Version: 1, CreatedAt: now}
		created = true
	})
	if failure != nil {
		return migration.Job{}, failure
	}
	if created {
		if err := a.Store.persistOnce(); err != nil {
			a.Store.Mutate(func() { delete(a.Store.MigrationJobs, id) })
			return migration.Job{}, err
		}
	}
	return result, nil
}

// The caller holds migrationMu throughout Advance, including provider/agent
// execution. Short saves use cloudOperationMu so executor helpers can also
// update artifact records without recursively locking the execution mutex.
// Version checks reject stale journal writers. Multi-server
// execution is disabled until a distributed lease and key lifecycle are provided.
type migrationJournal struct {
	api     *API
	orgID   string
	jobID   string
	version uint64
}

func (j *migrationJournal) Save(ctx context.Context, job migration.Job) error {
	j.api.cloudOperationMu.Lock()
	defer j.api.cloudOperationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.api.Cluster != nil || job.ID != j.jobID || job.OrgID != j.orgID {
		return errors.New("migration journal ownership mismatch")
	}
	var before *MigrationJobRecord
	var failure error
	j.api.Store.Mutate(func() {
		old := j.api.Store.MigrationJobs[j.jobID]
		if old == nil || old.Job.OrgID != j.orgID || old.Version != j.version || old.Job.PlanID != job.PlanID {
			failure = errors.New("migration journal version or identity changed")
			return
		}
		if old.Job.DestinationWritesPossible && !job.DestinationWritesPossible {
			failure = errors.New("destination write boundary cannot be cleared")
			return
		}
		for stage, receipt := range old.Job.Receipts {
			if job.Receipts[stage] != receipt {
				failure = errors.New("migration receipt is immutable")
				return
			}
		}
		before = old
		j.api.Store.MigrationJobs[j.jobID] = &MigrationJobRecord{Job: cloneMigrationJob(job), Version: j.version + 1, CreatedAt: old.CreatedAt}
	})
	if failure != nil {
		return failure
	}
	if err := j.api.Store.persistOnce(); err != nil {
		// Do not let an unpersisted success acknowledgement advance the next
		// in-process worker. A persisted running step will reconcile on restart.
		j.api.Store.Mutate(func() { j.api.Store.MigrationJobs[j.jobID] = before })
		return err
	}
	j.version++
	return nil
}

// advanceMigration is internal until explicit approval and concrete executors
// are wired. It never creates a job or treats a resource review as approval.
func (a *API) advanceMigration(ctx context.Context, orgID, id string, executor migration.Executor) error {
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("migration currently requires a single-server control plane")
	}
	var record MigrationJobRecord
	approved := false
	a.Store.View(func() {
		job := a.Store.MigrationJobs[id]
		if job == nil || job.Job.OrgID != orgID {
			return
		}
		plan := a.Store.MigrationPlans[job.Job.PlanID]
		if plan == nil || plan.OrgID != orgID || plan.ApprovedAt.IsZero() || plan.ApprovedBy == "" {
			return
		}
		record = *job
		record.Job = cloneMigrationJob(job.Job)
		approved = true
	})
	if !approved {
		return errors.New("approved migration job unavailable")
	}
	journal := &migrationJournal{api: a, orgID: orgID, jobID: id, version: record.Version}
	_, err := migration.Advance(ctx, journal, executor, record.Job)
	return err
}

// advanceMigrationWithAgentExecutor is used by the future approved migration
// endpoint. It intentionally supports only stages whose executor is complete.
func (a *API) advanceMigrationWithAgentExecutor(ctx context.Context, orgID, id string) error {
	return a.advanceMigration(ctx, orgID, id, a.migrationExecutor(orgID, id))
}

// Freeze a draft only; approval is deliberately cleared. Public creation will
// additionally enforce caller capabilities and selected workload preflight.
func (a *API) saveMigrationDraft(plan MigrationPlan, member *types.OrgMember) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || plan.ID == "" || plan.OrgID == "" || len(plan.Workloads) == 0 || len(plan.Workloads) > 100 {
		return errors.New("invalid migration draft")
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	var frozen MigrationPlan
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return err
	}
	frozen.ApprovedAt = time.Time{}
	frozen.ApprovedBy = ""
	frozen.CreatedAt = time.Now().UTC()
	frozen.ExpiresAt = frozen.CreatedAt.Add(30 * time.Minute)
	var failure error
	inserted := false
	a.Store.Mutate(func() {
		if old := a.Store.MigrationPlans[plan.ID]; old != nil {
			if old.OrgID != plan.OrgID || old.CloudReviewID != plan.CloudReviewID || len(old.Workloads) != len(plan.Workloads) {
				failure = errors.New("migration draft identity already exists")
				return
			}
			selected := map[string]bool{}
			for _, workload := range plan.Workloads {
				selected[workload.ID] = true
			}
			for _, workload := range old.Workloads {
				if !selected[workload.ID] || member == nil || !projectVisible(member, workload.ProjectID) {
					failure = errors.New("migration draft identity already exists")
					return
				}
			}
			return // Lost-response retry retains the original immutable snapshot.
		}
		review := a.Store.CloudReviewPlans[plan.CloudReviewID]
		if review == nil || review.OrgID != plan.OrgID || time.Now().After(review.ExpiresAt) {
			failure = errors.New("cloud review unavailable or expired")
			return
		}
		seen := map[string]bool{}
		for i, snapshot := range frozen.Workloads {
			service := a.Store.Services[snapshot.ID]
			if service == nil || seen[snapshot.ID] {
				failure = errors.New("invalid selected workload")
				return
			}
			project := a.Store.Projects[service.ProjectID]
			node := a.Store.Nodes[service.NodeID]
			if project == nil || project.OrgID != plan.OrgID || node == nil || node.OrgID != plan.OrgID || member == nil || !projectVisible(member, service.ProjectID) {
				failure = errors.New("workload ownership mismatch")
				return
			}
			seen[snapshot.ID] = true
			// Snapshot trusted store contents rather than caller-supplied settings.
			data, err := json.Marshal(service)
			if err != nil {
				failure = err
				return
			}
			var trusted types.Service
			if err := json.Unmarshal(data, &trusted); err != nil {
				failure = err
				return
			}
			frozen.Workloads[i] = trusted
		}
		if a.Store.MigrationPlans == nil {
			a.Store.MigrationPlans = map[string]*MigrationPlan{}
		}
		a.Store.MigrationPlans[plan.ID] = &frozen
		inserted = true
	})
	if failure != nil {
		return failure
	}
	if !inserted {
		return nil
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { delete(a.Store.MigrationPlans, plan.ID) })
		return err
	}
	return nil
}

var migrationDraftID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

func (a *API) handleCreateMigrationDraft(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil || !requireServiceControl(w, member) {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageMachines }) {
		http.Error(w, "machine management permission is required", 403)
		return
	}
	if a.Cluster != nil {
		http.Error(w, "migration requires a single-server control plane", 409)
		return
	}
	var req struct {
		ID            string   `json:"id"`
		CloudReviewID string   `json:"cloudReviewId"`
		ServiceIDs    []string `json:"serviceIds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF || !migrationDraftID.MatchString(req.ID) || len(req.ServiceIDs) == 0 || len(req.ServiceIDs) > 100 {
		http.Error(w, "invalid migration draft", 400)
		return
	}
	plan := MigrationPlan{ID: req.ID, OrgID: org.ID, CloudReviewID: req.CloudReviewID}
	for _, id := range req.ServiceIDs {
		plan.Workloads = append(plan.Workloads, types.Service{ID: id})
	}
	if err := a.saveMigrationDraft(plan, member); err != nil {
		http.Error(w, "draft could not be saved; check the review, selected workloads and unique draft ID", 409)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"id": req.ID, "status": "draft", "canStart": false, "message": "Workload snapshot saved; cost, compatibility and approval are still required"})
}

func (a *API) handleMigrationPlans(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil || !requireServiceControl(w, member) {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageMachines }) {
		http.Error(w, "machine management permission is required", 403)
		return
	}
	type summary struct {
		ID            string    `json:"id"`
		CloudReviewID string    `json:"cloudReviewId"`
		ServiceIDs    []string  `json:"serviceIds"`
		CreatedAt     time.Time `json:"createdAt"`
		ExpiresAt     time.Time `json:"expiresAt"`
		CanStart      bool      `json:"canStart"`
	}
	out := []summary{}
	a.Store.View(func() {
		for _, plan := range a.Store.MigrationPlans {
			if plan.OrgID != org.ID || (r.PathValue("id") != "" && plan.ID != r.PathValue("id")) {
				continue
			}
			item := summary{ID: plan.ID, CloudReviewID: plan.CloudReviewID, CreatedAt: plan.CreatedAt, ExpiresAt: plan.ExpiresAt, ServiceIDs: []string{}}
			visible := true
			for _, workload := range plan.Workloads {
				project := a.Store.Projects[workload.ProjectID]
				if project == nil || project.OrgID != org.ID || !projectVisible(member, workload.ProjectID) {
					visible = false
					break
				}
				item.ServiceIDs = append(item.ServiceIDs, workload.ID)
			}
			if visible {
				out = append(out, item)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("id") != "" {
		if len(out) == 0 {
			http.Error(w, "migration plan not found", 404)
			return
		}
		writeJSON(w, out[0])
		return
	}
	writeJSON(w, out)
}

func (a *API) handleMigrationJobs(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	_, member := a.requireOrg(w, r)
	if member == nil || !requireServiceControl(w, member) {
		return
	}
	type progress struct {
		ID                        string          `json:"id"`
		Stage                     migration.Stage `json:"stage"`
		Status                    string          `json:"status"`
		UpdatedAt                 time.Time       `json:"updatedAt"`
		DestinationWritesPossible bool            `json:"destinationWritesPossible"`
	}
	items := []progress{}
	a.Store.View(func() {
		for _, record := range a.Store.MigrationJobs {
			job := record.Job
			if job.OrgID != org.ID || (r.PathValue("id") != "" && job.ID != r.PathValue("id")) {
				continue
			}
			plan := a.Store.MigrationPlans[job.PlanID]
			if plan == nil || plan.OrgID != org.ID {
				continue
			}
			visible := true
			for _, source := range plan.Workloads {
				project := a.Store.Projects[source.ProjectID]
				if project == nil || project.OrgID != org.ID || !projectVisible(member, source.ProjectID) {
					visible = false
					break
				}
			}
			if !visible {
				continue
			}
			items = append(items, progress{job.ID, job.Stage, job.Status, job.UpdatedAt, job.DestinationWritesPossible})
		}
	})
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("id") != "" {
		if len(items) == 0 {
			http.Error(w, "migration not found", 404)
			return
		}
		writeJSON(w, items[0])
		return
	}
	writeJSON(w, items)
}
