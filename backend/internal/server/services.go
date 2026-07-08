package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/catalog"
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
	Ports       []int          `json:"ports,omitempty"`
	Command     string         `json:"command,omitempty"`
	Env         []types.EnvVar `json:"env"`
	Volumes     []string       `json:"volumes,omitempty"` // legacy template container paths
	Mounts      []types.Mount  `json:"mounts,omitempty"`
	PinnedPath  string         `json:"pinnedPath,omitempty"`
	MemLimitMB  int64          `json:"memLimitMb,omitempty"`
	CPULimit    float64        `json:"cpuLimit,omitempty"`
	RestartPol  string         `json:"restartPolicy,omitempty"`
	GPU         bool           `json:"gpu,omitempty"`
	HostNet     bool           `json:"hostNet,omitempty"`     // run with --network host (ingress edge)
	ContainerID string         `json:"containerId,omitempty"` // for stop/start/restart/remove
	Since       string         `json:"since,omitempty"`       // logs: RFC3339 lower bound
	Path        string         `json:"path,omitempty"`        // listdir: directory to list
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

	// Template deploys: expand the template into the service definition.
	var gpuNeeded bool
	if req.TemplateID != "" {
		tpl := catalog.Get(req.TemplateID)
		if tpl == nil {
			http.Error(w, "unknown template: "+req.TemplateID, http.StatusBadRequest)
			return
		}
		req.Kind = types.KindTemplate
		if tpl.Category == "database" {
			req.Kind = types.KindDatabase
		}
		req.Image = tpl.Image
		if req.Port == 0 {
			req.Port = tpl.Port
		}
		req.Ports = tpl.Ports
		req.Command = tpl.Command
		// Template volumes become editable managed mounts (visible on the
		// service's Storage tab, re-placeable onto a chosen drive).
		for _, cpath := range tpl.Volumes {
			req.Mounts = append(req.Mounts, types.Mount{
				Type:   types.MountVolume,
				Source: strings.ReplaceAll(strings.Trim(cpath, "/"), "/", "-"),
				Target: cpath,
			})
		}
		if req.MemLimitMB == 0 && tpl.MemMB > 0 {
			// Placement hint only; no hard container limit unless user set one.
			if req.Placement == nil {
				req.Placement = &types.PlacementRequest{}
			}
			if req.Placement.MemMB == 0 {
				req.Placement.MemMB = tpl.MemMB
			}
			req.Placement.NeedsGPU = req.Placement.NeedsGPU || tpl.GPU
		}
		gpuNeeded = tpl.GPU
		// Fill defaults + generate secrets for unset template env vars.
		provided := map[string]bool{}
		for _, e := range req.Env {
			provided[e.Key] = true
		}
		for _, def := range tpl.Env {
			if provided[def.Key] {
				continue
			}
			val := def.Default
			if def.Generate && val == "" {
				val = NewID() + NewID()
			}
			if val == "" && def.Required {
				http.Error(w, "missing required env: "+def.Key, http.StatusBadRequest)
				return
			}
			if val != "" {
				req.Env = append(req.Env, types.EnvVar{Key: def.Key, Value: val, Secret: def.Secret})
			}
		}
	}
	_ = gpuNeeded

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
		ComposeYML: svc.ComposeYML, Port: svc.Port, Ports: svc.Ports,
		Command: svc.Command, Env: svc.Env, Volumes: svc.Volumes, Mounts: svc.Mounts,
		PinnedPath: svc.PinnedPath, MemLimitMB: svc.MemLimitMB,
		CPULimit: svc.CPULimit, RestartPol: svc.RestartPol, GPU: svc.GPU,
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
	payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, ContainerID: svc.ContainerID, ComposeYML: svc.ComposeYML})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskRemove, ServiceID: svc.ID, Payload: string(payload), CreatedAt: time.Now(),
	})
	a.Store.Mutate(func() { delete(a.Store.Services, id) })
	a.Store.AddEvent("service", "Service removed: "+svc.Name, svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_removed", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
