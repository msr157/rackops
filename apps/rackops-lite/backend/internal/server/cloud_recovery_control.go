package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// Controls read-only observation, never provider creation, cancellation or deletion.
func (a *API) handleCloudRecoveryControl(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || (req.Action != "pause" && req.Action != "resume") {
		http.Error(w, "action must be pause or resume", 400)
		return
	}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.cloudOrg(w, r) == nil {
		return
	}
	status := 200
	message := ""
	var progress cloudProgress
	a.Store.Mutate(func() {
		job := a.Store.CloudProvisioning[r.PathValue("id")]
		if job == nil || job.OrgID != org.ID {
			status = 404
			message = "cloud operation not found"
			return
		}
		if job.State != "submitting" && job.State != "submitted" && job.State != "uncertain" {
			status = 409
			message = "only previously submitted operations can be observed"
			return
		}
		if job.RecoveryState == "provider_ready" {
			status = 409
			message = "provider recovery already completed"
			return
		}
		now := time.Now().UTC()
		if req.Action == "pause" {
			job.RecoveryState = "paused"
			job.RecoveryMessage = "Automatic status checks are paused. Existing cloud resources continue running and may incur charges."
		} else if job.RecoveryState == "paused" || job.RecoveryState == "needs_attention" {
			c := a.Store.CloudConnections[job.ConnectionID]
			if c == nil || c.OrgID != org.ID || c.Provider != job.Provider || c.AccountID != job.AccountID || c.Region != job.Region {
				status = 409
				message = "restore the original cloud connection before resuming checks"
				return
			}
			job.RecoveryPreviousAttempts += job.RecoveryAttempts
			job.RecoveryAttempts = 0
			job.RecoveryWindowStartedAt = now
			job.RecoveryNextAt = now
			job.RecoveryState = "waiting"
			job.RecoveryMessage = "Status checks resumed with a new bounded retry window. No replacement resource will be launched."
		}
		job.UpdatedAt = now
		progress = a.cloudProgressLocked(job, now)
	})
	if status != 200 {
		http.Error(w, message, status)
		return
	}
	if a.Store.persistOnce() != nil {
		http.Error(w, "could not persist recovery control; inspect operation state before retrying", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, progress)
}
