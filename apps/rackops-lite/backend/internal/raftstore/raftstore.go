// Package raftstore replicates control-plane state across server nodes with
// hashicorp/raft. Replication is snapshot-granular: the leader proposes the
// full state JSON whenever it changes (small at Lite scale); followers apply
// it into their local Store. On failover the new leader continues from the
// last applied state.
package raftstore

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

// Cluster wraps an embedded raft node.
type Cluster struct {
	Raft   *raft.Raft
	fsm    *fsm
	NodeID string
	// Addr is this node's raft bind address (host:port).
	Addr string
}

// fsm applies replicated state snapshots.
type fsm struct {
	mu    sync.Mutex
	state []byte // last applied full-state JSON
	apply func([]byte)
}

func (f *fsm) Apply(l *raft.Log) any {
	f.mu.Lock()
	f.state = append([]byte(nil), l.Data...)
	f.mu.Unlock()
	if f.apply != nil {
		f.apply(l.Data)
	}
	return nil
}

func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &snapshot{state: append([]byte(nil), f.state...)}, nil
}

func (f *fsm) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.state = data
	f.mu.Unlock()
	if f.apply != nil {
		f.apply(data)
	}
	return nil
}

type snapshot struct{ state []byte }

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.state); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}
func (s *snapshot) Release() {}

// Options configures the cluster member.
type Options struct {
	NodeID    string
	BindAddr  string // raft TCP bind, e.g. 0.0.0.0:9522
	Advertise string // address peers reach us on, e.g. 192.168.0.10:9522
	DataDir   string
	Bootstrap bool // start a fresh single-node cluster
	// OnApply is invoked with the replicated state JSON (followers + leader).
	OnApply func([]byte)
}

// New starts the raft node.
func New(o Options) (*Cluster, error) {
	cfg := raft.DefaultConfig()
	cfg.LocalID = raft.ServerID(o.NodeID)
	cfg.LogOutput = os.Stderr
	cfg.SnapshotInterval = 30 * time.Second
	cfg.SnapshotThreshold = 256

	dir := filepath.Join(o.DataDir, "raft")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	logs, err := raftboltdb.NewBoltStore(filepath.Join(dir, "raft.db"))
	if err != nil {
		return nil, err
	}
	snaps, err := raft.NewFileSnapshotStore(dir, 2, os.Stderr)
	if err != nil {
		return nil, err
	}

	advertise, err := net.ResolveTCPAddr("tcp", o.Advertise)
	if err != nil {
		return nil, err
	}
	transport, err := raft.NewTCPTransport(o.BindAddr, advertise, 4, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, err
	}

	f := &fsm{apply: o.OnApply}
	r, err := raft.NewRaft(cfg, f, logs, logs, snaps, transport)
	if err != nil {
		return nil, err
	}
	c := &Cluster{Raft: r, fsm: f, NodeID: o.NodeID, Addr: o.Advertise}

	if o.Bootstrap {
		hasState, _ := raft.HasExistingState(logs, logs, snaps)
		if !hasState {
			r.BootstrapCluster(raft.Configuration{
				Servers: []raft.Server{{ID: cfg.LocalID, Address: transport.LocalAddr()}},
			})
			log.Printf("raft: bootstrapped single-node cluster as %s", o.NodeID)
		}
	}
	return c, nil
}

// IsLeader reports whether this node currently leads the cluster.
func (c *Cluster) IsLeader() bool { return c.Raft.State() == raft.Leader }

// LeaderAddr returns the current leader's raft address ("" if unknown).
func (c *Cluster) LeaderAddr() string {
	addr, _ := c.Raft.LeaderWithID()
	return string(addr)
}

// Propose replicates a full-state snapshot (leader only).
func (c *Cluster) Propose(state []byte) error {
	if !c.IsLeader() {
		return fmt.Errorf("not leader")
	}
	return c.Raft.Apply(state, 5*time.Second).Error()
}

// AddVoter joins a new server to the cluster (leader only).
func (c *Cluster) AddVoter(nodeID, addr string) error {
	if !c.IsLeader() {
		return fmt.Errorf("not leader")
	}
	return c.Raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 10*time.Second).Error()
}

// Peers lists cluster members.
func (c *Cluster) Peers() []map[string]string {
	future := c.Raft.GetConfiguration()
	if future.Error() != nil {
		return nil
	}
	var out []map[string]string
	leader, _ := c.Raft.LeaderWithID()
	for _, s := range future.Configuration().Servers {
		role := "follower"
		if s.Address == leader {
			role = "leader"
		}
		out = append(out, map[string]string{"id": string(s.ID), "addr": string(s.Address), "role": role})
	}
	return out
}

// StateJSON parses arbitrary state for callers needing raw access.
func StateJSON(data []byte, v any) error { return json.Unmarshal(data, v) }
