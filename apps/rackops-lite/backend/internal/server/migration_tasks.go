package server

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

// queueMigrationTask connects the internal executor to durable agent delivery.
// Lock order matches result acceptance: delivery, cloud operation, Store.
// Caller-supplied keys and container IDs are never trusted.
func (a *API) queueMigrationTask(orgID string, kind types.TaskKind, payload types.MigrationTaskPayload) (string, error) {
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || (kind != types.TaskMigrationQuiesce && kind != types.TaskMigrationBackup && kind != types.TaskMigrationRestore && kind != types.TaskMigrationValidate) {
		return "", errors.New("unsupported migration task")
	}
	var nodeID string
	var failure error
	payload.BackupKey = nil
	if kind != types.TaskMigrationRestore && kind != types.TaskMigrationValidate {
		payload.ContainerID = ""
	}
	a.Store.View(func() {
		record := a.Store.MigrationJobs[payload.JobID]
		if record == nil || record.Job.OrgID != orgID || record.Job.Status != "running" {
			failure = errors.New("migration step is not running")
			return
		}
		expected := migration.Quiesce
		if kind == types.TaskMigrationBackup {
			expected = migration.Backup
		} else if kind == types.TaskMigrationRestore {
			expected = migration.Restore
		} else if kind == types.TaskMigrationValidate {
			expected = migration.Validate
		}
		if record.Job.Stage != expected {
			failure = errors.New("migration task does not match the active step")
			return
		}
		plan := a.Store.MigrationPlans[record.Job.PlanID]
		if plan == nil || plan.OrgID != orgID || plan.ApprovedAt.IsZero() {
			failure = errors.New("approved migration plan unavailable")
			return
		}
		if kind == types.TaskMigrationRestore || kind == types.TaskMigrationValidate {
			artifact := a.Store.BackupArtifacts[payload.ArtifactID]
			if artifact == nil || artifact.OrgID != orgID || artifact.JobID != payload.JobID || artifact.ServiceID != payload.ServiceID || artifact.DestinationNodeID == "" {
				failure = errors.New("destination backup artifact unavailable")
				return
			}
			if kind == types.TaskMigrationRestore && artifact.State != "destination_received" && artifact.State != "restored" {
				failure = errors.New("artifact not received at destination")
				return
			}
			if kind == types.TaskMigrationValidate && artifact.State != "restored" && artifact.State != "validated" {
				failure = errors.New("artifact not restored at destination")
				return
			}
			destNode := a.Store.Nodes[artifact.DestinationNodeID]
			if destNode == nil || destNode.OrgID != orgID {
				failure = errors.New("destination node ownership changed")
				return
			}
			nodeID = artifact.DestinationNodeID
			payload.Kind = artifact.Kind
			payload.PlaintextBytes = artifact.Plaintext.Bytes
			payload.PlaintextSHA256 = artifact.Plaintext.SHA256
			if kind == types.TaskMigrationRestore {
				key, err := decryptState(a.Store.stateKey, artifact.SealedKey)
				if err != nil || len(key) != 32 {
					failure = errors.New("backup key unavailable")
					return
				}
				payload.BackupKey = key
			}
			return
		}
		sources := map[string]types.Service{}
		for _, source := range plan.Workloads {
			sources[source.ID] = source
		}
		source, exists := sources[payload.ServiceID]
		current := a.Store.Services[payload.ServiceID]
		if !exists || source.Adopted || source.Kind == types.KindCompose || current == nil || current.NodeID != source.NodeID || current.ContainerID != source.ContainerID || source.ContainerID == "" {
			failure = errors.New("owned migration source changed or is unsupported")
			return
		}
		node := a.Store.Nodes[source.NodeID]
		project := a.Store.Projects[current.ProjectID]
		if node == nil || node.OrgID != orgID || project == nil || project.OrgID != orgID {
			failure = errors.New("source ownership changed")
			return
		}
		nodeID = source.NodeID
		payload.ContainerID = source.ContainerID
		for i, writer := range payload.Writers {
			selected, exists := sources[writer.ServiceID]
			live := a.Store.Services[writer.ServiceID]
			if !exists || selected.NodeID != nodeID || live == nil || live.ContainerID != selected.ContainerID || live.NodeID != nodeID || selected.ContainerID == "" {
				failure = errors.New("writer must be an unchanged selected workload on the backup node")
				return
			}
			payload.Writers[i].ContainerID = selected.ContainerID
		}
		if kind == types.TaskMigrationBackup {
			artifact := a.Store.BackupArtifacts[payload.ArtifactID]
			if artifact == nil || artifact.OrgID != orgID || artifact.JobID != payload.JobID || artifact.ServiceID != payload.ServiceID || artifact.SourceNodeID != nodeID || artifact.Kind != payload.Kind || artifact.State != "prepared" {
				failure = errors.New("prepared backup artifact unavailable")
				return
			}
			key, err := decryptState(a.Store.stateKey, artifact.SealedKey)
			if err != nil || len(key) != 32 {
				failure = errors.New("backup key unavailable")
				return
			}
			payload.BackupKey = key
		}
	})
	defer clear(payload.BackupKey)
	if failure != nil {
		return "", failure
	}
	id := payload.JobID + ":" + string(kind) + ":" + payload.ServiceID + ":" + payload.ArtifactID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	task := types.AgentTask{ID: id, Kind: kind, ServiceID: payload.ServiceID, Payload: string(encoded), CreatedAt: time.Now().UTC()}
	existing := false
	a.Store.View(func() {
		if old := a.Store.TaskOperations[id]; old != nil {
			if old.NodeID != nodeID || old.Task.ServiceID != task.ServiceID || old.Task.Kind != task.Kind || (old.Task.Payload != "" && old.Task.Payload != task.Payload) {
				failure = errors.New("migration task identity conflict")
			}
			existing = true
		}
		for _, old := range a.Store.TaskQueue[nodeID] {
			if old.ID == id {
				existing = true
				if old.Payload != task.Payload {
					failure = errors.New("migration task identity conflict")
				}
			}
		}
	})
	if failure != nil {
		return "", failure
	}
	if existing {
		return id, nil
	}
	a.Store.EnqueueTask(nodeID, task)
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() {
			queue := a.Store.TaskQueue[nodeID]
			filtered := queue[:0]
			for _, entry := range queue {
				if entry.ID != id {
					filtered = append(filtered, entry)
				}
			}
			a.Store.TaskQueue[nodeID] = filtered
		})
		return "", err
	}
	return id, nil
}
