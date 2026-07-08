package server

import (
	"encoding/json"
	"net/http"

	"github.com/rackops/rackops/internal/raftstore"
)

// Cluster wires the raft layer into the API (nil = single-server mode).
var _ = json.Marshal

// handleClusterStatus reports raft membership and leadership.
func (a *API) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if a.Cluster == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	var leaderHTTP string
	a.Store.View(func() {
		for id, addr := range a.Store.ClusterHTTP {
			for _, p := range a.Cluster.Peers() {
				if p["id"] == id && p["role"] == "leader" {
					leaderHTTP = addr
				}
			}
		}
	})
	writeJSON(w, map[string]any{
		"enabled":    true,
		"nodeId":     a.Cluster.NodeID,
		"isLeader":   a.Cluster.IsLeader(),
		"leaderAddr": a.Cluster.LeaderAddr(),
		"leaderHttp": leaderHTTP,
		"peers":      a.Cluster.Peers(),
	})
}

// handleClusterJoin admits a new server (called on the leader by the joiner).
// Auth: join token (same trust level as agent enrollment).
func (a *API) handleClusterJoin(w http.ResponseWriter, r *http.Request) {
	if a.Cluster == nil {
		http.Error(w, "clustering disabled on this server", http.StatusServiceUnavailable)
		return
	}
	if a.JoinToken != "" && r.Header.Get("X-Rackops-Token") != a.JoinToken {
		http.Error(w, "invalid join token", http.StatusUnauthorized)
		return
	}
	var req struct {
		NodeID   string `json:"nodeId"`
		RaftAddr string `json:"raftAddr"`
		HTTPAddr string `json:"httpAddr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" || req.RaftAddr == "" {
		http.Error(w, "nodeId and raftAddr required", http.StatusBadRequest)
		return
	}
	if !a.Cluster.IsLeader() {
		http.Error(w, "not the leader; join via "+a.leaderHTTP(), http.StatusMisdirectedRequest)
		return
	}
	if err := a.Cluster.AddVoter(req.NodeID, req.RaftAddr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Store.Mutate(func() {
		if a.Store.ClusterHTTP == nil {
			a.Store.ClusterHTTP = map[string]string{}
		}
		a.Store.ClusterHTTP[req.NodeID] = req.HTTPAddr
	})
	a.Store.AddEvent("cluster", "Server joined control plane: "+req.NodeID+" ("+req.RaftAddr+")", "", "")
	writeJSON(w, map[string]string{"status": "joined"})
}

// leaderHTTP resolves the leader's HTTP address from replicated state.
func (a *API) leaderHTTP() string {
	if a.Cluster == nil {
		return ""
	}
	leaderRaft := a.Cluster.LeaderAddr()
	var httpAddr string
	a.Store.View(func() {
		for _, p := range a.Cluster.Peers() {
			if p["addr"] == leaderRaft {
				httpAddr = a.Store.ClusterHTTP[p["id"]]
			}
		}
	})
	return httpAddr
}

// clusterGuard rejects writes on followers, pointing callers at the leader.
func (a *API) clusterGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Cluster != nil && !a.Cluster.IsLeader() && r.Method != http.MethodGet &&
			r.URL.Path != "/api/agent/ingest" && r.URL.Path != "/api/cluster/join" &&
			r.URL.Path != "/api/auth/login" {
			leader := a.leaderHTTP()
			if leader != "" {
				http.Error(w, `{"error":"follower","leader":"`+leader+`"}`, http.StatusMisdirectedRequest)
				return
			}
			http.Error(w, "no leader elected yet — retry shortly", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StartReplication hooks store persistence to raft proposals (leader only).
// The FSM apply callback must import ONLY on followers — the leader's store is
// authoritative, so re-importing its own proposals would clobber concurrent
// local writes and create a feedback loop.
func (a *API) StartReplication(c *raftstore.Cluster) {
	a.Cluster = c
	a.applyGuard = func(state []byte) {
		if !c.IsLeader() {
			_ = a.Store.ImportJSON(state)
		}
	}
	a.Store.OnDirty = func(state []byte) {
		if c.IsLeader() {
			_ = c.Propose(state)
		}
	}
}

// ApplyReplicated is the FSM callback wired at raft construction.
func (a *API) ApplyReplicated(state []byte) {
	if a.applyGuard != nil {
		a.applyGuard(state)
	}
}
