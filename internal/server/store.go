package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/msr157/rackops/internal/api"
)

// staleAfter marks a node stale when it hasn't reported for this long.
const staleAfter = 15 * time.Second

// Store is the node registry. It always keeps an in-memory cache for
// sub-millisecond reads, and optionally persists to Postgres so that
// restarting the control plane doesn't lose the last known state.
type Store struct {
	mu    sync.RWMutex
	nodes map[string]*api.Node

	// db is nil when no DATABASE_URL is configured (pure in-memory mode).
	db *sql.DB
}

// NewStore creates an in-memory-only store.
func NewStore() *Store {
	return &Store{nodes: make(map[string]*api.Node)}
}

// NewStoreWithDB creates a store backed by Postgres.
// It creates the required table if it does not exist and pre-warms the
// in-memory cache from persisted rows.
func NewStoreWithDB(db *sql.DB) *Store {
	s := &Store{nodes: make(map[string]*api.Node), db: db}
	s.migrate()
	s.warmup()
	return s
}

// migrate ensures the rackops_nodes table exists.
func (s *Store) migrate() {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS rackops_nodes (
			id          TEXT PRIMARY KEY,
			hostname    TEXT NOT NULL,
			last_seen   TIMESTAMPTZ NOT NULL,
			snapshot    JSONB NOT NULL
		)
	`)
	if err != nil {
		log.Printf("store: migrate failed (continuing in-memory): %v", err)
		s.db = nil
	} else {
		log.Println("store: postgres table rackops_nodes ready")
	}

	if s.db != nil {
		_, err = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS early_access_requests (
				id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				email       TEXT UNIQUE NOT NULL,
				name        TEXT NOT NULL,
				industry    TEXT NOT NULL,
				notes       TEXT,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`)
		if err != nil {
			log.Printf("store: migrate early_access_requests failed: %v", err)
		} else {
			log.Println("store: postgres table early_access_requests ready")
		}
	}
}

// warmup pre-loads persisted nodes into memory on startup.
func (s *Store) warmup() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT id, hostname, last_seen, snapshot FROM rackops_nodes`)
	if err != nil {
		log.Printf("store: warmup query failed: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var (
			id, hostname string
			lastSeen     time.Time
			snapJSON     []byte
		)
		if err := rows.Scan(&id, &hostname, &lastSeen, &snapJSON); err != nil {
			continue
		}
		var snap api.Snapshot
		if err := json.Unmarshal(snapJSON, &snap); err != nil {
			continue
		}
		s.nodes[id] = &api.Node{
			ID:       id,
			Hostname: hostname,
			LastSeen: lastSeen,
			Latest:   snap,
		}
		count++
	}
	log.Printf("store: warmed up %d nodes from postgres", count)
}

func (s *Store) Upsert(report api.ReportRequest) {
	s.mu.Lock()
	s.nodes[report.NodeID] = &api.Node{
		ID:       report.NodeID,
		Hostname: report.Snapshot.Hostname,
		LastSeen: time.Now().UTC(),
		Latest:   report.Snapshot,
	}
	node := s.nodes[report.NodeID]
	s.mu.Unlock()

	if s.db != nil {
		snapJSON, err := json.Marshal(node.Latest)
		if err == nil {
			_, err = s.db.Exec(`
				INSERT INTO rackops_nodes (id, hostname, last_seen, snapshot)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (id) DO UPDATE
				SET hostname  = EXCLUDED.hostname,
				    last_seen = EXCLUDED.last_seen,
				    snapshot  = EXCLUDED.snapshot
			`, node.ID, node.Hostname, node.LastSeen, snapJSON)
			if err != nil {
				log.Printf("store: upsert to postgres failed: %v", err)
			}
		}
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

// AddEarlyAccessRequest adds a new early access request.
// Returns an error if the email already exists or if DB is not configured.
func (s *Store) AddEarlyAccessRequest(req api.EarlyAccessRequest) error {
	if s.db == nil {
		// If running in-memory only, just log and pretend it succeeded.
		log.Printf("store: received early access request for %s (in-memory mode, ignoring)", req.Email)
		return nil
	}

	_, err := s.db.Exec(`
		INSERT INTO early_access_requests (email, name, industry, notes)
		VALUES ($1, $2, $3, $4)
	`, req.Email, req.Name, req.Industry, req.Notes)

	return err
}
