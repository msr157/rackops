package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// DeployPayload is the JSON blob sent to an agent inside a deploy task.
type DeployPayload struct {
	ServiceID   string         `json:"serviceId"`
	Name        string         `json:"name"`
	Image       string         `json:"image,omitempty"`
	GitRepo     string         `json:"gitRepo,omitempty"`
	GitBranch   string         `json:"gitBranch,omitempty"`
	GitToken    string         `json:"gitToken,omitempty"`
	ComposeYML  string         `json:"composeYml,omitempty"`
	Port        int            `json:"port"`
	Env         []types.EnvVar `json:"env"`
	Volumes     []string       `json:"volumes,omitempty"` // hostPath:containerPath
	PinnedPath  string         `json:"pinnedPath,omitempty"`
	MemLimitMB  int64          `json:"memLimitMb,omitempty"`
	ContainerID string         `json:"containerId,omitempty"` // for stop/start/restart/remove
}

// handleCreateService creates a service and queues its first deployment.
func (a *API) handleCreateService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		types.Service
		Placement *types.PlacementRequest `json:"placement,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.Kind == "" {
		req.Kind = types.KindApp
	}

	// Auto-placement: score nodes and pick the winner.
	if req.NodeID == "" || req.NodeID == "auto" {
		pr := types.PlacementRequest{}
		if req.Placement != nil {
			pr = *req.Placement
		}
		scores := a.Store.PlaceWorkload(pr)
		picked := ""
		for _, sc := range scores {
			if sc.Viable {
				picked = sc.NodeID
				break
			}
		}
		if picked == "" {
			http.Error(w, "no viable node for this workload", http.StatusConflict)
			return
		}
		req.NodeID = picked
		req.AutoPlace = true
	}

	svc := req.Service
	svc.ID = NewID()
	svc.Status = types.StatusBuilding
	svc.CreatedAt = time.Now()
	svc.UpdatedAt = time.Now()

	a.Store.Mutate(func() { a.Store.Services[svc.ID] = &svc })
	a.queueDeploy(&svc, "manual")

	nodeName := svc.NodeID
	a.Store.View(func() {
		if n := a.Store.Nodes[svc.NodeID]; n != nil {
			nodeName = n.Name
		}
	})
	placement := ""
	if svc.AutoPlace {
		placement = " (auto-placed)"
	}
	a.Store.AddEvent("deploy", fmt.Sprintf("Deploying %s to %s%s", svc.Name, nodeName, placement), svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_update", &svc)
	writeJSON(w, &svc)
}

// queueDeploy creates a Deployment record and enqueues the agent task.
// The deployment ID doubles as the agent task ID so results correlate.
func (a *API) queueDeploy(svc *types.Service, trigger string) *types.Deployment {
	version := 1
	a.Store.View(func() {
		for _, d := range a.Store.Deployments {
			if d.ServiceID == svc.ID && d.Version >= version {
				version = d.Version + 1
			}
		}
	})
	dep := &types.Deployment{
		ID: NewID(), ServiceID: svc.ID, Version: version,
		Status: types.StatusBuilding, Trigger: trigger, StartedAt: time.Now(),
	}
	a.Store.Mutate(func() { a.Store.Deployments[dep.ID] = dep })

	payload, _ := json.Marshal(DeployPayload{
		ServiceID: svc.ID, Name: svc.Name, Image: svc.Image,
		GitRepo: svc.GitRepo, GitBranch: svc.GitBranch, GitToken: svc.GitToken,
		ComposeYML: svc.ComposeYML, Port: svc.Port, Env: svc.Env,
		PinnedPath: svc.PinnedPath, MemLimitMB: svc.MemLimitMB,
	})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: dep.ID, Kind: types.TaskDeploy, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
	return dep
}

// handleServiceAction handles start/stop/restart/redeploy.
func (a *API) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[id] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	switch action {
	case "redeploy":
		a.Store.Mutate(func() { svc.Status = types.StatusBuilding })
		a.queueDeploy(svc, "manual")
	case "start", "stop", "restart":
		kind := map[string]types.TaskKind{
			"start": types.TaskStart, "stop": types.TaskStop, "restart": types.TaskRestart,
		}[action]
		payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, ContainerID: svc.ContainerID})
		a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
			ID: NewID(), Kind: kind, ServiceID: svc.ID, Payload: string(payload), CreatedAt: time.Now(),
		})
		if action == "stop" {
			a.Store.Mutate(func() { svc.Status = types.StatusStopped })
		}
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	a.Store.AddEvent("service", fmt.Sprintf("%s: %s requested", svc.Name, action), svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_update", svc)
	writeJSON(w, svc)
}

// handleDeleteService removes the service and asks the agent to tear down.
func (a *API) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[id] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, ContainerID: svc.ContainerID})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskRemove, ServiceID: svc.ID, Payload: string(payload), CreatedAt: time.Now(),
	})
	a.Store.Mutate(func() { delete(a.Store.Services, id) })
	a.Store.AddEvent("service", "Service removed: "+svc.Name, svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_removed", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
