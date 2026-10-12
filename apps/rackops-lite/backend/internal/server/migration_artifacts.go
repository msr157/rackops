package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

func (a *API) registerMigrationArtifactResult(nodeID, serviceID string, result *types.MigrationArtifactResult) error {
	var orgID string
	a.Store.View(func() {
		artifact := a.Store.BackupArtifacts[result.ArtifactID]
		if artifact != nil && artifact.ServiceID == serviceID && artifact.SourceNodeID == nodeID && artifact.JobID == result.JobID {
			orgID = artifact.OrgID
		}
	})
	if orgID == "" {
		return errors.New("backup result does not match the assigned source")
	}
	return a.registerMigrationArtifact(orgID, result.JobID, nodeID, result.ArtifactID,
		migration.ArtifactDigest{Bytes: result.PlaintextBytes, SHA256: result.PlaintextSHA256},
		migration.TransferManifest{Bytes: result.EncryptedBytes, SHA256: result.EncryptedSHA256})
}

// bindMigrationArtifactDestination must run after the destination agent has
// enrolled. It cannot be rebound: a different node would gain access to the
// encrypted payload and make recovery provenance ambiguous.
func (a *API) bindMigrationArtifactDestination(orgID, jobID, artifactID, nodeID string) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || nodeID == "" {
		return errors.New("invalid migration destination")
	}
	var old *BackupArtifact
	var changed bool
	var failure error
	a.Store.Mutate(func() {
		artifact := a.Store.BackupArtifacts[artifactID]
		job := a.Store.MigrationJobs[jobID]
		node := a.Store.Nodes[nodeID]
		if artifact == nil || job == nil || node == nil || artifact.OrgID != orgID || artifact.JobID != jobID || node.OrgID != orgID || job.Job.OrgID != orgID || job.Job.Stage != migration.Transfer || job.Job.Status != "running" || (artifact.State != "ready" && artifact.State != "source_uploaded") {
			failure = errors.New("artifact is not ready for destination binding")
			return
		}
		if artifact.DestinationNodeID != "" && artifact.DestinationNodeID != nodeID {
			failure = errors.New("artifact is already bound to another destination")
			return
		}
		if artifact.DestinationNodeID == nodeID {
			return
		}
		old = artifact
		copy := *artifact
		copy.DestinationNodeID = nodeID
		a.Store.BackupArtifacts[artifactID] = &copy
		changed = true
	})
	if failure != nil || !changed {
		return failure
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.BackupArtifacts[artifactID] = old })
		return err
	}
	return nil
}

func (a *API) registerMigrationTransferResult(nodeID string, task types.AgentTask, result *types.MigrationTransferResult) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	var old *BackupArtifact
	var changed bool
	var failure error
	a.Store.Mutate(func() {
		artifact := a.Store.BackupArtifacts[result.ArtifactID]
		if artifact == nil || artifact.JobID != result.JobID || artifact.ServiceID != task.ServiceID {
			failure = errors.New("transfer result does not match assigned artifact")
			return
		}
		if task.Kind == types.TaskMigrationTransferSource {
			if nodeID != artifact.SourceNodeID || result.Phase != "source_uploaded" || (artifact.State != "ready" && artifact.State != "source_uploaded") {
				failure = errors.New("invalid source transfer result")
				return
			}
			if artifact.State == "source_uploaded" {
				return
			}
		} else if task.Kind == types.TaskMigrationTransferDestination {
			if nodeID != artifact.DestinationNodeID || result.Phase != "destination_received" || (artifact.State != "source_uploaded" && artifact.State != "destination_received") {
				failure = errors.New("invalid destination transfer result")
				return
			}
			if artifact.State == "destination_received" {
				return
			}
		} else {
			failure = errors.New("unexpected transfer task")
			return
		}
		old = artifact
		copy := *artifact
		if result.Phase == "source_uploaded" {
			copy.State = "source_uploaded"
		} else {
			copy.State = "destination_received"
		}
		a.Store.BackupArtifacts[result.ArtifactID] = &copy
		changed = true
	})
	if failure != nil || !changed {
		return failure
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.BackupArtifacts[result.ArtifactID] = old })
		return err
	}
	return nil
}

// Prepare key material once, durably, before an agent can start the backup.
// This is internal; agent dispatch must independently authenticate node/job scope.
func (a *API) prepareMigrationArtifact(orgID, jobID, serviceID, kind string) (string, error) {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || (kind != "volume" && kind != "postgresql" && kind != "mysql") {
		return "", errors.New("unsupported backup preparation")
	}
	id := jobID + ":" + serviceID + ":" + kind
	var sourceNode string
	var failure error
	existing := false
	a.Store.View(func() {
		job := a.Store.MigrationJobs[jobID]
		if job == nil || job.Job.OrgID != orgID {
			failure = errors.New("migration unavailable")
			return
		}
		if old := a.Store.BackupArtifacts[id]; old != nil {
			if old.OrgID != orgID || old.JobID != jobID || old.ServiceID != serviceID || old.Kind != kind {
				failure = errors.New("artifact identity conflict")
				return
			}
			existing = true
			return
		}
		if job.Job.Stage != migration.Backup || job.Job.Status != "running" || job.Job.Receipts[migration.Quiesce] == "" {
			failure = errors.New("migration is not ready for a final backup")
			return
		}
		plan := a.Store.MigrationPlans[job.Job.PlanID]
		if plan == nil || plan.OrgID != orgID {
			failure = errors.New("migration plan unavailable")
			return
		}
		for _, workload := range plan.Workloads {
			if workload.ID == serviceID {
				sourceNode = workload.NodeID
				break
			}
		}
		service := a.Store.Services[serviceID]
		node := a.Store.Nodes[sourceNode]
		if sourceNode == "" || service == nil || service.NodeID != sourceNode || node == nil || node.OrgID != orgID {
			failure = errors.New("backup source ownership changed")
		}
	})
	if failure != nil {
		return "", failure
	}
	if existing {
		return id, nil
	}
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	sealed, err := encryptState(a.Store.stateKey, key)
	if err != nil {
		return "", err
	}
	artifact := &BackupArtifact{ID: id, OrgID: orgID, JobID: jobID, ServiceID: serviceID, SourceNodeID: sourceNode, Kind: kind, SealedKey: sealed, State: "prepared", CreatedAt: time.Now().UTC()}
	a.Store.Mutate(func() {
		if a.Store.BackupArtifacts == nil {
			a.Store.BackupArtifacts = map[string]*BackupArtifact{}
		}
		a.Store.BackupArtifacts[id] = artifact
	})
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { delete(a.Store.BackupArtifacts, id) })
		return "", err
	}
	return id, nil
}

func validArtifactHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}

// Accept metadata only from the authenticated owned source task. A completed
// artifact's identity and both digests are immutable, including on lost-ack retry.
func (a *API) registerMigrationArtifact(orgID, jobID, nodeID, id string, plain migration.ArtifactDigest, transfer migration.TransferManifest) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil || !validArtifactHash(plain.SHA256) || !validArtifactHash(transfer.SHA256) || plain.Bytes < 0 || plain.Bytes > 1<<50 || transfer.Bytes <= plain.Bytes || transfer.Bytes > 1<<50 {
		return errors.New("invalid artifact metadata")
	}
	var old *BackupArtifact
	var failure error
	changed := false
	a.Store.Mutate(func() {
		artifact := a.Store.BackupArtifacts[id]
		if artifact == nil || artifact.OrgID != orgID || artifact.JobID != jobID || artifact.SourceNodeID != nodeID {
			failure = errors.New("artifact ownership mismatch")
			return
		}
		if artifact.State == "ready" {
			if artifact.Plaintext != plain || artifact.Transfer != transfer {
				failure = errors.New("artifact digest conflict")
			}
			return
		}
		if artifact.State != "prepared" {
			failure = errors.New("artifact is not awaiting completion")
			return
		}
		old = artifact
		copy := *artifact
		copy.Plaintext, copy.Transfer, copy.State = plain, transfer, "ready"
		a.Store.BackupArtifacts[id] = &copy
		changed = true
	})
	if failure != nil {
		return failure
	}
	if !changed {
		return nil
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.BackupArtifacts[id] = old })
		return err
	}
	return nil
}

func (a *API) registerMigrationRestoreResult(nodeID, jobID, artifactID string) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	var old *BackupArtifact
	var changed bool
	var failure error
	a.Store.Mutate(func() {
		artifact := a.Store.BackupArtifacts[artifactID]
		if artifact == nil || artifact.JobID != jobID || artifact.DestinationNodeID != nodeID {
			failure = errors.New("restore result does not match assigned artifact destination")
			return
		}
		if artifact.State != "destination_received" && artifact.State != "restored" {
			failure = errors.New("artifact was not received at destination")
			return
		}
		if artifact.State == "restored" {
			return
		}
		old = artifact
		copy := *artifact
		copy.State = "restored"
		a.Store.BackupArtifacts[artifactID] = &copy
		changed = true
	})
	if failure != nil || !changed {
		return failure
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.BackupArtifacts[artifactID] = old })
		return err
	}
	return nil
}

func (a *API) registerMigrationValidateResult(nodeID, jobID, artifactID string) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	var old *BackupArtifact
	var changed bool
	var failure error
	a.Store.Mutate(func() {
		artifact := a.Store.BackupArtifacts[artifactID]
		if artifact == nil || artifact.JobID != jobID || artifact.DestinationNodeID != nodeID {
			failure = errors.New("validation result does not match assigned artifact destination")
			return
		}
		if artifact.State != "restored" && artifact.State != "validated" {
			failure = errors.New("artifact was not restored at destination")
			return
		}
		if artifact.State == "validated" {
			return
		}
		old = artifact
		copy := *artifact
		copy.State = "validated"
		a.Store.BackupArtifacts[artifactID] = &copy
		changed = true
	})
	if failure != nil || !changed {
		return failure
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.BackupArtifacts[artifactID] = old })
		return err
	}
	return nil
}
