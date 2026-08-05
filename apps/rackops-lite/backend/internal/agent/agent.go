// Package agent implements the RackOps node agent: it samples telemetry,
// pushes reports to the control plane, and executes returned tasks against
// the local container runtime.
package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/telemetry"
	"github.com/rackops/rackops/internal/types"
)

// Config configures the agent.
type Config struct {
	ServerURL string // control plane base URL
	Token     string // join token
	Name      string // node display name (default: hostname)
	OrgID     string // organization to join (empty: server's default org)
	DataDir   string // where node identity is stored
	Interval  time.Duration
	Version   string
}

// Agent is the node agent runtime.
type Agent struct {
	cfg       Config
	nodeID    string
	collector *telemetry.Collector
	runtime   *Runtime
	client    *http.Client
	results   []types.TaskResult
	mesh      meshState
	// servicePorts tracks ports of services deployed on this node (from
	// deploy payloads) so they can be proxied onto the mesh.
	servicePorts map[string]int // serviceID -> port
	// publicIP is the internet-visible address (reflector-detected, refreshed
	// hourly). Guarded by publicIPMu; "" until first detection completes.
	publicIP   string
	publicIPMu sync.Mutex
	// follows tracks live `logs -f` streams by stream ID so a logs-stop task
	// (or shutdown) can cancel them. Guarded by followMu.
	follows  map[string]*follower
	followMu sync.Mutex
	// fd holds Front Door state: the install's signing key and the
	// supervised tunnel to the vendor edge. Guarded by its own mutex.
	fd frontDoor
}

// follower is one live `logs -f` tail. Stored by pointer so a goroutine can
// remove only its own registration even if a newer follower took the stream ID.
type follower struct {
	cancel context.CancelFunc
}

// New creates an agent, loading or generating a persistent node identity.
func New(cfg Config) (*Agent, error) {
	if cfg.Interval == 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".rackops")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		cfg.Name, _ = os.Hostname()
	}

	// Persistent node ID survives restarts.
	idPath := filepath.Join(cfg.DataDir, "node-id")
	id, err := os.ReadFile(idPath)
	if err != nil {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		id = []byte(hex.EncodeToString(b))
		if err := os.WriteFile(idPath, id, 0o600); err != nil {
			return nil, err
		}
	}

	rt, err := DetectRuntime()
	if err != nil {
		log.Printf("agent: no container runtime found (%v) — deploys disabled, telemetry only", err)
	} else {
		log.Printf("agent: using container runtime: %s", rt.Bin)
	}

	ag := &Agent{
		cfg:          cfg,
		nodeID:       string(id),
		collector:    telemetry.NewCollector(),
		runtime:      rt,
		client:       &http.Client{Timeout: 30 * time.Second},
		servicePorts: map[string]int{},
		follows:      map[string]*follower{},
	}
	if err := ag.initMeshKeys(); err != nil {
		log.Printf("agent: mesh keys: %v (mesh disabled)", err)
	}
	if err := ag.initFrontDoorKey(); err != nil {
		log.Printf("agent: front door key: %v (front door disabled)", err)
	}
	return ag, nil
}

// Run starts the report/execute loop, blocking until ctx is cancelled. On
// cancellation it tears down the mesh interface and relay listeners so a
// restart (or the host's shutdown) doesn't leak the WireGuard device or ports.
func (a *Agent) Run(ctx context.Context) {
	log.Printf("agent: node %s (%s) reporting to %s every %s", a.cfg.Name, a.nodeID, a.cfg.ServerURL, a.cfg.Interval)
	defer a.closeMesh()
	defer a.closeFollows()
	defer a.closeFrontDoor()
	hw := a.collector.Hardware()
	lastHW := time.Now()

	// Detect the internet-visible IP in the background (never blocks reports)
	// and refresh hourly — home connections change IPs.
	go func() {
		for {
			ip := telemetry.PublicIP()
			a.publicIPMu.Lock()
			a.publicIP = ip
			a.publicIPMu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()

	for {
		// Refresh full hardware inventory once a minute (drives/GPU usage change).
		if time.Since(lastHW) > time.Minute {
			hw = a.collector.Hardware()
			lastHW = time.Now()
		}

		host := telemetry.Host()
		a.publicIPMu.Lock()
		host.PublicIP = a.publicIP
		a.publicIPMu.Unlock()
		report := types.AgentReport{
			NodeID:      a.nodeID,
			Name:        a.cfg.Name,
			OrgID:       a.cfg.OrgID,
			Hardware:    hw,
			Metrics:     a.collector.Metrics(),
			Host:        host,
			AgentVer:    a.cfg.Version,
			TaskResults: a.results,
		}
		if a.mesh.keys.Public != "" {
			report.Mesh = &types.MeshInfo{PublicKey: a.mesh.keys.Public, MeshIP: a.mesh.meshIP}
		}
		// Always report Front Door state, even when off: the control plane
		// needs this node's public key to be able to enroll it, and the
		// console needs to distinguish "off" from "never reported".
		if fd := a.frontDoorState(); fd.PublicKey != "" {
			report.FrontDoor = &fd
		}
		if a.runtime != nil {
			report.Containers = a.runtime.List()
		}

		resp, err := a.push(report)
		if err != nil {
			log.Printf("agent: push failed: %v (retrying)", err)
			if !sleepCtx(ctx, a.cfg.Interval) {
				return
			}
			continue
		}
		a.results = nil // delivered

		// Apply mesh assignment; expose the ports the server asks for plus
		// any known locally (covers agent restarts before server catches up).
		ports := make([]int, 0, len(a.servicePorts))
		for _, p := range a.servicePorts {
			ports = append(ports, p)
		}
		if resp.Mesh != nil {
			ports = append(ports, resp.Mesh.ExposePorts...)
		}
		a.syncMesh(resp.Mesh, report.Containers, ports)
		a.syncFrontDoor(ctx, resp.FrontDoor)

		for _, task := range resp.Tasks {
			res := a.execute(ctx, task)
			// Follow tasks stream out-of-band and have no immediate result to
			// report back; only enqueue results that carry output.
			if res != nil {
				a.results = append(a.results, *res)
			}
		}
		if !sleepCtx(ctx, a.cfg.Interval) {
			return
		}
	}
}

// sleepCtx waits for d or until ctx is cancelled. It returns false if the
// context was cancelled (caller should stop).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// push sends a report and returns the control plane's response.
func (a *Agent) push(rep types.AgentReport) (*types.AgentResponse, error) {
	body, err := json.Marshal(rep)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", strings.TrimSuffix(a.cfg.ServerURL, "/")+"/api/agent/ingest", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Rackops-Token", a.cfg.Token)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	var out types.AgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// execute runs one task and returns its outcome, or nil for tasks (log
// follows) whose output streams out-of-band and has no immediate result.
func (a *Agent) execute(ctx context.Context, task types.AgentTask) *types.TaskResult {
	log.Printf("agent: executing task %s (%s) for service %s", task.ID, task.Kind, task.ServiceID)
	res := types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID}
	var p DeployPayload
	if task.Payload != "" {
		if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
			res.Log = "bad payload: " + err.Error()
			return &res
		}
	}

	// Log-follow/stop don't need a container runtime check up front.
	switch task.Kind {
	case types.TaskLogsFollow:
		a.startFollow(ctx, p)
		return nil
	case types.TaskLogsStop:
		a.stopFollow(p.StreamID)
		return nil
	}

	if a.runtime == nil {
		res.Log = "no container runtime available on this node"
		return &res
	}

	switch task.Kind {
	case types.TaskDeploy:
		// Stream build/pull/run output live (keyed by the deployment/task ID)
		// while still accumulating the full log for the deployment record.
		sink := a.newLogSink(ctx, task.ID, p.ServiceID, "deploy")
		cid, hostPort, _, err := a.runtime.Deploy(p, sink)
		if err != nil {
			sink.WriteString("\nERROR: " + err.Error() + "\n")
		}
		res.Log = sink.Close()
		res.ContainerID = cid
		res.HostPort = hostPort
		res.OK = err == nil
		if err == nil && hostPort > 0 {
			a.servicePorts[p.ServiceID] = hostPort
		}
	case types.TaskStop:
		out, err := a.runtime.Simple("stop", targetContainer(p))
		res.Log, res.OK = out, err == nil
	case types.TaskStart:
		out, err := a.runtime.Simple("start", targetContainer(p))
		res.Log, res.OK = out, err == nil
	case types.TaskRestart:
		out, err := a.runtime.Simple("restart", targetContainer(p))
		res.Log, res.OK = out, err == nil
	case types.TaskRemove:
		if p.ComposeYML != "" {
			out, err := a.runtime.composeDown(p)
			res.Log, res.OK = out, err == nil
		} else {
			out, err := a.runtime.Simple("rm", "-f", targetContainer(p))
			res.Log, res.OK = out, err == nil
		}
		// Any quick tunnel attached to the service goes with it.
		_, _ = a.runtime.Simple("rm", "-f", quickTunnelName(p.ServiceID))
		delete(a.servicePorts, p.ServiceID)
	case types.TaskLogs:
		res.Log, res.OK = a.fetchLogs(p)
	case types.TaskListDir:
		out, err := listDir(p.Path)
		res.Log, res.OK = out, err == nil
		if err != nil {
			res.Log = "ERROR: " + err.Error()
		}
	case types.TaskTunnelStart:
		url, err := a.runtime.StartQuickTunnel(p.ServiceID, p.Port)
		if err != nil {
			res.Log = "ERROR: " + err.Error()
		} else {
			res.Log, res.OK = url, true
		}
	case types.TaskTunnelStop:
		_, _ = a.runtime.Simple("rm", "-f", quickTunnelName(p.ServiceID))
		res.OK = true
	default:
		res.Log = "unknown task kind: " + string(task.Kind)
	}
	return &res
}

// quickTunnelName is the cloudflared container name for a service's tunnel.
// Labeled rackops.tunnel (NOT rackops.service) so it stays out of the
// container→service status sync.
func quickTunnelName(serviceID string) string {
	return "rackops-qt-" + serviceID
}

// fetchLogs returns a service's runtime logs, handling every container shape:
// compose stacks (project logs), plain containers by name, a recorded
// container ID as fallback, and a clear message when nothing exists at all.
func (a *Agent) fetchLogs(p DeployPayload) (string, bool) {
	if p.ComposeYML != "" {
		out, err := a.runtime.composeLogs(p, 500, p.Since)
		if err != nil {
			return out + "\n(compose logs failed: " + err.Error() + ")", false
		}
		return out, true
	}
	out, err := a.runtime.LogsSince(targetContainer(p), 500, p.Since)
	if err == nil {
		return out, true
	}
	if p.ContainerID != "" {
		if byID, idErr := a.runtime.LogsSince(p.ContainerID, 500, p.Since); idErr == nil {
			return byID, true
		}
	}
	return "The container for this service does not exist on this node — it was removed or has not deployed yet.\n" +
		"Check the Deployments tab for build/run output. If the service shows failed, hit Redeploy (or wait for auto-heal).\n\n" +
		"(" + strings.TrimSpace(out) + ")", false
}

// DeployPayload mirrors server.DeployPayload (kept in sync; agent is
// deliberately decoupled from the server package).
type DeployPayload struct {
	ServiceID     string         `json:"serviceId"`
	Name          string         `json:"name"`
	Image         string         `json:"image,omitempty"`
	GitRepo       string         `json:"gitRepo,omitempty"`
	GitBranch     string         `json:"gitBranch,omitempty"`
	GitToken      string         `json:"gitToken,omitempty"`
	ComposeYML    string         `json:"composeYml,omitempty"`
	Port          int            `json:"port"`
	ContainerPort int            `json:"containerPort,omitempty"`
	Ports         []int          `json:"ports,omitempty"`
	Command       string         `json:"command,omitempty"`
	Env           []types.EnvVar `json:"env"`
	Volumes       []string       `json:"volumes,omitempty"`
	Mounts        []types.Mount  `json:"mounts,omitempty"`
	PinnedPath    string         `json:"pinnedPath,omitempty"`
	MemLimitMB    int64          `json:"memLimitMb,omitempty"`
	CPULimit      float64        `json:"cpuLimit,omitempty"`
	RestartPol    string         `json:"restartPolicy,omitempty"`
	GPU           bool           `json:"gpu,omitempty"`
	HostNet       bool           `json:"hostNet,omitempty"`
	ExternalName  string         `json:"externalName,omitempty"` // adopted container's real name
	ContainerID   string         `json:"containerId,omitempty"`
	Since         string         `json:"since,omitempty"`
	Path          string         `json:"path,omitempty"`
	StreamID      string         `json:"streamId,omitempty"`
}

func containerName(p DeployPayload) string {
	return "rackops-" + sanitize(p.Name) + "-" + p.ServiceID
}

// targetContainer is the container name lifecycle ops (stop/start/restart/
// remove/logs) act on. Adopted services carry their real container name in
// ExternalName; everything RackOps deployed uses the rackops- naming scheme.
func targetContainer(p DeployPayload) string {
	if p.ExternalName != "" {
		return p.ExternalName
	}
	return containerName(p)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' {
			b.WriteRune('-')
		}
	}
	return b.String()
}
