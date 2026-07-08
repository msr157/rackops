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

	Nodes       map[string]*types.Node        `json:"nodes"`
	Projects    map[string]*types.Project     `json:"projects"`
	Envs        map[string]*types.Environment `json:"envs"`
	Services    map[string]*types.Service     `json:"services"`
	Deployments map[string]*types.Deployment  `json:"deployments"`
	Users       map[string]*types.User        `json:"users"`
	Alerts      map[string]*types.Alert       `json:"alerts"`
	Domains     map[string]*types.Domain      `json:"domains"`
	Ingress     types.IngressConfig           `json:"ingress"`
	Events      []*types.Event                `json:"events"`

	// taskQueue maps nodeID -> pending agent tasks (not persisted; re-derived).
	taskQueue map[string][]types.AgentTask

	dataDir string
	dirty   bool
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
		Alerts:      map[string]*types.Alert{},
		Domains:     map[string]*types.Domain{},
		taskQueue:   map[string][]types.AgentTask{},
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
		s.mu.Unlock()
		if err == nil {
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, data, 0o600); err == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
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

// EnqueueTask queues a task for a node's agent.
func (s *Store) EnqueueTask(nodeID string, task types.AgentTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskQueue[nodeID] = append(s.taskQueue[nodeID], task)
}

// DrainTasks pops all pending tasks for a node.
func (s *Store) DrainTasks(nodeID string) []types.AgentTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := s.taskQueue[nodeID]
	s.taskQueue[nodeID] = nil
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

// NewID returns a random 12-hex-char identifier.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
