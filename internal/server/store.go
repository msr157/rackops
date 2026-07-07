package server

import (
	"sync"
	"time"

	"github.com/msr157/rackops/internal/api"
)

// staleAfter marks a node stale when it hasn't reported for this long.
const staleAfter = 15 * time.Second

// Store is an in-memory node registry. Persistence is intentionally out of
// scope for the MVP — restarting the control plane just waits one report
// interval for agents to re-register.
type Store struct {
	mu    sync.RWMutex
	nodes map[string]*api.Node
}

func NewStore() *Store {
	return &Store{nodes: make(map[string]*api.Node)}
}

func (s *Store) Upsert(report api.ReportRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[report.NodeID] = &api.Node{
		ID:       report.NodeID,
		Hostname: report.Snapshot.Hostname,
		LastSeen: time.Now().UTC(),
		Latest:   report.Snapshot,
	}
}

func (s *Store) List() []api.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]api.Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, withStatus(*n))
	}
	return out
}

func (s *Store) Get(id string) (api.Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	if !ok {
		return api.Node{}, false
	}
	return withStatus(*n), true
}

func withStatus(n api.Node) api.Node {
	if time.Since(n.LastSeen) > staleAfter {
		n.Status = api.StatusStale
	} else {
		n.Status = api.StatusOnline
	}
	return n
}
