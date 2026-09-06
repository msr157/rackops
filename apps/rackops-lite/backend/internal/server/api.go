package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/raftstore"
	"github.com/rackops/rackops/internal/types"
)

// API is the control plane HTTP server.
type API struct {
	nodeEnrollmentMu   sync.Mutex
	cloudIdentityCheck func(context.Context, string, string, json.RawMessage) (cloud.Identity, error)
	cloudOperationMu   sync.Mutex
	awsIdentityCheck   func(context.Context, string, cloud.AWSCredentials) (cloud.Identity, error)
	awsMachineCheck    func(context.Context, string, cloud.AWSCredentials, cloud.AWSMachineSpec, string) (cloud.MachineInspection, error)
	cloudMachineCheck  func(context.Context, string, string, json.RawMessage, cloud.MachineSpec, string) (cloud.MachineInspection, error)
	deliveryMu         sync.Mutex
	resetDeliveryMu    sync.Mutex
	Store              *Store
	Hub                *Hub
	// History records downsampled metric rings for charts and deltas
	// (nil = endpoints answer with empty series; ingest skips recording).
	History *History
	// Mesh is the WireGuard hub (nil = mesh disabled).
	Mesh *MeshHub
	// DashFS is the embedded dashboard filesystem (nil = API only).
	DashFS fs.FS
	// JoinToken authorizes agents to register.
	JoinToken string
	// InstallID anonymously identifies this install to the vendor CRM
	// (support tickets, reset-email relay). Random UUID, minted once.
	InstallID string
	// DataDir is the server's state directory — shared with the embedded
	// local agent, whose node-id file identifies the control plane's own
	// machine in the fleet (see localNodeID).
	DataDir string
	// Cluster is the raft layer (nil = single-server).
	Cluster *raftstore.Cluster
	// FrontDoorPOP is the vendor edge installs dial (host:port). Empty
	// disables the feature: a self-hosted install with no vendor
	// relationship should not offer a button that cannot work.
	FrontDoorPOP string
	// FrontDoorZone is the DNS suffix public hostnames are minted under.
	FrontDoorZone string
	// FrontDoorAdmin is the base URL of the edge's admin API, used to
	// enroll this install and register its hostnames.
	FrontDoorAdmin string
	// FrontDoorToken authenticates to that admin API.
	FrontDoorToken string
	// applyGuard applies replicated state on followers only.
	applyGuard func([]byte)
}

// localNodeID returns the node ID of the control plane's own machine. The
// embedded local agent persists it at <dataDir>/node-id; empty when the
// server runs with --no-local-agent or before the agent's first start. Read
// per call (no cache): it's one small cached-by-the-OS file, and the first
// calls can race the agent's start.
func (a *API) localNodeID() string {
	if a.DataDir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(a.DataDir, "node-id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Router builds the full HTTP mux.
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()

	// Every documented endpoint registers from apiRoutes — the same table
	// /api/openapi.json is generated from, so code and docs cannot drift.
	for _, rt := range a.apiRoutes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}

	// Non-REST surfaces (streams, proxies, docs UI) register directly.
	// ---- Live stream (SSE, org-scoped) ----
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		org, _ := a.requireOrg(w, r)
		if org == nil {
			return
		}
		a.Hub.ServeFiltered(w, r, org.ID, func(message []byte) bool { return a.eventVisible(r, org.ID, message) })
	})

	// ---- Mesh relay proxy ----
	mux.HandleFunc("/relay/{id}/{rest...}", a.handleRelay)

	// ---- API docs ----
	mux.HandleFunc("GET /api/openapi.json", a.handleOpenAPI)
	mux.HandleFunc("GET /swagger", a.handleSwagger)
	mux.HandleFunc("GET /swagger/{asset}", a.handleSwaggerAsset)

	// ---- Liveness ----
	// Deliberately unauthenticated: the Docker HEALTHCHECK and Swarm's
	// rolling-update gate probe this before routing traffic to a new task,
	// and neither can present credentials.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{
			"status":  "ok",
			"service": "rackops-control-plane",
			"version": ServerVersion,
		})
	})

	// ---- Dashboard ----
	if a.DashFS != nil {
		mux.Handle("/", spaHandler(a.DashFS))
	}
	return a.clusterGuard(a.authMiddleware(mux))
}

// ---- Agent ingest ----

func (a *API) handleIngest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var rep types.AgentReport
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !a.agentAuthorized(rep.NodeID, r.Header.Get("X-Rackops-Token")) {
		http.Error(w, "invalid node credential", http.StatusUnauthorized)
		return
	}

	inventoryValid := rep.ContainersValid == nil || *rep.ContainersValid
	if !inventoryValid {
		rep.Containers = nil
	}
	var node *types.Node
	isNew := false
	authorized := false
	a.Store.Mutate(func() {
		if !a.agentAuthorizedLocked(rep.NodeID, r.Header.Get("X-Rackops-Token")) {
			return
		}
		authorized = true
		node = a.Store.Nodes[rep.NodeID]
		if node != nil && node.Status == types.NodeJoining {
			isNew = true
		}
		if node == nil {
			isNew = true
			node = &types.Node{
				ID:       rep.NodeID,
				Role:     types.RoleWorker,
				JoinedAt: time.Now(),
			}
			a.Store.Nodes[rep.NodeID] = node
		}
		// Org routing: honor the org the agent was joined with on first sight
		// (join-command carries --org); everything else lands in the default
		// org. Membership never changes on later reports — moving a machine
		// between orgs is an explicit admin action, not an agent restart.
		if node.OrgID == "" {
			if rep.OrgID != "" && a.Store.Orgs[rep.OrgID] != nil {
				node.OrgID = rep.OrgID
			} else {
				// Inline default-org lookup (we already hold the store lock).
				for id, o := range a.Store.Orgs {
					if node.OrgID == "" || o.CreatedAt.Before(a.Store.Orgs[node.OrgID].CreatedAt) {
						node.OrgID = id
					}
				}
			}
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
			if cs.ServiceID == "" || cs.Tunnel {
				continue // tunnel helpers must not drive service status
			}
			// The Traefik edge is a pseudo-service: track its liveness as
			// ingress health so a crash-loop (e.g. port conflict) surfaces
			// instead of showing "running" forever.
			if cs.ServiceID == ingressServiceID {
				if a.Store.Ingress.NodeID == rep.NodeID {
					if cs.State == "running" {
						if a.Store.Ingress.Status != "running" {
							a.Store.Ingress.Status = "running"
							a.Store.Ingress.StatusMsg = ""
						}
					} else if a.Store.Ingress.Enabled {
						a.Store.Ingress.Status = "failed"
						if a.Store.Ingress.StatusMsg == "" {
							a.Store.Ingress.StatusMsg = "edge container is " + cs.State + " (check its logs — often a port conflict)"
						}
					}
				}
				continue
			}
			if svc := a.Store.Services[cs.ServiceID]; svc != nil && svc.NodeID == rep.NodeID {
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

	if !authorized {
		http.Error(w, "node credential was revoked", http.StatusUnauthorized)
		return
	}
	// Fold this report into the metric history rings (charts, deltas).
	a.History.Observe(rep.NodeID, rep.Metrics, rep.Containers)

	// Process task results.
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	var acknowledged []string
	for _, res := range rep.TaskResults {
		if rep.DurableTasks {
			if a.acceptTaskResult(rep.NodeID, res) {
				acknowledged = append(acknowledged, res.TaskID)
			}
		} else {
			a.applyTaskResult(rep.NodeID, res)
		}
	}

	// Adopt containers that were already running on the node (created outside
	// RackOps) so pre-existing workloads show up and become manageable.
	if inventoryValid {
		a.adoptExternal(rep.NodeID, rep.Containers)
	}

	// Reconcile: the agent's report is ground truth. A service marked running
	// whose container is GONE (not merely stopped — absent) gets flagged
	// failed and auto-healed; a tunnel whose container vanished is restarted.
	// A sample collected before the reported command finished is not evidence
	// that its replacement is missing. Wait for the next inventory report.
	if inventoryValid && len(rep.TaskResults) == 0 {
		a.reconcileNode(rep.NodeID, rep.Containers)
	}

	if isNew {
		a.Store.AddEvent("node_joined", fmt.Sprintf("Node %s joined the fleet (%s)", node.Name, node.Hardware.CPUModel), node.ID, "")
		a.Hub.BroadcastOrg(node.OrgID, "node_joined", node)
	}

	// Alerts: broadcast new ones AND resolutions — the UI drops a resolved
	// alert from "Needs attention" the moment the condition clears.
	raised, resolved := a.Store.EvaluateAlerts(node)
	for _, alert := range raised {
		a.Store.AddEvent("alert", alert.Message, node.ID, "")
		a.Hub.BroadcastOrg(node.OrgID, "alert", alert)
		a.NotifyAlert(alert)
	}
	for _, alert := range resolved {
		a.Store.AddEvent("alert", "Resolved: "+alert.Message, node.ID, "")
		a.Hub.BroadcastOrg(node.OrgID, "alert_resolved", alert)
	}

	a.Hub.BroadcastOrg(node.OrgID, "node_metrics", node)

	a.applyFrontDoorReport(rep.NodeID, rep.FrontDoor)

	limit := -1
	if rep.TaskSlots != nil {
		limit = max(0, min(*rep.TaskSlots, 64))
	}
	var tasks []types.AgentTask
	if rep.DurableTasks {
		var err error
		tasks, err = a.Store.deliverTasks(rep.NodeID, max(0, limit), time.Now())
		if err != nil {
			http.Error(w, "task state could not be saved; agent will retry", http.StatusServiceUnavailable)
			return
		}
	} else {
		tasks = a.Store.DrainTasksLimit(rep.NodeID, limit)
	}
	resp := types.AgentResponse{
		DurableTasks:      true,
		Tasks:             tasks,
		AcknowledgedTasks: acknowledged,
		// The tunnel assignment is desired state, re-asserted on every
		// report rather than queued as a task: a task would be delivered
		// once and lost across an agent restart, leaving the tunnel down
		// until something else happened to change.
		FrontDoor: a.frontDoorSpecFor(rep.NodeID),
	}
	if a.Mesh != nil {
		if m := a.Mesh.HandleExchange(rep.NodeID, rep.Mesh); m != nil {
			// Tell the agent which service ports to expose on the mesh.
			a.Store.View(func() {
				for _, svc := range a.Store.Services {
					if svc.NodeID == rep.NodeID && svc.Port > 0 {
						m.ExposePorts = append(m.ExposePorts, svc.Port)
					}
				}
			})
			// If this node is the ingress edge, send relay specs for every
			// domain-routed service living on OTHER nodes.
			m.Relays = a.relaySpecsFor(rep.NodeID)
			resp.Mesh = m
			if node.MeshIP != m.MeshIP {
				a.Store.Mutate(func() { node.MeshIP = m.MeshIP })
			}
		}
	}
	writeJSON(w, resp)
}

// healState rate-limits auto-healing per service/tunnel/edge: at most one
// attempt per 2 minutes and 5 consecutive attempts total, so a genuinely
// broken workload becomes a visible failure instead of a silent retry loop.
// The counter resets whenever the container is seen alive again.
type healRecord struct {
	attempts int
	last     time.Time
}

var healState sync.Map // key -> healRecord

func healAllowed(key string) bool {
	now := time.Now()
	if v, ok := healState.Load(key); ok {
		rec := v.(healRecord)
		if rec.attempts >= 5 || now.Sub(rec.last) < 2*time.Minute {
			return false
		}
		rec.attempts++
		rec.last = now
		healState.Store(key, rec)
		return true
	}
	healState.Store(key, healRecord{attempts: 1, last: now})
	return true
}

func healExhausted(key string) bool {
	if v, ok := healState.Load(key); ok {
		return v.(healRecord).attempts >= 5
	}
	return false
}

func healReset(key string) { healState.Delete(key) }

// reconcileNode compares desired state to the agent's reported containers.
// Absent-but-should-run services are marked failed and redeployed (with a
// cooldown); vanished quick tunnels are restarted; a missing Traefik edge is
// redeployed. This is what keeps workloads up across reboots, manual
// `docker rm`, and crashed daemons — and keeps the UI honest meanwhile.
func (a *API) reconcileNode(nodeID string, reported []types.ContainerStatus) {
	present := map[string]bool{}
	tunnels := map[string]bool{}
	for _, cs := range reported {
		if cs.ServiceID == "" {
			continue
		}
		if cs.Tunnel {
			tunnels[cs.ServiceID] = true
		} else {
			present[cs.ServiceID] = true
		}
	}

	var flipped []*types.Service    // was running, container just vanished
	var missing []*types.Service    // already failed, still absent (quiet retry)
	var tunnelLost []*types.Service // URL assigned but cloudflared gone
	edgeMissing := false
	a.Store.Mutate(func() {
		for _, svc := range a.Store.Services {
			if svc.NodeID != nodeID {
				continue
			}
			if a.Store.taskBlocksHealing(nodeID, svc.ID) {
				continue
			}
			// Compose stacks aren't label-tracked (compose owns their
			// lifecycle); skip them to avoid false positives.
			if svc.ComposeYML != "" {
				continue
			}
			// Adopted services have no deploy recipe and aren't rackops-labelled;
			// their liveness is tracked by adoptExternal, never auto-healed here.
			if svc.Adopted {
				continue
			}
			if present[svc.ID] {
				healReset(svc.ID) // container alive again → fresh heal budget
				continue
			}
			// Absent container: running services get flagged immediately;
			// failed ones keep retrying quietly (bounded by the heal budget).
			switch svc.Status {
			case types.StatusRunning:
				svc.Status = types.StatusFailed
				svc.UpdatedAt = time.Now()
				flipped = append(flipped, svc)
			case types.StatusFailed:
				missing = append(missing, svc)
			}
		}
		for _, svc := range a.Store.Services {
			if svc.NodeID == nodeID && svc.QuickTunnelURL != "" && !tunnels[svc.ID] && !a.Store.taskBlocksHealing(nodeID, svc.ID) {
				tunnelLost = append(tunnelLost, svc)
			}
		}
		if a.Store.Ingress.Enabled && a.Store.Ingress.NodeID == nodeID && !present[ingressServiceID] && !a.Store.taskBlocksHealing(nodeID, ingressServiceID) {
			if a.Store.Ingress.Status == "running" {
				a.Store.Ingress.Status = "failed"
				a.Store.Ingress.StatusMsg = "edge container disappeared — redeploying"
			}
			edgeMissing = true
		} else if present[ingressServiceID] {
			healReset("edge")
		}
	})

	// Just-vanished services: always announce the failure, heal if budget allows.
	for _, svc := range flipped {
		if healAllowed(svc.ID) {
			a.Store.Mutate(func() { svc.Status = types.StatusBuilding })
			a.queueDeploy(svc, "auto-heal")
			a.Store.AddEvent("service", fmt.Sprintf("%s: container missing — auto-healing (redeploy queued)", svc.Name), nodeID, svc.ID)
		} else if healExhausted(svc.ID) {
			a.Store.AddEvent("service", fmt.Sprintf("%s: container missing; auto-heal budget exhausted — fix the cause and hit Redeploy", svc.Name), nodeID, svc.ID)
		} else {
			a.Store.AddEvent("service", fmt.Sprintf("%s: container missing (auto-heal retries shortly)", svc.Name), nodeID, svc.ID)
		}
		a.serviceChanged(svc)
	}
	// Still-absent failed services: retry quietly on the heal budget's cadence.
	for _, svc := range missing {
		if !healAllowed(svc.ID) {
			continue
		}
		a.Store.Mutate(func() { svc.Status = types.StatusBuilding })
		a.queueDeploy(svc, "auto-heal")
		a.Store.AddEvent("service", fmt.Sprintf("%s: retrying auto-heal (redeploy queued)", svc.Name), nodeID, svc.ID)
		a.serviceChanged(svc)
	}

	for _, svc := range tunnelLost {
		if !healAllowed("qt-" + svc.ID) {
			continue
		}
		payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, Port: svc.Port})
		taskID := NewID()
		pendingTunnels.Store(taskID, svc.ID)
		a.Store.EnqueueTask(nodeID, types.AgentTask{
			ID: taskID, Kind: types.TaskTunnelStart, ServiceID: svc.ID,
			Payload: string(payload), CreatedAt: time.Now(),
		})
		a.Store.AddEvent("tunnel", svc.Name+": quick tunnel disappeared — restarting (URL will change)", nodeID, svc.ID)
	}

	// The dashboard's own quick tunnel heals exactly like a service tunnel:
	// operator turned it on, the cloudflared container vanished from the
	// control-plane machine → restart it (the URL rotates).
	if nodeID == a.localNodeID() {
		dashLost := false
		a.Store.View(func() {
			dashLost = a.Store.Ingress.DashboardTunnel &&
				a.Store.Ingress.DashboardTunnelURL != "" && !tunnels[dashboardTunnelID] && !a.Store.taskBlocksHealing(nodeID, dashboardTunnelID)
		})
		if dashLost && healAllowed("qt-"+dashboardTunnelID) {
			port, _ := strconv.Atoi(a.listenPort())
			payload, _ := json.Marshal(DeployPayload{ServiceID: dashboardTunnelID, Name: "dashboard", Port: port})
			taskID := NewID()
			pendingTunnels.Store(taskID, dashboardTunnelID)
			a.Store.EnqueueTask(nodeID, types.AgentTask{
				ID: taskID, Kind: types.TaskTunnelStart, ServiceID: dashboardTunnelID,
				Payload: string(payload), CreatedAt: time.Now(),
			})
			a.Store.AddEvent("tunnel", "Dashboard quick tunnel disappeared — restarting (URL will change)", nodeID, "")
		}
	}

	if edgeMissing && healAllowed("edge") {
		var cfg types.IngressConfig
		a.Store.View(func() { cfg = a.Store.Ingress })
		a.deployTraefik(cfg)
		a.Store.AddEvent("ingress", "Edge (Traefik) container missing — auto-healing", nodeID, "")
		a.Hub.Broadcast("ingress_update", cfg)
	}
}

// adoptSuppress holds container names recently removed via the UI, keyed
// "nodeID/name". Deleting an adopted service removes its service record and
// queues a container removal, but the container lingers for a report cycle or
// two until the agent tears it down — without this, adoptExternal would
// re-adopt it in that gap, leaving a ghost service pointing at a dead
// container. Entries expire after a short grace period.
var adoptSuppress sync.Map // "nodeID/name" -> time.Time

const adoptSuppressTTL = 2 * time.Minute

func suppressAdopt(nodeID, name string) {
	if name == "" {
		return
	}
	adoptSuppress.Store(nodeID+"/"+name, time.Now())
}

func adoptSuppressed(nodeID, name string) bool {
	v, ok := adoptSuppress.Load(nodeID + "/" + name)
	if !ok {
		return false
	}
	if time.Since(v.(time.Time)) > adoptSuppressTTL {
		adoptSuppress.Delete(nodeID + "/" + name)
		return false
	}
	return true
}

// adoptExternal turns containers already running on a node (created outside
// RackOps) into managed "adopted" services, and keeps their status/stats in
// sync on every report. Matching is by (node, container name) — a container's
// name is stable across restarts, its ID is not. Adopted services are never
// rebuilt or auto-healed (RackOps has no recipe for them); the user can
// start/stop/restart/remove them and read their logs.
func (a *API) adoptExternal(nodeID string, reported []types.ContainerStatus) {
	var created, updated []*types.Service
	a.Store.Mutate(func() {
		// Index this node's existing adopted services by their container name.
		existing := map[string]*types.Service{}
		for _, svc := range a.Store.Services {
			if svc.NodeID == nodeID && svc.Adopted && svc.ExternalName != "" {
				existing[svc.ExternalName] = svc
			}
		}
		for _, cs := range reported {
			if !cs.External || cs.Name == "" {
				continue
			}
			// Skip containers a UI delete just removed — avoids re-adopting a
			// container mid-teardown into a ghost service.
			if adoptSuppressed(nodeID, cs.Name) {
				continue
			}
			status := types.StatusStopped
			if cs.State == "running" {
				status = types.StatusRunning
			}
			if svc := existing[cs.Name]; svc != nil {
				changed := svc.Status != status || svc.ContainerID != cs.ID
				svc.Status = status
				svc.ContainerID = cs.ID
				svc.CPUPct = cs.CPUPct
				svc.MemUsedMB = cs.MemUsedMB
				if cs.Image != "" {
					svc.Image = cs.Image
				}
				if cs.HostPort > 0 && svc.Port == 0 {
					svc.Port = cs.HostPort
				}
				if changed {
					svc.UpdatedAt = time.Now()
					updated = append(updated, svc)
				}
				continue
			}
			projID, envID := a.Store.defaultPlacement()
			svc := &types.Service{
				ID: NewID(), ProjectID: projID, EnvID: envID,
				Name: cs.Name, Kind: types.KindApp, Status: status,
				Image: cs.Image, NodeID: nodeID,
				Adopted: true, ExternalName: cs.Name,
				Port: cs.HostPort, Ingress: types.IngressNone,
				ContainerID: cs.ID, CPUPct: cs.CPUPct, MemUsedMB: cs.MemUsedMB,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			a.Store.Services[svc.ID] = svc
			created = append(created, svc)
		}
	})
	for _, svc := range created {
		a.Store.AddEvent("service", fmt.Sprintf("Adopted %q — a container already running on %s, now manageable here", svc.ExternalName, a.nodeName(nodeID)), nodeID, svc.ID)
		a.serviceChanged(svc)
	}
	for _, svc := range updated {
		a.serviceChanged(svc)
	}
}

// applyTaskResult updates service state from an agent's task outcome.
func (a *API) applyTaskResult(nodeID string, res types.TaskResult) {
	// Log fetches route straight to the waiting HTTP request.
	if deliverLogs(res.TaskID, res.Log) {
		return
	}
	// Quick-tunnel starts resolve to a service URL, not a deployment.
	if a.applyTunnelResult(nodeID, res) {
		return
	}
	// The Traefik edge is a pseudo-service with no Deployment record; its
	// result feeds the ingress health status instead.
	if res.ServiceID == ingressServiceID {
		a.applyIngressResult(nodeID, res)
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
			// The authoritative final log; trim huge build output for storage
			// (the live stream already showed it all in real time).
			finalLog := res.Log
			if len(finalLog) > 20_000 {
				finalLog = "…(earlier output truncated — it streamed live during the build)…\n" + finalLog[len(finalLog)-20_000:]
			}
			dep.Log = finalLog
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
				// The agent bumps to a free host port if the chosen one was
				// taken by a non-RackOps process; keep routing pointed at the
				// port the container is actually published on.
				if res.HostPort > 0 {
					svc.Port = res.HostPort
				}
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
		a.serviceChanged(svc)
		a.NotifyDeploy(svc, res.OK)
	}
}

// ---- Fleet handlers ----

// serviceInOrg reports whether a service belongs to an org, resolved through
// its project (services carry no OrgID of their own). Orphaned services
// (project deleted) fall back to their node's org. Callers hold Store.View.
func (a *API) serviceInOrg(s *types.Service, orgID string) bool {
	if p := a.Store.Projects[s.ProjectID]; p != nil {
		return p.OrgID == orgID
	}
	if n := a.Store.Nodes[s.NodeID]; n != nil {
		return n.OrgID == orgID
	}
	return false
}

func (a *API) handleListNodes(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var out []*types.Node
	for _, n := range a.Store.SortedNodes() {
		if n.OrgID == org.ID {
			out = append(out, n)
		}
	}
	writeJSON(w, out)
}

func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[r.PathValue("id")] })
	if node == nil || node.OrgID != org.ID {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	writeJSON(w, node)
}

// handleOverview aggregates the active org's KPIs.
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
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
			if n.OrgID != org.ID {
				continue
			}
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
			if !a.serviceInOrg(s, org.ID) {
				continue
			}
			o.ServicesTotal++
			if s.Status == types.StatusRunning {
				o.ServicesUp++
			}
		}
		for _, al := range a.Store.Alerts {
			if !al.Resolved && a.alertInOrg(al, org.ID) {
				o.OpenAlerts++
			}
		}
	})
	writeJSON(w, o)
}

// alertInOrg resolves an alert's org through its node or service. Alerts with
// neither (install-wide) show everywhere. Callers hold Store.View.
func (a *API) alertInOrg(al *types.Alert, orgID string) bool {
	if al.NodeID != "" {
		if n := a.Store.Nodes[al.NodeID]; n != nil {
			return n.OrgID == orgID
		}
	}
	if al.ServiceID != "" {
		if s := a.Store.Services[al.ServiceID]; s != nil {
			return a.serviceInOrg(s, orgID)
		}
	}
	return true
}

// eventInOrg mirrors alertInOrg for feed events.
func (a *API) eventInOrg(ev *types.Event, orgID string) bool {
	if ev.NodeID != "" {
		if n := a.Store.Nodes[ev.NodeID]; n != nil {
			return n.OrgID == orgID
		}
	}
	if ev.ServiceID != "" {
		if s := a.Store.Services[ev.ServiceID]; s != nil {
			return a.serviceInOrg(s, orgID)
		}
	}
	return true
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var evs []*types.Event
	a.Store.View(func() {
		for _, ev := range a.Store.Events {
			if a.eventInOrg(ev, org.ID) {
				evs = append(evs, ev)
			}
		}
	})
	// newest first
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	writeJSON(w, evs)
}

func (a *API) handleAlerts(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var alerts []*types.Alert
	a.Store.View(func() {
		for _, al := range a.Store.Alerts {
			if (!al.Resolved || r.URL.Query().Get("all") == "true") && a.alertInOrg(al, org.ID) {
				alerts = append(alerts, al)
			}
		}
	})
	writeJSON(w, alerts)
}

func (a *API) handleJoinCommand(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageMachines }) {
		http.Error(w, "you don't have permission to add machines to this organization", http.StatusForbidden)
		return
	}
	// The command runs on the machine being joined, so it must never point at
	// loopback: a dashboard opened on http://localhost:8080 would otherwise
	// mint a command telling the new machine to join itself. A routable r.Host
	// (LAN IP, public IP, domain) is the best signal — the operator has already
	// proven it reachable — and loopback falls back to the server's advertised
	// address (--advertise, or the primary interface IP).
	host := r.Host
	if hostIsLoopback(host) {
		host = a.advertiseAddr()
	}
	// --org routes the new machine into the active org at first ingest.
	// installCommand is the from-scratch path: the release installer's agent
	// mode downloads the binary, sets up Docker + a service, and joins — one
	// paste on a bare machine. command remains the bare-binary variant.
	writeJSON(w, map[string]string{
		"command": fmt.Sprintf("rackops agent --server http://%s --token %s --org %s", host, a.JoinToken, org.ID),
		"installCommand": fmt.Sprintf(
			"curl -fsSL https://github.com/%s/releases/latest/download/install.sh | sudo bash -s -- agent --server http://%s --token %s --org %s",
			releaseRepo, host, a.JoinToken, org.ID),
		"token": a.JoinToken,
	})
}

// hostIsLoopback reports whether a Host header names this machine only:
// localhost, 127.0.0.0/8, or [::1] — with or without a port.
func hostIsLoopback(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// ---- Project / env / service handlers ----

func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var out []*types.Project
	a.Store.View(func() {
		for _, p := range a.Store.Projects {
			if p.OrgID == org.ID && projectVisible(member, p.ID) {
				out = append(out, p)
			}
		}
	})
	writeJSON(w, out)
}

func (a *API) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.CreateProjects }) {
		http.Error(w, "you don't have permission to create projects in this organization", http.StatusForbidden)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	p := &types.Project{ID: NewID(), Name: req.Name, Description: req.Description, OrgID: org.ID, CreatedBy: member.UserID, CreatedAt: time.Now()}
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
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	pid := r.PathValue("id")
	var out []*types.Environment
	a.Store.View(func() {
		p := a.Store.Projects[pid]
		if p == nil || p.OrgID != org.ID || !projectVisible(member, pid) {
			return
		}
		for _, e := range a.Store.Envs {
			if e.ProjectID == pid {
				out = append(out, e)
			}
		}
	})
	writeJSON(w, out)
}

func (a *API) handleCreateEnv(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	pid := r.PathValue("id")
	ok := false
	a.Store.View(func() {
		p := a.Store.Projects[pid]
		ok = p != nil && p.OrgID == org.ID && projectVisible(member, pid)
	})
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	e := &types.Environment{ID: NewID(), ProjectID: pid, Name: req.Name, CreatedAt: time.Now()}
	a.Store.Mutate(func() { a.Store.Envs[e.ID] = e })
	writeJSON(w, e)
}

func (a *API) handleListServices(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
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
			if !a.serviceInOrg(s, org.ID) || !projectVisible(member, s.ProjectID) {
				continue
			}
			out = append(out, s)
		}
	})
	writeJSON(w, out)
}

func (a *API) handlePlacement(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var req types.PlacementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.OrgID = org.ID // never rank another tenant's hardware
	writeJSON(w, a.Store.PlaceWorkload(req))
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	v = publicServiceResponse(v)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// spaHandler serves the embedded dashboard with SPA fallback to index.html.
// index.html is served no-cache so browsers always pick up new asset hashes
// after an upgrade; hashed assets themselves cache forever.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/" // SPA fallback
			path = "index.html"
		}
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		} else if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
