package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// pendingTunnels correlates tunnel-start task IDs to service IDs so the
// async result (the trycloudflare URL) lands on the right service.
var pendingTunnels sync.Map // taskID -> serviceID

// handleStartQuickTunnel launches a Cloudflare quick tunnel for a service:
// a zero-account, zero-DNS public URL for TESTING (random on each restart,
// bandwidth-limited by Cloudflare, not for production traffic).
func (a *API) handleStartQuickTunnel(w http.ResponseWriter, r *http.Request) {
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[r.PathValue("id")] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	if svc.Port == 0 {
		http.Error(w, "service has no HTTP port to tunnel", http.StatusBadRequest)
		return
	}
	payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, Port: svc.Port})
	taskID := NewID()
	pendingTunnels.Store(taskID, svc.ID)
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: taskID, Kind: types.TaskTunnelStart, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
	a.Store.AddEvent("tunnel", "Starting quick tunnel for "+svc.Name, svc.NodeID, svc.ID)
	writeJSON(w, map[string]string{"status": "starting"})
}

// handleStopQuickTunnel tears the tunnel down and clears the URL.
func (a *API) handleStopQuickTunnel(w http.ResponseWriter, r *http.Request) {
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[r.PathValue("id")] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskTunnelStop, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
	a.Store.Mutate(func() { svc.QuickTunnelURL = "" })
	a.Store.AddEvent("tunnel", "Quick tunnel stopped for "+svc.Name, svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_update", svc)
	w.WriteHeader(http.StatusNoContent)
}

// applyTunnelResult consumes a tunnel-start outcome; returns false when the
// task wasn't a tunnel start (so the caller keeps routing the result).
func (a *API) applyTunnelResult(nodeID string, res types.TaskResult) bool {
	v, ok := pendingTunnels.LoadAndDelete(res.TaskID)
	if !ok {
		return false
	}
	sid := v.(string)
	var svc *types.Service
	a.Store.Mutate(func() {
		svc = a.Store.Services[sid]
		if svc != nil && res.OK && strings.HasPrefix(res.Log, "https://") {
			svc.QuickTunnelURL = res.Log
		}
	})
	if svc == nil {
		return true
	}
	if res.OK {
		a.Store.AddEvent("tunnel", svc.Name+" is publicly reachable at "+res.Log+" (quick tunnel — testing)", nodeID, svc.ID)
	} else {
		a.Store.AddEvent("tunnel", "Quick tunnel failed for "+svc.Name+": "+tailLog(res.Log, 160), nodeID, svc.ID)
	}
	a.Hub.Broadcast("service_update", svc)
	return true
}
