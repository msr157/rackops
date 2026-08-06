package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// pendingTunnels correlates tunnel-start task IDs to service IDs so the
// async result (the trycloudflare URL) lands on the right service.
var pendingTunnels sync.Map // taskID -> serviceID

// dashboardTunnelID is the pseudo service ID for the control plane's own
// quick tunnel. Container naming, task correlation, and the agent's tunnel
// report all treat it exactly like a service ID, so the whole service-tunnel
// pipeline (start, stop, auto-heal) is reused unchanged.
const dashboardTunnelID = "dashboard"

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

// handleDashboardTunnel starts or stops a quick tunnel for the dashboard
// itself — remote access to the control plane for installs behind NAT/CGNAT,
// where no port forwarding is possible. Runs on the control plane's own
// machine via its embedded local agent.
func (a *API) handleDashboardTunnel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID := a.localNodeID()

	if req.Enabled {
		if nodeID == "" {
			http.Error(w, "the control plane runs without a local agent (--no-local-agent), so it has no machine to run the tunnel container on", http.StatusBadRequest)
			return
		}
		port, _ := strconv.Atoi(a.listenPort())
		payload, _ := json.Marshal(DeployPayload{ServiceID: dashboardTunnelID, Name: "dashboard", Port: port})
		taskID := NewID()
		pendingTunnels.Store(taskID, dashboardTunnelID)
		a.Store.Mutate(func() { a.Store.Ingress.DashboardTunnel = true })
		a.Store.EnqueueTask(nodeID, types.AgentTask{
			ID: taskID, Kind: types.TaskTunnelStart, ServiceID: dashboardTunnelID,
			Payload: string(payload), CreatedAt: time.Now(),
		})
		a.Store.AddEvent("tunnel", "Starting a quick tunnel for the dashboard", nodeID, "")
		writeJSON(w, map[string]string{"status": "starting"})
		return
	}

	var cfg types.IngressConfig
	a.Store.Mutate(func() {
		a.Store.Ingress.DashboardTunnel = false
		a.Store.Ingress.DashboardTunnelURL = ""
		cfg = a.Store.Ingress
	})
	if nodeID != "" {
		payload, _ := json.Marshal(DeployPayload{ServiceID: dashboardTunnelID, Name: "dashboard"})
		a.Store.EnqueueTask(nodeID, types.AgentTask{
			ID: NewID(), Kind: types.TaskTunnelStop, ServiceID: dashboardTunnelID,
			Payload: string(payload), CreatedAt: time.Now(),
		})
	}
	a.Store.AddEvent("tunnel", "Dashboard quick tunnel stopped", nodeID, "")
	a.Hub.Broadcast("ingress_update", cfg)
	writeJSON(w, cfg)
}

// applyTunnelResult consumes a tunnel-start outcome; returns false when the
// task wasn't a tunnel start (so the caller keeps routing the result).
func (a *API) applyTunnelResult(nodeID string, res types.TaskResult) bool {
	v, ok := pendingTunnels.LoadAndDelete(res.TaskID)
	if !ok {
		return false
	}
	sid := v.(string)

	// The dashboard's own tunnel: the URL lands on the ingress config, and a
	// failed start flips the toggle back off — an enabled switch with no URL
	// and no retry would read as "still working on it" forever.
	if sid == dashboardTunnelID {
		var cfg types.IngressConfig
		a.Store.Mutate(func() {
			if res.OK && strings.HasPrefix(res.Log, "https://") {
				a.Store.Ingress.DashboardTunnelURL = res.Log
			} else if !res.OK {
				a.Store.Ingress.DashboardTunnel = false
			}
			cfg = a.Store.Ingress
		})
		if res.OK {
			a.Store.AddEvent("tunnel", "Dashboard is reachable from anywhere at "+res.Log+" (quick tunnel — the URL changes on restart)", nodeID, "")
		} else {
			a.Store.AddEvent("tunnel", "Dashboard quick tunnel failed: "+tailLog(res.Log, 160), nodeID, "")
		}
		a.Hub.Broadcast("ingress_update", cfg)
		return true
	}
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
