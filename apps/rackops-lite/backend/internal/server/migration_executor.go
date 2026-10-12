package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

// agentMigrationExecutor orchestrates the durable multi-step migration workflow:
// Quiesce -> Backup -> Transfer -> Restore -> Validate -> Cutover -> EnableWrites -> Complete.
// Each task is durable and idempotently queued before reporting running.
type agentMigrationExecutor struct {
	api   *API
	orgID string
	jobID string
}

func (a *API) migrationExecutor(orgID, jobID string) migration.Executor {
	return &agentMigrationExecutor{api: a, orgID: orgID, jobID: jobID}
}

func (e *agentMigrationExecutor) getWorkloadServices(stage migration.Stage) ([]string, error) {
	var services []string
	var failure error
	e.api.Store.View(func() {
		job := e.api.Store.MigrationJobs[e.jobID]
		if job == nil || job.Job.OrgID != e.orgID || job.Job.Stage != stage || job.Job.Status != "running" {
			failure = errors.New("migration step is not running")
			return
		}
		plan := e.api.Store.MigrationPlans[job.Job.PlanID]
		if plan == nil || plan.OrgID != e.orgID {
			failure = errors.New("approved migration plan unavailable")
			return
		}
		for _, workload := range plan.Workloads {
			services = append(services, workload.ID)
		}
	})
	if failure != nil {
		return nil, failure
	}
	if len(services) == 0 {
		return nil, errors.New("approved migration workloads are unavailable")
	}
	sort.Strings(services)
	return services, nil
}

func (e *agentMigrationExecutor) getArtifacts() ([]*BackupArtifact, error) {
	var artifacts []*BackupArtifact
	var failure error
	e.api.Store.View(func() {
		job := e.api.Store.MigrationJobs[e.jobID]
		if job == nil || job.Job.OrgID != e.orgID {
			failure = errors.New("migration job unavailable")
			return
		}
		for _, art := range e.api.Store.BackupArtifacts {
			if art.JobID == e.jobID && art.OrgID == e.orgID {
				artifacts = append(artifacts, art)
			}
		}
	})
	if failure != nil {
		return nil, failure
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].ID < artifacts[j].ID })
	return artifacts, nil
}

func (e *agentMigrationExecutor) Execute(ctx context.Context, stage migration.Stage, operationID string) (string, bool, error) {
	switch stage {
	case migration.Provision:
		var plan *MigrationPlan
		var review *CloudReviewPlan
		e.api.Store.View(func() {
			job := e.api.Store.MigrationJobs[e.jobID]
			if job != nil {
				plan = e.api.Store.MigrationPlans[job.Job.PlanID]
				if plan != nil && plan.CloudReviewID != "" {
					review = e.api.Store.CloudReviewPlans[plan.CloudReviewID]
				}
			}
		})
		if plan == nil {
			return "", false, errors.New("migration plan unavailable")
		}
		if plan.DestinationNodeID != "" {
			var nodeOnline bool
			e.api.Store.View(func() {
				if n := e.api.Store.Nodes[plan.DestinationNodeID]; n != nil && n.Status == types.NodeOnline {
					nodeOnline = true
				}
			})
			if nodeOnline {
				return operationID, true, nil
			}
		}
		if review == nil {
			return "", false, errors.New("cloud review plan unavailable for provisioning")
		}
		provID := review.Spec.ID()
		var existingProv *CloudProvisioning
		e.api.Store.View(func() {
			existingProv = e.api.Store.CloudProvisioning[provID]
		})
		if existingProv == nil {
			prov := CloudProvisioning{
				ID:           provID,
				OrgID:        e.orgID,
				ConnectionID: review.ConnectionID,
				Provider:     review.Provider,
				Region:       review.Region,
				AccountID:    review.AccountID,
				Spec:         review.Spec,
			}
			if err := e.api.saveCloudProvisioning(prov); err != nil {
				return "", false, err
			}
		}
		var provJob *CloudProvisioning
		e.api.Store.View(func() {
			provJob = e.api.Store.CloudProvisioning[provID]
		})
		if provJob != nil && provJob.BootstrapConfig == nil {
			config := cloud.BootstrapConfig{
				ServerURL:    "https://controlplane.rackops.local",
				OrgID:        e.orgID,
				Architecture: review.Architecture,
				BinaryURL:    "https://github.com/msr157/rackops/releases/download/v1.0.0/rackops-linux-" + review.Architecture,
				SHA256:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}
			if err := e.api.prepareCloudBootstrap(e.orgID, provID, config); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Quiesce:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		for _, serviceID := range services {
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationQuiesce, types.MigrationTaskPayload{JobID: e.jobID, ServiceID: serviceID}); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Backup:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		for _, serviceID := range services {
			var kind string
			var writers []types.MigrationWriter
			e.api.Store.View(func() {
				svc := e.api.Store.Services[serviceID]
				if svc != nil {
					if svc.Kind == types.KindDatabase {
						kind = "postgresql"
						if strings.Contains(strings.ToLower(svc.Image), "mysql") {
							kind = "mysql"
						}
					} else {
						kind = "volume"
					}
				}
			})
			if kind == "" {
				kind = "volume"
			}
			artID, err := e.api.prepareMigrationArtifact(e.orgID, e.jobID, serviceID, kind)
			if err != nil {
				return "", false, err
			}
			payload := types.MigrationTaskPayload{
				JobID:      e.jobID,
				ServiceID:  serviceID,
				ArtifactID: artID,
				Kind:       kind,
				Writers:    writers,
			}
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationBackup, payload); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Transfer:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts prepared for transfer")
		}
		for _, art := range artifacts {
			if _, err := e.api.queueMigrationTransferTask(e.orgID, e.jobID, art.ID, types.TaskMigrationTransferSource); err != nil {
				return "", false, err
			}
			if art.State == "source_uploaded" && art.DestinationNodeID != "" {
				if _, err := e.api.queueMigrationTransferTask(e.orgID, e.jobID, art.ID, types.TaskMigrationTransferDestination); err != nil {
					return "", false, err
				}
			}
		}
		return operationID, false, nil

	case migration.Restore:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts available for restore")
		}
		for _, art := range artifacts {
			if art.State != "destination_received" && art.State != "restored" {
				return "", false, errors.New("artifact not yet received at destination: " + art.ID)
			}
			payload := types.MigrationTaskPayload{
				JobID:      e.jobID,
				ServiceID:  art.ServiceID,
				ArtifactID: art.ID,
				Kind:       art.Kind,
			}
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationRestore, payload); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Validate:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts available for validation")
		}
		for _, art := range artifacts {
			if art.State != "restored" && art.State != "validated" {
				return "", false, errors.New("artifact not yet restored: " + art.ID)
			}
			payload := types.MigrationTaskPayload{
				JobID:      e.jobID,
				ServiceID:  art.ServiceID,
				ArtifactID: art.ID,
				Kind:       art.Kind,
			}
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationValidate, payload); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Cutover:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		var destNodeID string
		e.api.Store.View(func() {
			job := e.api.Store.MigrationJobs[e.jobID]
			if job != nil {
				plan := e.api.Store.MigrationPlans[job.Job.PlanID]
				if plan != nil {
					destNodeID = plan.DestinationNodeID
				}
			}
		})
		if destNodeID == "" {
			return "", false, errors.New("destination node identity unavailable for cutover")
		}
		for _, serviceID := range services {
			payload := types.MigrationTaskPayload{
				JobID:     e.jobID,
				ServiceID: serviceID,
			}
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationCutover, payload); err != nil {
				return "", false, err
			}
		}
		e.api.Store.Mutate(func() {
			for _, serviceID := range services {
				svc := e.api.Store.Services[serviceID]
				if svc != nil {
					svc.NodeID = destNodeID
					svc.UpdatedAt = time.Now().UTC()
				}
			}
		})
		_ = e.api.Store.persistOnce()
		return operationID, false, nil

	case migration.EnableWrites:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		for _, serviceID := range services {
			var kind string
			e.api.Store.View(func() {
				svc := e.api.Store.Services[serviceID]
				if svc != nil {
					if svc.Kind == types.KindDatabase {
						kind = "postgresql"
						if strings.Contains(strings.ToLower(svc.Image), "mysql") {
							kind = "mysql"
						}
					} else {
						kind = "volume"
					}
				}
			})
			payload := types.MigrationTaskPayload{
				JobID:     e.jobID,
				ServiceID: serviceID,
				Kind:      kind,
			}
			if _, err := e.api.queueMigrationTask(e.orgID, types.TaskMigrationEnableWrites, payload); err != nil {
				return "", false, err
			}
		}
		return operationID, false, nil

	case migration.Complete:
		return operationID, true, nil

	default:
		return "", false, errors.New("migration stage executor is not implemented")
	}
}

func (e *agentMigrationExecutor) Reconcile(_ context.Context, stage migration.Stage, operationID string) (string, bool, error) {
	switch stage {
	case migration.Provision:
		var plan *MigrationPlan
		var provJob *CloudProvisioning
		e.api.Store.View(func() {
			job := e.api.Store.MigrationJobs[e.jobID]
			if job != nil {
				plan = e.api.Store.MigrationPlans[job.Job.PlanID]
				if plan != nil && plan.CloudReviewID != "" {
					provJob = e.api.Store.CloudProvisioning[plan.CloudReviewID]
				}
			}
		})
		if plan == nil {
			return "", false, errors.New("migration plan unavailable")
		}
		if plan.DestinationNodeID != "" {
			var nodeOnline bool
			e.api.Store.View(func() {
				if n := e.api.Store.Nodes[plan.DestinationNodeID]; n != nil && n.Status == types.NodeOnline {
					nodeOnline = true
				}
			})
			if nodeOnline {
				return operationID, true, nil
			}
		}
		if provJob == nil {
			return "", false, errors.New("cloud provisioning job not found")
		}
		if provJob.EnrollmentHash != "" {
			var enrolledNodeID string
			e.api.Store.View(func() {
				grant := e.api.Store.NodeEnrollments[provJob.EnrollmentHash]
				if grant != nil && grant.NodeID != "" {
					node := e.api.Store.Nodes[grant.NodeID]
					if node != nil && node.Status == types.NodeOnline {
						enrolledNodeID = node.ID
					}
				}
			})
			if enrolledNodeID != "" {
				e.api.Store.Mutate(func() {
					p := e.api.Store.MigrationPlans[plan.ID]
					if p != nil {
						p.DestinationNodeID = enrolledNodeID
					}
					for _, art := range e.api.Store.BackupArtifacts {
						if art.JobID == e.jobID && art.OrgID == e.orgID {
							art.DestinationNodeID = enrolledNodeID
						}
					}
				})
				_ = e.api.Store.persistOnce()
				return operationID, true, nil
			}
		}
		return "", false, nil

	case migration.Quiesce:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, serviceID := range services {
				id := e.jobID + ":" + string(types.TaskMigrationQuiesce) + ":" + serviceID + ":"
				task := e.api.Store.TaskOperations[id]
				if task == nil || task.State == "delivered" {
					done = false
					continue
				}
				if task.State != "completed" || task.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("at least one source writer did not quiesce")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Backup:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no backup artifacts found")
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, art := range artifacts {
				current := e.api.Store.BackupArtifacts[art.ID]
				if current == nil || (current.State != "ready" && current.State != "source_uploaded" && current.State != "destination_received" && current.State != "restored") {
					done = false
				}
				taskID := e.jobID + ":" + string(types.TaskMigrationBackup) + ":" + art.ServiceID + ":" + art.ID
				op := e.api.Store.TaskOperations[taskID]
				if op != nil && op.State == "completed" && op.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("at least one backup task failed")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Transfer:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts for transfer reconciliation")
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, art := range artifacts {
				current := e.api.Store.BackupArtifacts[art.ID]
				if current == nil {
					done = false
					continue
				}
				if current.State != "destination_received" && current.State != "restored" {
					done = false
					if current.State == "source_uploaded" && current.DestinationNodeID != "" {
						destTaskID := e.jobID + ":" + string(types.TaskMigrationTransferDestination) + ":" + art.ID
						if op := e.api.Store.TaskOperations[destTaskID]; op != nil && op.State == "completed" && op.Outcome != "succeeded" {
							failure = true
						}
					}
					sourceTaskID := e.jobID + ":" + string(types.TaskMigrationTransferSource) + ":" + art.ID
					if op := e.api.Store.TaskOperations[sourceTaskID]; op != nil && op.State == "completed" && op.Outcome != "succeeded" {
						failure = true
					}
				}
			}
		})
		if failure {
			return "", false, errors.New("transfer failed on source or destination")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Restore:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts for restore reconciliation")
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, art := range artifacts {
				current := e.api.Store.BackupArtifacts[art.ID]
				if current == nil || current.State != "restored" {
					done = false
				}
				taskID := e.jobID + ":" + string(types.TaskMigrationRestore) + ":" + art.ServiceID + ":" + art.ID
				op := e.api.Store.TaskOperations[taskID]
				if op != nil && op.State == "completed" && op.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("at least one destination restore task failed")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Validate:
		artifacts, err := e.getArtifacts()
		if err != nil {
			return "", false, err
		}
		if len(artifacts) == 0 {
			return "", false, errors.New("no artifacts for validate reconciliation")
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, art := range artifacts {
				current := e.api.Store.BackupArtifacts[art.ID]
				if current == nil || current.State != "validated" {
					done = false
				}
				taskID := e.jobID + ":" + string(types.TaskMigrationValidate) + ":" + art.ServiceID + ":" + art.ID
				op := e.api.Store.TaskOperations[taskID]
				if op != nil && op.State == "completed" && op.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("at least one destination validation task failed")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Cutover:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, serviceID := range services {
				taskID := e.jobID + ":" + string(types.TaskMigrationCutover) + ":" + serviceID + ":"
				op := e.api.Store.TaskOperations[taskID]
				if op == nil || op.State == "delivered" {
					done = false
					continue
				}
				if op.State != "completed" || op.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("cutover routing failed on destination node")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.EnableWrites:
		services, err := e.getWorkloadServices(stage)
		if err != nil {
			return "", false, err
		}
		done := true
		var failure bool
		e.api.Store.View(func() {
			for _, serviceID := range services {
				taskID := e.jobID + ":" + string(types.TaskMigrationEnableWrites) + ":" + serviceID + ":"
				op := e.api.Store.TaskOperations[taskID]
				if op == nil || op.State == "delivered" {
					done = false
					continue
				}
				if op.State != "completed" || op.Outcome != "succeeded" {
					failure = true
				}
			}
		})
		if failure {
			return "", false, errors.New("enable writes failed on destination node")
		}
		if !done {
			return "", false, nil
		}
		return operationID, true, nil

	case migration.Complete:
		return operationID, true, nil

	default:
		return "", false, errors.New("migration stage reconciliation is not implemented")
	}
}
