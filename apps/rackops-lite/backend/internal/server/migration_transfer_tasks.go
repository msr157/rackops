package server

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

// queueMigrationTransferTask sends only immutable encrypted-transfer metadata.
// It never distributes a backup key: destination restore receives that key only
// after transfer completion through a separate protected task.
func (a *API) queueMigrationTransferTask(orgID, jobID, artifactID string, kind types.TaskKind) (string, error) {
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || (kind != types.TaskMigrationTransferSource && kind != types.TaskMigrationTransferDestination) {
		return "", errors.New("invalid migration transfer task")
	}
	var artifact BackupArtifact
	var nodeID string
	var failure error
	a.Store.View(func() {
		stored := a.Store.BackupArtifacts[artifactID]
		job := a.Store.MigrationJobs[jobID]
		if stored == nil || job == nil || stored.OrgID != orgID || stored.JobID != jobID || job.Job.OrgID != orgID || job.Job.Stage != migration.Transfer || job.Job.Status != "running" {
			failure = errors.New("migration artifact is not ready for transfer")
			return
		}
		if kind == types.TaskMigrationTransferSource {
			if stored.State != "ready" && stored.State != "source_uploaded" {
				failure = errors.New("source artifact state cannot be transferred")
				return
			}
			nodeID = stored.SourceNodeID
		} else {
			if stored.State != "source_uploaded" && stored.State != "destination_received" || stored.DestinationNodeID == "" {
				failure = errors.New("destination is not ready for transfer")
				return
			}
			nodeID = stored.DestinationNodeID
		}
		if node := a.Store.Nodes[nodeID]; node == nil || node.OrgID != orgID {
			failure = errors.New("transfer node ownership changed")
			return
		}
		artifact = *stored
	})
	if failure != nil {
		return "", failure
	}
	payload := types.MigrationTaskPayload{JobID: jobID, ServiceID: artifact.ServiceID, ArtifactID: artifactID, TransferBytes: artifact.Transfer.Bytes, TransferSHA256: artifact.Transfer.SHA256}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id := jobID + ":" + string(kind) + ":" + artifactID
	task := types.AgentTask{ID: id, Kind: kind, ServiceID: artifact.ServiceID, Payload: string(raw), CreatedAt: time.Now().UTC()}
	exists := false
	a.Store.View(func() {
		if old := a.Store.TaskOperations[id]; old != nil {
			exists = old.NodeID == nodeID && old.Task.Payload == task.Payload
			if !exists {
				failure = errors.New("transfer task identity conflict")
			}
		}
		for _, queued := range a.Store.TaskQueue[nodeID] {
			if queued.ID == id {
				exists = queued.Payload == task.Payload
				if !exists {
					failure = errors.New("transfer task identity conflict")
				}
			}
		}
	})
	if failure != nil {
		return "", failure
	}
	if exists {
		return id, nil
	}
	a.Store.EnqueueTask(nodeID, task)
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() {
			queue := a.Store.TaskQueue[nodeID]
			filtered := queue[:0]
			for _, queued := range queue {
				if queued.ID != id {
					filtered = append(filtered, queued)
				}
			}
			a.Store.TaskQueue[nodeID] = filtered
		})
		return "", err
	}
	return id, nil
}
