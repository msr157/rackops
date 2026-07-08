// Package agent implements the RackOps node agent: it samples telemetry,
// pushes reports to the control plane, and executes returned tasks against
// the local container runtime.
package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/telemetry"
	"github.com/rackops/rackops/internal/types"
)

// Config configures the agent.
type Config struct {
	ServerURL string // control plane base URL
	Token     string // join token
	Name      string // node display name (default: hostname)
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

	return &Agent{
		cfg:       cfg,
		nodeID:    string(id),
		collector: telemetry.NewCollector(),
		runtime:   rt,
		client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Run starts the report/execute loop; blocks forever.
func (a *Agent) Run() {
	log.Printf("agent: node %s (%s) reporting to %s every %s", a.cfg.Name, a.nodeID, a.cfg.ServerURL, a.cfg.Interval)
	hw := a.collector.Hardware()
	lastHW := time.Now()

	for {
		// Refresh full hardware inventory once a minute (drives/GPU usage change).
		if time.Since(lastHW) > time.Minute {
			hw = a.collector.Hardware()
			lastHW = time.Now()
		}

		report := types.AgentReport{
			NodeID:      a.nodeID,
			Name:        a.cfg.Name,
			Hardware:    hw,
			Metrics:     a.collector.Metrics(),
			Host:        telemetry.Host(),
			AgentVer:    a.cfg.Version,
			TaskResults: a.results,
		}
		if a.runtime != nil {
			report.Containers = a.runtime.List()
		}

		resp, err := a.push(report)
		if err != nil {
			log.Printf("agent: push failed: %v (retrying)", err)
			time.Sleep(a.cfg.Interval)
			continue
		}
		a.results = nil // delivered

		for _, task := range resp.Tasks {
			res := a.execute(task)
			a.results = append(a.results, res)
		}
		time.Sleep(a.cfg.Interval)
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

// execute runs one task and reports the outcome.
func (a *Agent) execute(task types.AgentTask) types.TaskResult {
	log.Printf("agent: executing task %s (%s) for service %s", task.ID, task.Kind, task.ServiceID)
	res := types.TaskResult{TaskID: task.ID}
	if a.runtime == nil {
		res.Log = "no container runtime available on this node"
		return res
	}
	var p DeployPayload
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		res.Log = "bad payload: " + err.Error()
		return res
	}

	switch task.Kind {
	case types.TaskDeploy:
		cid, logOut, err := a.runtime.Deploy(p)
		res.Log = logOut
		res.ContainerID = cid
		res.OK = err == nil
		if err != nil {
			res.Log += "\nERROR: " + err.Error()
		}
	case types.TaskStop:
		out, err := a.runtime.Simple("stop", containerName(p))
		res.Log, res.OK = out, err == nil
	case types.TaskStart:
		out, err := a.runtime.Simple("start", containerName(p))
		res.Log, res.OK = out, err == nil
	case types.TaskRestart:
		out, err := a.runtime.Simple("restart", containerName(p))
		res.Log, res.OK = out, err == nil
	case types.TaskRemove:
		out, err := a.runtime.Simple("rm", "-f", containerName(p))
		res.Log, res.OK = out, err == nil
	default:
		res.Log = "unknown task kind: " + string(task.Kind)
	}
	return res
}

// DeployPayload mirrors server.DeployPayload (kept in sync; agent is
// deliberately decoupled from the server package).
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
	Volumes     []string       `json:"volumes,omitempty"`
	PinnedPath  string         `json:"pinnedPath,omitempty"`
	MemLimitMB  int64          `json:"memLimitMb,omitempty"`
	ContainerID string         `json:"containerId,omitempty"`
}

func containerName(p DeployPayload) string {
	return "rackops-" + sanitize(p.Name) + "-" + p.ServiceID
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
