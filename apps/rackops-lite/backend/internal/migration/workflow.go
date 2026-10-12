package migration

import (
	"context"
	"errors"
	"time"
)

type Stage string

const (
	Provision    Stage = "provision"
	Quiesce      Stage = "quiesce"
	Backup       Stage = "backup"
	Transfer     Stage = "transfer"
	Restore      Stage = "restore"
	Validate     Stage = "validate"
	Cutover      Stage = "cutover"
	EnableWrites Stage = "enable_writes"
	Complete     Stage = "complete"
)

var stages = []Stage{Provision, Quiesce, Backup, Transfer, Restore, Validate, Cutover, EnableWrites, Complete}

// Job is persisted by the control plane. No credentials, SQL, environment values
// or dump contents belong in this record. Resource receipts survive retries.
type Job struct {
	ID        string           `json:"id"`
	OrgID     string           `json:"orgId"`
	PlanID    string           `json:"planId"`
	Stage     Stage            `json:"stage"`
	Status    string           `json:"status"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Receipts  map[Stage]string `json:"receipts"`
	// Write enablement may have succeeded even when its acknowledgement was lost.
	DestinationWritesPossible bool `json:"destinationWritesPossible"`
}

// Journal must atomically persist before returning success and serialize a job
// across its whole Advance call. HA/multiple runners need a durable lease first.
type Journal interface {
	Save(context.Context, Job) error
}

// Executor methods use the stable operationID for deduplication. Reconcile must
// inspect the original operation; it must never create a replacement operation.
// User-visible messages are stage-specific; provider errors are not persisted.
type Executor interface {
	// Execute may dispatch an asynchronous operation. A false done value keeps
	// the persisted job running; Reconcile decides whether it later completed.
	Execute(context.Context, Stage, string) (receipt string, done bool, err error)
	Reconcile(context.Context, Stage, string) (receipt string, done bool, err error)
}

// Advance executes at most one step. The calling control plane validates current
// authorization, approval, immutable plan and ownership before creating a job.
// Unknown results remain awaiting reconciliation, never automatically replayed.
func Advance(ctx context.Context, journal Journal, executor Executor, job Job) (Job, error) {
	if job.ID == "" || job.OrgID == "" || job.PlanID == "" {
		return job, errors.New("incomplete migration identity")
	}
	index := -1
	for i, stage := range stages {
		if stage == job.Stage {
			index = i
			break
		}
	}
	if index < 0 {
		return job, errors.New("unknown migration stage")
	}
	for _, required := range stages[:index] {
		if job.Receipts[required] == "" {
			return job, errors.New("migration prerequisite receipt is missing")
		}
	}
	if job.Stage == Complete {
		return job, nil
	}
	if job.Status != "ready" && job.Status != "running" && job.Status != "uncertain" {
		return job, errors.New("migration is paused or requires operator recovery")
	}
	// Never mutate the journal's map through a shallow copy.
	receipts := make(map[Stage]string, len(job.Receipts)+1)
	for stage, receipt := range job.Receipts {
		receipts[stage] = receipt
	}
	job.Receipts = receipts
	operationID := job.ID + ":" + string(job.Stage)
	var receipt string
	var err error
	if job.Status == "running" || job.Status == "uncertain" {
		var done bool
		receipt, done, err = executor.Reconcile(ctx, job.Stage, operationID)
		if err != nil {
			job.Status = "recovery_required"
			job.UpdatedAt = time.Now().UTC()
			if saveErr := journal.Save(context.WithoutCancel(ctx), job); saveErr != nil {
				return job, saveErr
			}
			return job, errors.New("migration step requires operator recovery; retain source and destination")
		}
		if !done {
			return job, nil
		}
	} else {
		job.Status = "running"
		job.UpdatedAt = time.Now().UTC()
		// Persist this BEFORE issuing the write-enablement command. On a lost
		// response or process crash, routing back to the old source is unsafe.
		if job.Stage == EnableWrites {
			job.DestinationWritesPossible = true
		}
		if err := journal.Save(ctx, job); err != nil {
			return job, err
		}
		var done bool
		receipt, done, err = executor.Execute(ctx, job.Stage, operationID)
		if err != nil {
			job.Status = "uncertain"
			job.UpdatedAt = time.Now().UTC()
			if saveErr := journal.Save(context.WithoutCancel(ctx), job); saveErr != nil {
				return job, saveErr
			}
			return job, errors.New("migration step outcome is uncertain; reconciliation is required")
		}
		if !done {
			return job, nil
		}
	}
	if receipt == "" {
		return job, errors.New("migration step did not return a durable receipt")
	}
	job.Receipts[job.Stage] = receipt
	job.Stage = stages[index+1]
	job.Status = "ready"
	if job.Stage == Complete {
		job.Status = "completed"
	}
	job.UpdatedAt = time.Now().UTC()
	if err := journal.Save(ctx, job); err != nil {
		// Return a reconciliation state, never an advanced runnable stage when
		// its durable acknowledgement failed. The next attempt inspects the same
		// operation ID rather than repeating the side effect or skipping a step.
		job.Stage = stages[index]
		job.Status = "uncertain"
		return job, err
	}
	return job, nil
}

// RequestRecovery records intent only. It does not automatically switch traffic,
// enable writers or delete resources. Reconciliation must first establish that
// in-flight commands have finished; source resumption is an explicit later step.
func RequestRecovery(ctx context.Context, journal Journal, job Job) (Job, error) {
	if job.Status == "running" || job.Status == "uncertain" {
		return job, errors.New("reconcile the in-flight operation before recovery")
	}
	if job.DestinationWritesPossible {
		job.Status = "data_reconciliation_required"
	} else {
		job.Status = "source_recovery_requested"
	}
	job.UpdatedAt = time.Now().UTC()
	return job, journal.Save(ctx, job)
}
