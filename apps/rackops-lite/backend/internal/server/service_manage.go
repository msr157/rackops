package server

import (
	"encoding/json"
	"fmt"
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
// container is recreated with the new config; a node change migrates the
// workload (remove on old node, deploy on new).
func (a *API) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	svc, org, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	var upd ServiceUpdate
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	oldNode := svc.NodeID
	migrate := false
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
		if upd.NodeID != nil && *upd.NodeID != "" && *upd.NodeID != svc.NodeID {
			if a.Store.Nodes[*upd.NodeID] != nil {
				svc.NodeID = *upd.NodeID
				svc.AutoPlace = false
				svc.RelayPort = 0 // re-derived by ingress for the new node
				migrate = true
			}
		}
		svc.UpdatedAt = time.Now()
	})

	if upd.Redeploy {
		if migrate {
			// Tear down on the old node, then deploy on the new one.
			payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, ExternalName: svc.ExternalName, ContainerID: svc.ContainerID, ComposeYML: svc.ComposeYML})
			a.Store.EnqueueTask(oldNode, types.AgentTask{
				ID: NewID(), Kind: types.TaskRemove, ServiceID: svc.ID,
				Payload: string(payload), CreatedAt: time.Now(),
			})
			a.Store.AddEvent("migrate", fmt.Sprintf("Migrating %s: %s -> %s", svc.Name, a.nodeName(oldNode), a.nodeName(svc.NodeID)), svc.NodeID, svc.ID)
		}
		a.Store.Mutate(func() { svc.Status = types.StatusBuilding })
		a.queueDeploy(svc, "config-change")
		a.Store.AddEvent("service", "Configuration applied, redeploying: "+svc.Name, svc.NodeID, svc.ID)
	} else {
		a.Store.AddEvent("service", "Configuration saved (pending redeploy): "+svc.Name, svc.NodeID, svc.ID)
	}
	a.Hub.Broadcast("service_update", svc)
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
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[r.PathValue("id")] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	a.Store.Mutate(func() { svc.LogsClearedAt = time.Now() })
	writeJSON(w, map[string]string{"clearedAt": svc.LogsClearedAt.Format(time.RFC3339)})
}

// handleNodeFS lists a directory on a node's host filesystem (folder browser).
// Round-trips through the agent's task queue like log fetches do.
func (a *API) handleNodeFS(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[nodeID] })
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
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
