package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func (a *API) handleCloudProgressList(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	out := []cloudProgress{}
	a.Store.View(func() {
		for _, job := range a.Store.CloudProvisioning {
			if job.OrgID == org.ID {
				out = append(out, a.cloudProgressLocked(job, time.Now().UTC()))
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, out)
}

// A dedicated response type prevents credentials, enrollment hashes and startup
// payloads from leaking when fields are added to the durable job model.
type cloudProgress struct {
	ID                 string     `json:"id"`
	Provider           string     `json:"provider"`
	Region             string     `json:"region"`
	State              string     `json:"state"`
	ResourceID         string     `json:"resourceId,omitempty"`
	OperationID        string     `json:"operationId,omitempty"`
	ResourceState      string     `json:"resourceState,omitempty"`
	AgentStage         string     `json:"agentStage"`
	Message            string     `json:"message"`
	NodeID             string     `json:"nodeId,omitempty"`
	EnrollmentDeadline *time.Time `json:"enrollmentDeadline,omitempty"`
	LastSeen           *time.Time `json:"lastSeen,omitempty"`
	// An authenticated heartbeat is not proof of application or Docker health.
	CanMigrate            bool       `json:"canMigrate"`
	RecoveryState         string     `json:"recoveryState,omitempty"`
	RecoveryMessage       string     `json:"recoveryMessage,omitempty"`
	RecoveryAttempts      int        `json:"recoveryAttempts,omitempty"`
	RecoveryTotalAttempts int        `json:"recoveryTotalAttempts"`
	RecoveryNextAt        *time.Time `json:"recoveryNextAt,omitempty"`
}

func (a *API) handleCloudProgress(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var progress cloudProgress
	a.Store.View(func() {
		if job := a.Store.CloudProvisioning[r.PathValue("id")]; job != nil && job.OrgID == org.ID {
			progress = a.cloudProgressLocked(job, time.Now().UTC())
		}
	})
	if progress.ID == "" {
		http.Error(w, "cloud operation not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, progress)
}

// Read while holding the store lock. Agent evidence and provider receipts stay
// separate: neither an allocated VM nor a startup script exit proves readiness.
func (a *API) cloudProgressLocked(job *CloudProvisioning, now time.Time) cloudProgress {
	out := cloudProgress{ID: job.ID, Provider: job.Provider, Region: job.Region, State: job.State,
		ResourceID: job.Receipt.ResourceID, OperationID: job.Receipt.OperationID, ResourceState: job.Receipt.State}
	out.RecoveryState, out.RecoveryMessage, out.RecoveryAttempts = job.RecoveryState, job.RecoveryMessage, job.RecoveryAttempts
	out.RecoveryTotalAttempts = job.RecoveryPreviousAttempts + job.RecoveryAttempts
	if job.RecoveryState == "waiting" || job.RecoveryState == "checking" {
		next := job.RecoveryNextAt
		out.RecoveryNextAt = &next
	}
	stage := func(code, message string) cloudProgress { out.AgentStage = code; out.Message = message; return out }
	if job.BootstrapConfig == nil || job.EnrollmentHash == "" || len(job.SealedBootstrap) == 0 {
		return stage("bootstrap_missing", "Prepare a verified agent bootstrap before launching this plan.")
	}
	grant := a.Store.NodeEnrollments[job.EnrollmentHash]
	if grant == nil || grant.OrgID != job.OrgID {
		return stage("enrollment_unavailable", "Enrollment evidence is unavailable. Inspect existing resources before any replacement launch.")
	}
	deadline := grant.ExpiresAt
	out.EnrollmentDeadline = &deadline
	if grant.NodeID == "" {
		if !now.Before(deadline) {
			return stage("enrollment_expired", "The agent did not enroll before its deadline. Inspect VM startup logs and network access; do not create another VM to retry this operation.")
		}
		if job.State == "planned" {
			return stage("awaiting_submission", "Bootstrap is prepared; this operation has not been submitted.")
		}
		return stage("awaiting_enrollment", "Waiting for the cloud agent to enroll. Check image prerequisites and outbound HTTPS access if startup stalls.")
	}
	node := a.Store.Nodes[grant.NodeID]
	credential := a.Store.NodeCredentials[grant.NodeID]
	if node == nil || node.OrgID != job.OrgID || credential == nil || credential.OrgID != job.OrgID {
		return stage("agent_identity_unavailable", "The enrolled machine or its credential is unavailable in this organization. Inspect the existing VM and restore its original identity.")
	}
	out.NodeID = node.ID
	if node.LastSeen.IsZero() {
		if !now.Before(deadline) {
			return stage("heartbeat_timed_out", "Enrollment succeeded but no heartbeat arrived before the deadline. Check the existing agent service and connectivity.")
		}
		return stage("awaiting_heartbeat", "Enrollment succeeded; waiting for the first authenticated heartbeat.")
	}
	seen := node.LastSeen
	out.LastSeen = &seen
	if node.Status != types.NodeOnline || now.Sub(seen) > time.Minute || seen.After(now.Add(time.Minute)) {
		return stage("agent_offline", "The enrolled agent has no fresh heartbeat. Reconnect the existing machine before continuing.")
	}
	if node.OS != "linux" || node.Arch != job.BootstrapConfig.Architecture {
		return stage("agent_incompatible", "The agent reported an OS or architecture that does not match the saved Linux bootstrap plan.")
	}
	return stage("agent_connected", "The enrolled agent is connected. Application health, data restore and routing still require validation before migration.")
}
