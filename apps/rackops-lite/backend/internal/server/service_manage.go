package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// ServiceUpdate is the editable subset of a service. Pointer fields distinguish
// "not provided" from zero values.
type ServiceUpdate struct {
	Image         *string         `json:"image,omitempty"`
	GitBranch     *string         `json:"gitBranch,omitempty"`
	Command       *string         `json:"command,omitempty"`
	Port          *int            `json:"port,omitempty"`
	ContainerPort *int            `json:"containerPort,omitempty"`
	Ports         *[]int          `json:"ports,omitempty"`
	Env           *[]types.EnvVar `json:"env,omitempty"`
	Mounts        *[]types.Mount  `json:"mounts,omitempty"`
	MemLimitMB    *int64          `json:"memLimitMb,omitempty"`
	CPULimit      *float64        `json:"cpuLimit,omitempty"`
	RestartPol    *string         `json:"restartPolicy,omitempty"`
	GPU           *bool           `json:"gpu,omitempty"`
	NodeID        *string         `json:"nodeId,omitempty"` // migration target
	Redeploy      bool            `json:"redeploy"`         // apply now (Save & Redeploy)
}

// handleUpdateService applies edits to a service. With Redeploy=true the
// container is recreated with the new config. Machine moves require a
// recoverable migration operation and are never performed by this endpoint.
func (a *API) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	svc, org, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !requireServiceControl(w, member) {
		return
	}
	var upd ServiceUpdate
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Reject before changing any fields, including a save-only request. The
	// old implementation changed the recorded node even without a redeploy,
	// and removed the source before checking whether the destination worked.
	if upd.NodeID != nil && *upd.NodeID != "" && *upd.NodeID != svc.NodeID {
		http.Error(w, "machine moves are unavailable until a verified migration and recovery plan can be created; this app remains on its current machine", http.StatusConflict)
		return
	}
	// A migration target must be the org's own hardware.
	if upd.NodeID != nil && *upd.NodeID != "" {
		ok := false
		a.Store.View(func() {
			n := a.Store.Nodes[*upd.NodeID]
			ok = n != nil && n.OrgID == org.ID
		})
		if !ok {
			http.Error(w, "machine not found in this organization", http.StatusNotFound)
			return
		}
	}
	// Adopted apps have no deploy recipe — redeploying would spawn a duplicate,
	// rackops-named container alongside the real one. Config edits can only be
	// saved (they never take effect); block the redeploy path outright.
	if svc.Adopted && upd.Redeploy {
		http.Error(w, "this app was adopted (started outside RackOps) and can't be rebuilt — use Restart to bounce it", http.StatusBadRequest)
		return
	}

	// Validate mounts before committing anything.
	if upd.Env != nil {
		var resolved []types.EnvVar
		var err error
		a.Store.View(func() { resolved, err = resolveEnvUpdate(svc.Env, *upd.Env) })
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upd.Env = &resolved
	}
	if upd.Mounts != nil {
		for _, m := range *upd.Mounts {
			if m.Target == "" || !strings.HasPrefix(m.Target, "/") {
				http.Error(w, "mount target must be an absolute container path: "+m.Target, http.StatusBadRequest)
				return
			}
			if m.Type == types.MountBind && !strings.HasPrefix(m.Source, "/") {
				http.Error(w, "bind mount source must be an absolute host path: "+m.Source, http.StatusBadRequest)
				return
			}
		}
	}

	a.Store.Mutate(func() {
		if upd.Image != nil {
			svc.Image = *upd.Image
		}
		if upd.GitBranch != nil {
			svc.GitBranch = *upd.GitBranch
		}
		if upd.Command != nil {
			svc.Command = *upd.Command
		}
		if upd.Port != nil {
			svc.Port = *upd.Port
		}
		if upd.ContainerPort != nil {
			svc.ContainerPort = *upd.ContainerPort
		}
		if upd.Ports != nil {
			svc.Ports = *upd.Ports
		}
		if upd.Env != nil {
			svc.Env = *upd.Env
		}
		if upd.Mounts != nil {
			svc.Mounts = *upd.Mounts
		}
		if upd.MemLimitMB != nil {
			svc.MemLimitMB = *upd.MemLimitMB
		}
		if upd.CPULimit != nil {
			svc.CPULimit = *upd.CPULimit
		}
		if upd.RestartPol != nil {
			svc.RestartPol = *upd.RestartPol
		}
		if upd.GPU != nil {
			svc.GPU = *upd.GPU
		}
		svc.UpdatedAt = time.Now()
	})

	if upd.Redeploy {
		a.Store.Mutate(func() { svc.Status = types.StatusBuilding })
		a.queueDeploy(svc, "config-change")
		a.Store.AddEvent("service", "Configuration applied, redeploying: "+svc.Name, svc.NodeID, svc.ID)
	} else {
		a.Store.AddEvent("service", "Configuration saved (pending redeploy): "+svc.Name, svc.NodeID, svc.ID)
	}
	a.serviceChanged(svc)
	writeJSON(w, svc)
}

// handleGetService returns one service with its git token stripped.
func (a *API) handleGetService(w http.ResponseWriter, r *http.Request) {
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	out := *svc
	out.GitToken = ""
	writeJSON(w, &out)
}

func (a *API) nodeName(id string) string {
	name := id
	a.Store.View(func() {
		if n := a.Store.Nodes[id]; n != nil {
			name = n.Name
		}
	})
	return name
}

// handleClearLogs sets the UI-level log horizon: subsequent fetches use
// --since this moment, so old output disappears without touching the
// container's on-disk log file.
func (a *API) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !requireServiceControl(w, member) {
		return
	}
	a.Store.Mutate(func() { svc.LogsClearedAt = time.Now() })
	writeJSON(w, map[string]string{"clearedAt": svc.LogsClearedAt.Format(time.RFC3339)})
}

// handleNodeFS lists a directory on a node's host filesystem (folder browser).
// Round-trips through the agent's task queue like log fetches do.
func (a *API) handleNodeFS(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	nodeID := r.PathValue("id")
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[nodeID] })
	if node == nil || node.OrgID != org.ID {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageMachines }) {
		http.Error(w, "machine management permission is required to browse host files", http.StatusForbidden)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	taskID := NewID()
	ch := make(chan string, 1)
	pendingLogs.Store(taskID, ch)
	defer pendingLogs.Delete(taskID)

	payload, _ := json.Marshal(DeployPayload{Path: path})
	a.Store.EnqueueTask(nodeID, types.AgentTask{
		ID: taskID, Kind: types.TaskListDir, Payload: string(payload), CreatedAt: time.Now(),
	})

	select {
	case out := <-ch:
		if strings.HasPrefix(out, "ERROR: ") {
			http.Error(w, strings.TrimPrefix(out, "ERROR: "), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(out))
	case <-time.After(10 * time.Second):
		http.Error(w, "timed out waiting for agent (node offline?)", http.StatusGatewayTimeout)
	case <-r.Context().Done():
	}
}
