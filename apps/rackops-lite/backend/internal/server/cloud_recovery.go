package server

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/cloud"
)

// RunCloudRecovery only reconciles previously submitted operations. Planned
// resources are never submitted here, even when a bootstrap is present.
func (a *API) RunCloudRecovery() {
	if !a.cloudRecoveryMu.TryLock() {
		return
	}
	defer a.cloudRecoveryMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-a.Store.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		a.recoverCloudJobs(ctx, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func recoverableCloudJob(job *CloudProvisioning) bool {
	if job == nil || (job.State != "submitting" && job.State != "submitted" && job.State != "uncertain") {
		return false
	}
	return job.RecoveryState != "provider_ready" && job.RecoveryState != "needs_attention" && job.RecoveryState != "paused"
}

// Attempts and next-run time are saved before any provider access, so a crash
// cannot reset the retry budget. No raw provider errors are exposed or logged.
func (a *API) recoverCloudJobs(ctx context.Context, now time.Time) {
	if a.Cluster != nil {
		return
	}
	var ids []string
	a.Store.View(func() {
		for id, job := range a.Store.CloudProvisioning {
			if recoverableCloudJob(job) && !now.Before(job.RecoveryNextAt) {
				ids = append(ids, id)
			}
		}
	})
	sort.Strings(ids)
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		a.recoverCloudJob(ctx, id, now)
	}
}

func (a *API) recoverCloudJob(ctx context.Context, id string, now time.Time) {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if ctx.Err() != nil || a.Cluster != nil {
		return
	}
	var job CloudProvisioning
	var connection CloudConnection
	eligible := false
	a.Store.Mutate(func() {
		current := a.Store.CloudProvisioning[id]
		if !recoverableCloudJob(current) || now.Before(current.RecoveryNextAt) {
			return
		}
		started := current.RecoveryWindowStartedAt
		if started.IsZero() {
			started = current.CreatedAt
		}
		if current.RecoveryAttempts >= 36 || now.Sub(started) > 24*time.Hour {
			current.RecoveryState = "needs_attention"
			current.RecoveryMessage = "Automatic recovery limit reached. Inspect the saved cloud resource and operation IDs before further action."
			return
		}
		current.RecoveryAttempts++
		delay := 30 * time.Second
		for i := 1; i < current.RecoveryAttempts && delay < 15*time.Minute; i++ {
			delay *= 2
		}
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
		current.RecoveryNextAt = now.Add(delay)
		current.RecoveryState = "checking"
		current.RecoveryMessage = "Checking the previously submitted operation; no new resource will be created."
		job = *current
		c := a.Store.CloudConnections[current.ConnectionID]
		if c == nil || c.OrgID != current.OrgID || c.Provider != current.Provider || c.AccountID != current.AccountID || c.Region != current.Region {
			current.RecoveryState = "needs_attention"
			current.RecoveryMessage = "The original cloud connection is unavailable or changed. Restore access to the original account before recovery."
			return
		}
		connection = *c
		eligible = true
	})
	if err := a.Store.persistOnce(); err != nil || !eligible {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	credentials, err := decryptState(a.Store.stateKey, connection.SealedCredentials)
	var provider cloud.Provider
	if err == nil {
		factory := a.cloudRecoveryProvider
		if factory == nil {
			factory = cloud.NewProvider
		}
		provider, err = factory(callCtx, connection.Provider, connection.Region, credentials)
	}
	if err == nil {
		err = a.runCloudProvisioningLocked(callCtx, job.OrgID, id, provider)
	}
	a.Store.Mutate(func() {
		current := a.Store.CloudProvisioning[id]
		if current == nil {
			return
		}
		current.RecoveryState = "waiting"
		current.RecoveryMessage = "Waiting for the existing cloud operation; another bounded check is scheduled."
		if err != nil {
			current.RecoveryMessage = "Cloud recovery could not verify the operation. Check account access, credential expiry and connectivity; saved resource IDs are retained."
			return
		}
		switch strings.ToLower(current.Receipt.State) {
		case "running", "succeeded":
			if current.Receipt.ResourceID != "" {
				current.RecoveryState = "provider_ready"
				current.RecoveryMessage = "The provider reports the resource ready. Agent, application, data and routing validation are still required."
			}
		case "failed", "canceled", "cancelled", "terminated", "shutting-down", "deleting":
			current.RecoveryState = "needs_attention"
			current.RecoveryMessage = "The provider reports a terminal resource state. Inspect the existing resource; no replacement or cleanup was attempted."
		}
	})
	a.Store.persistOnce()
}
