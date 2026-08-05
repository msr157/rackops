// Package server implements the RackOps control plane.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Store holds all cluster state. v1 persists JSON snapshots to disk; the
// mutation methods are funneled through apply() so a Raft FSM can replace
// the persistence layer without touching handlers (milestone: HA).
type Store struct {
	mu sync.RWMutex

	Nodes       map[string]*types.Node          `json:"nodes"`
	Projects    map[string]*types.Project       `json:"projects"`
	Envs        map[string]*types.Environment   `json:"envs"`
	Services    map[string]*types.Service       `json:"services"`
	Deployments map[string]*types.Deployment    `json:"deployments"`
	Users       map[string]*types.User          `json:"users"`
	Tokens      map[string]*types.APIToken      `json:"tokens"`
	Alerts      map[string]*types.Alert         `json:"alerts"`
	Domains     map[string]*types.Domain        `json:"domains"`
	Orgs        map[string]*types.Organization  `json:"orgs"`
	Invitations map[string]*types.Invitation    `json:"invitations"`
	Tickets     map[string]*types.SupportTicket `json:"tickets"`
	Ingress     types.IngressConfig             `json:"ingress"`
	// EarlyAccess holds enterprise early-access requests submitted from the
	// public marketing site, keyed by lowercased email so a re-submission is
	// a conflict rather than a duplicate entry.
	EarlyAccess map[string]*types.EarlyAccessLead `json:"earlyAccess,omitempty"`
	// FrontDoor is the install-wide RackOps Front Door configuration: the
	// vendor-run tunnel that gives apps a public HTTPS address without a
	// public IP.
	FrontDoor types.FrontDoorConfig `json:"frontDoor"`
	// Settings is the pre-org legacy blob; EnsureDefaultOrg moves it into
	// OrgSettings under the default org. Kept only so old state files load.
	Settings Settings `json:"settings"`
	// OrgSettings holds each organization's notification targets and Copilot
	// provider config — tenants must not share webhooks or AI keys.
	OrgSettings map[string]*Settings `json:"orgSettings,omitempty"`
	Events      []*types.Event       `json:"events"`

	// TaskQueue maps nodeID -> pending agent tasks. Persisted so queued
	// deploys/actions survive a control-plane restart; it is NOT applied from
	// replicated snapshots — each leader owns task delivery to the agents it
	// serves (see ImportJSON).
	TaskQueue map[string][]types.AgentTask `json:"taskQueue,omitempty"`

	// JWTSecret signs user session tokens. A random per-cluster secret,
	// generated once and then persisted and replicated so every HA peer agrees
	// and sessions survive leader failover. It is deliberately independent of
	// the agent join token — enrollment credentials must not be able to mint
	// user sessions.
	JWTSecret []byte `json:"jwtSecret,omitempty"`

	// ClusterHTTP maps server nodeID -> HTTP address (replicated for redirects).
	ClusterHTTP map[string]string `json:"clusterHttp,omitempty"`

	// DashboardURL is the scheme+host operators actually reach the dashboard
	// on, recorded at each successful login. Password-reset links are built on
	// it so an unauthenticated Host header can't poison emailed links.
	DashboardURL string `json:"dashboardUrl,omitempty"`
	// InstanceName is a friendly label for this install, shown in support
	// tickets and reset emails ("request came from <name>").
	InstanceName string `json:"instanceName,omitempty"`

	dataDir string
	dirty   bool
	// OnDirty, when set, is called with the exported state after each persist
	// (used by the Raft layer to replicate; leader only).
	OnDirty func([]byte) `json:"-"`
	// importing suppresses OnDirty while applying replicated state.
	importing bool
}

// Settings holds cluster-wide integrations config.
type Settings struct {
	// Notifications
	DiscordWebhook string `json:"discordWebhook,omitempty"`
	SlackWebhook   string `json:"slackWebhook,omitempty"`
	TelegramToken  string `json:"telegramToken,omitempty"`
	TelegramChatID string `json:"telegramChatId,omitempty"`
	GenericWebhook string `json:"genericWebhook,omitempty"`
	NotifyDeploys  bool   `json:"notifyDeploys"`
	NotifyAlerts   bool   `json:"notifyAlerts"`
	// AI providers (first configured wins unless Provider pins one)
	AIProvider     string `json:"aiProvider,omitempty"` // auto | anthropic | gemini | openai | ollama
	AnthropicKey   string `json:"anthropicKey,omitempty"`
	AnthropicModel string `json:"anthropicModel,omitempty"`
	GeminiKey      string `json:"geminiKey,omitempty"`
	GeminiModel    string `json:"geminiModel,omitempty"`
	OpenAIKey      string `json:"openaiKey,omitempty"`
	OpenAIBase     string `json:"openaiBase,omitempty"` // for Ollama/compatible: http://host:11434/v1
	OpenAIModel    string `json:"openaiModel,omitempty"`
}

// NewStore loads state from dataDir or initializes a fresh store.
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		Nodes:       map[string]*types.Node{},
		Projects:    map[string]*types.Project{},
		Envs:        map[string]*types.Environment{},
		Services:    map[string]*types.Service{},
		Deployments: map[string]*types.Deployment{},
		Users:       map[string]*types.User{},
		Tokens:      map[string]*types.APIToken{},
		Alerts:      map[string]*types.Alert{},
		Domains:     map[string]*types.Domain{},
		Orgs:        map[string]*types.Organization{},
		Invitations: map[string]*types.Invitation{},
		Tickets:     map[string]*types.SupportTicket{},
		EarlyAccess: map[string]*types.EarlyAccessLead{},
		TaskQueue:   map[string][]types.AgentTask{},
		dataDir:     dataDir,
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "state.json")
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	}
	if s.TaskQueue == nil {
		s.TaskQueue = map[string][]types.AgentTask{}
	}
	// State files written before early-access capture existed unmarshal this
	// as nil; writing to a nil map panics.
	if s.EarlyAccess == nil {
		s.EarlyAccess = map[string]*types.EarlyAccessLead{}
	}
	// Generate the session-signing secret once, then persist + replicate it.
	if len(s.JWTSecret) < 32 {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		s.JWTSecret = secret
		s.dirty = true
	}
	// Ensure a default project + environments exist.
	if len(s.Projects) == 0 {
		p := &types.Project{
			ID: NewID(), Name: "Default Infrastructure",
			Description: "Auto-created default project", CreatedAt: time.Now(),
		}
		s.Projects[p.ID] = p
		for _, env := range []string{"production", "staging", "development"} {
			e := &types.Environment{ID: NewID(), ProjectID: p.ID, Name: env, CreatedAt: time.Now()}
			s.Envs[e.ID] = e
		}
		s.dirty = true
	}
	go s.persistLoop()
	return s, nil
}

// persistLoop snapshots state to disk when dirty.
func (s *Store) persistLoop() {
	for range time.Tick(2 * time.Second) {
		s.mu.Lock()
		if !s.dirty {
			s.mu.Unlock()
			continue
		}
		data, err := json.MarshalIndent(s, "", " ")
		s.dirty = false
		path := filepath.Join(s.dataDir, "state.json")
		onDirty := s.OnDirty
		importing := s.importing
		s.mu.Unlock()
		if err == nil {
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, data, 0o600); err == nil {
				_ = os.Rename(tmp, path)
			}
			if onDirty != nil && !importing {
				onDirty(data)
			}
		}
	}
}

// ImportJSON replaces replicated state (nodes/services/etc) from a snapshot.
// The task queue is runtime-local delivery state — each server hands tasks to
// the agents reporting to it — so it is preserved, never imported.
func (s *Store) ImportJSON(data []byte) error {
	var in Store
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.importing = true
	s.Nodes = in.Nodes
	s.Projects = in.Projects
	s.Envs = in.Envs
	s.Services = in.Services
	s.Deployments = in.Deployments
	s.Users = in.Users
	s.Tokens = in.Tokens
	s.Alerts = in.Alerts
	s.Domains = in.Domains
	s.Ingress = in.Ingress
	s.FrontDoor = in.FrontDoor
	s.Settings = in.Settings
	s.Events = in.Events
	s.ClusterHTTP = in.ClusterHTTP
	// The session-signing secret is cluster-wide: adopt the leader's so tokens
	// issued anywhere in the cluster validate here after failover.
	if len(in.JWTSecret) >= 32 {
		s.JWTSecret = in.JWTSecret
	}
	s.dirty = true // persist locally
	s.importing = false
	return nil
}

// Mutate runs fn under the write lock and marks state dirty.
func (s *Store) Mutate(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
	s.dirty = true
}

// View runs fn under the read lock.
func (s *Store) View(fn func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn()
}

// SessionSecret returns the cluster's JWT signing secret. Read under the lock
// because followers adopt the leader's secret on snapshot import.
func (s *Store) SessionSecret() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.JWTSecret
}

// AddEvent appends to the event feed (capped at 500 entries).
func (s *Store) AddEvent(kind, msg, nodeID, serviceID string) *types.Event {
	ev := &types.Event{
		ID: NewID(), Kind: kind, Message: msg,
		NodeID: nodeID, ServiceID: serviceID, CreatedAt: time.Now(),
	}
	s.Mutate(func() {
		s.Events = append(s.Events, ev)
		if len(s.Events) > 500 {
			s.Events = s.Events[len(s.Events)-500:]
		}
	})
	return ev
}

// EnqueueTask queues a task for a node's agent. Marked dirty so the queue is
// persisted — a task queued just before a restart is still delivered after it.
func (s *Store) EnqueueTask(nodeID string, task types.AgentTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TaskQueue[nodeID] = append(s.TaskQueue[nodeID], task)
	s.dirty = true
}

// DrainTasks pops all pending tasks for a node.
func (s *Store) DrainTasks(nodeID string) []types.AgentTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := s.TaskQueue[nodeID]
	if len(tasks) == 0 {
		return nil
	}
	delete(s.TaskQueue, nodeID)
	s.dirty = true
	return tasks
}

// SortedNodes returns nodes ordered by join time.
func (s *Store) SortedNodes() []*types.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Node, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out
}

// defaultPlacement returns the project ID and an environment ID to file
// auto-created (adopted) services under. It prefers the "production" env of
// the earliest project, falling back to any env, and creates a default project
// if the store somehow has none. Callers must hold the write lock.
func (s *Store) defaultPlacement() (projectID, envID string) {
	// Earliest project by creation time keeps placement stable across restarts.
	var proj *types.Project
	for _, p := range s.Projects {
		if proj == nil || p.CreatedAt.Before(proj.CreatedAt) {
			proj = p
		}
	}
	if proj == nil {
		proj = &types.Project{
			ID: NewID(), Name: "Default Infrastructure",
			Description: "Auto-created default project", CreatedAt: time.Now(),
		}
		s.Projects[proj.ID] = proj
	}
	for _, e := range s.Envs {
		if e.ProjectID != proj.ID {
			continue
		}
		if e.Name == "production" {
			return proj.ID, e.ID
		}
		if envID == "" {
			envID = e.ID // fallback: first env in the project
		}
	}
	if envID == "" {
		e := &types.Environment{ID: NewID(), ProjectID: proj.ID, Name: "production", CreatedAt: time.Now()}
		s.Envs[e.ID] = e
		envID = e.ID
	}
	return proj.ID, envID
}

// NewID returns a random 12-hex-char identifier.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// genSecret returns a 64-character hex secret (32 random bytes) for generated
// template credentials — long enough for apps that require a minimum length,
// and purely alphanumeric so no app rejects the character set.
func genSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
