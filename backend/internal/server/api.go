package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// API is the control plane HTTP server.
type API struct {
	Store *Store
	Hub   *Hub
	// DashFS is the embedded dashboard filesystem (nil = API only).
	DashFS fs.FS
	// JoinToken authorizes agents to register.
	JoinToken string
}

// Router builds the full HTTP mux.
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()

	// ---- Agent protocol ----
	mux.HandleFunc("POST /api/agent/ingest", a.handleIngest)

	// ---- Fleet ----
	mux.HandleFunc("GET /api/nodes", a.handleListNodes)
	mux.HandleFunc("GET /api/nodes/{id}", a.handleGetNode)
	mux.HandleFunc("GET /api/overview", a.handleOverview)
	mux.HandleFunc("GET /api/events", a.handleEvents)
	mux.HandleFunc("GET /api/alerts", a.handleAlerts)
	mux.HandleFunc("GET /api/join-command", a.handleJoinCommand)

	// ---- Projects / envs / services ----
	mux.HandleFunc("GET /api/projects", a.handleListProjects)
	mux.HandleFunc("POST /api/projects", a.handleCreateProject)
	mux.HandleFunc("GET /api/projects/{id}/envs", a.handleListEnvs)
	mux.HandleFunc("POST /api/projects/{id}/envs", a.handleCreateEnv)
	mux.HandleFunc("GET /api/services", a.handleListServices)
	mux.HandleFunc("POST /api/services", a.handleCreateService)
	mux.HandleFunc("POST /api/services/{id}/{action}", a.handleServiceAction)
	mux.HandleFunc("DELETE /api/services/{id}", a.handleDeleteService)

	// ---- Placement ----
	mux.HandleFunc("POST /api/placement", a.handlePlacement)

	// ---- Templates & logs ----
	mux.HandleFunc("GET /api/templates", a.handleTemplates)
	mux.HandleFunc("GET /api/services/{id}/logs", a.handleServiceLogs)
	mux.HandleFunc("GET /api/services/{id}/deployments", a.handleServiceDeployments)

	// ---- Live stream ----
	mux.Handle("GET /api/stream", a.Hub)

	// ---- Dashboard ----
	if a.DashFS != nil {
		mux.Handle("GET /", spaHandler(a.DashFS))
	}
	return mux
}

// ---- Agent ingest ----

func (a *API) handleIngest(w http.ResponseWriter, r *http.Request) {
	if a.JoinToken != "" && r.Header.Get("X-Rackops-Token") != a.JoinToken {
		http.Error(w, "invalid join token", http.StatusUnauthorized)
		return
	}
	var rep types.AgentReport
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var node *types.Node
	isNew := false
	a.Store.Mutate(func() {
		node = a.Store.Nodes[rep.NodeID]
		if node == nil {
			isNew = true
			node = &types.Node{
				ID:       rep.NodeID,
				Role:     types.RoleWorker,
				JoinedAt: time.Now(),
			}
			a.Store.Nodes[rep.NodeID] = node
		}
		node.Name = rep.Name
		node.Status = types.NodeOnline
		node.LastSeen = time.Now()
		node.Hardware = rep.Hardware
		node.Metrics = rep.Metrics
		node.Hostname = rep.Host.Hostname
		node.OS = rep.Host.OS
		node.Arch = rep.Host.Arch
		node.Kernel = rep.Host.Kernel
		node.IP = rep.Host.IP
		node.PublicIP = rep.Host.PublicIP
		node.HasPublic = rep.Host.PublicIP != ""
		node.AgentVer = rep.AgentVer
		node.Metrics.Containers = len(rep.Containers)

		// Sync container states onto services.
		for _, cs := range rep.Containers {
			if cs.ServiceID == "" {
				continue
			}
			if svc := a.Store.Services[cs.ServiceID]; svc != nil {
				svc.ContainerID = cs.ID
				svc.CPUPct = cs.CPUPct
				svc.MemUsedMB = cs.MemUsedMB
				if svc.Status == types.StatusRunning && cs.State != "running" {
					svc.Status = types.StatusFailed
				}
				if cs.State == "running" && (svc.Status == types.StatusPending || svc.Status == types.StatusStopped) {
					svc.Status = types.StatusRunning
				}
			}
		}
	})

	// Process task results.
	for _, res := range rep.TaskResults {
		a.applyTaskResult(rep.NodeID, res)
	}

	if isNew {
		a.Store.AddEvent("node_joined", fmt.Sprintf("Node %s joined the fleet (%s)", node.Name, node.Hardware.CPUModel), node.ID, "")
		a.Hub.Broadcast("node_joined", node)
	}

	// Alerts.
	for _, alert := range a.Store.EvaluateAlerts(node) {
		a.Store.AddEvent("alert", alert.Message, node.ID, "")
		a.Hub.Broadcast("alert", alert)
	}

	a.Hub.Broadcast("node_metrics", node)

	resp := types.AgentResponse{Tasks: a.Store.DrainTasks(rep.NodeID)}
	writeJSON(w, resp)
}

// applyTaskResult updates service state from an agent's task outcome.
func (a *API) applyTaskResult(nodeID string, res types.TaskResult) {
	// Log fetches route straight to the waiting HTTP request.
	if deliverLogs(res.TaskID, res.Log) {
		return
	}
	var svc *types.Service
	var dep *types.Deployment
	a.Store.Mutate(func() {
		for _, d := range a.Store.Deployments {
			if d.ID == res.TaskID {
				dep = d
				break
			}
		}
		if dep != nil {
			svc = a.Store.Services[dep.ServiceID]
			dep.FinishedAt = time.Now()
			dep.DurationMS = time.Since(dep.StartedAt).Milliseconds()
			dep.Log = res.Log
			if res.OK {
				dep.Status = types.StatusRunning
			} else {
				dep.Status = types.StatusFailed
			}
		}
		if svc != nil {
			if res.OK {
				svc.Status = types.StatusRunning
				svc.ContainerID = res.ContainerID
			} else {
				svc.Status = types.StatusFailed
			}
			svc.UpdatedAt = time.Now()
		}
	})
	if svc != nil {
		verb := "deployed"
		if !res.OK {
			verb = "failed to deploy"
		}
		a.Store.AddEvent("deploy", fmt.Sprintf("Service %s %s on node %s", svc.Name, verb, nodeID), nodeID, svc.ID)
		a.Hub.Broadcast("service_update", svc)
	}
}

// ---- Fleet handlers ----

func (a *API) handleListNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.Store.SortedNodes())
}

func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[r.PathValue("id")] })
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	writeJSON(w, node)
}

// handleOverview aggregates cluster-wide KPIs.
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	type overview struct {
		NodesOnline   int     `json:"nodesOnline"`
		NodesTotal    int     `json:"nodesTotal"`
		ServicesUp    int     `json:"servicesUp"`
		ServicesTotal int     `json:"servicesTotal"`
		TotalMemMB    int64   `json:"totalMemMb"`
		UsedMemMB     int64   `json:"usedMemMb"`
		TotalStoreGB  float64 `json:"totalStorageGb"`
		UsedStoreGB   float64 `json:"usedStorageGb"`
		AvgCPUPct     float64 `json:"avgCpuPct"`
		OpenAlerts    int     `json:"openAlerts"`
	}
	var o overview
	a.Store.View(func() {
		var cpuSum float64
		for _, n := range a.Store.Nodes {
			o.NodesTotal++
			if n.Status == types.NodeOnline {
				o.NodesOnline++
				cpuSum += n.Metrics.CPUPct
			}
			o.TotalMemMB += n.Hardware.MemTotalMB
			o.UsedMemMB += n.Metrics.MemUsedMB
			for _, d := range n.Hardware.Drives {
				o.TotalStoreGB += d.SizeGB
				o.UsedStoreGB += d.UsedGB
			}
		}
		if o.NodesOnline > 0 {
			o.AvgCPUPct = round1(cpuSum / float64(o.NodesOnline))
		}
		for _, s := range a.Store.Services {
			o.ServicesTotal++
			if s.Status == types.StatusRunning {
				o.ServicesUp++
			}
		}
		for _, al := range a.Store.Alerts {
			if !al.Resolved {
				o.OpenAlerts++
			}
		}
	})
	writeJSON(w, o)
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	var evs []*types.Event
	a.Store.View(func() {
		evs = append(evs, a.Store.Events...)
	})
	// newest first
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	writeJSON(w, evs)
}

func (a *API) handleAlerts(w http.ResponseWriter, r *http.Request) {
	var alerts []*types.Alert
	a.Store.View(func() {
		for _, al := range a.Store.Alerts {
			if !al.Resolved || r.URL.Query().Get("all") == "true" {
				alerts = append(alerts, al)
			}
		}
	})
	writeJSON(w, alerts)
}

func (a *API) handleJoinCommand(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	writeJSON(w, map[string]string{
		"command": fmt.Sprintf("rackops agent --server http://%s --token %s", host, a.JoinToken),
		"token":   a.JoinToken,
	})
}

// ---- Project / env / service handlers ----

func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	var out []*types.Project
	a.Store.View(func() {
		for _, p := range a.Store.Projects {
			out = append(out, p)
		}
	})
	writeJSON(w, out)
}

func (a *API) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	p := &types.Project{ID: NewID(), Name: req.Name, Description: req.Description, CreatedAt: time.Now()}
	a.Store.Mutate(func() {
		a.Store.Projects[p.ID] = p
		for _, env := range []string{"production", "staging", "development"} {
			e := &types.Environment{ID: NewID(), ProjectID: p.ID, Name: env, CreatedAt: time.Now()}
			a.Store.Envs[e.ID] = e
		}
	})
	a.Store.AddEvent("project", "Project created: "+p.Name, "", "")
	writeJSON(w, p)
}

func (a *API) handleListEnvs(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	var out []*types.Environment
	a.Store.View(func() {
		for _, e := range a.Store.Envs {
			if e.ProjectID == pid {
				out = append(out, e)
			}
		}
	})
	writeJSON(w, out)
}

func (a *API) handleCreateEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	e := &types.Environment{ID: NewID(), ProjectID: r.PathValue("id"), Name: req.Name, CreatedAt: time.Now()}
	a.Store.Mutate(func() { a.Store.Envs[e.ID] = e })
	writeJSON(w, e)
}

func (a *API) handleListServices(w http.ResponseWriter, r *http.Request) {
	var out []*types.Service
	proj, env := r.URL.Query().Get("project"), r.URL.Query().Get("env")
	a.Store.View(func() {
		for _, s := range a.Store.Services {
			if proj != "" && s.ProjectID != proj {
				continue
			}
			if env != "" && s.EnvID != env {
				continue
			}
			out = append(out, s)
		}
	})
	writeJSON(w, out)
}

func (a *API) handlePlacement(w http.ResponseWriter, r *http.Request) {
	var req types.PlacementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, a.Store.PlaceWorkload(req))
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// spaHandler serves the embedded dashboard with SPA fallback to index.html.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/" // SPA fallback
		}
		fileServer.ServeHTTP(w, r)
	})
}
