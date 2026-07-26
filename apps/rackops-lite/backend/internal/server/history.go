package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Metric history: the time dimension behind every chart and "vs yesterday"
// delta in the dashboard. Agents already report utilization every ~2s; this
// package downsamples those reports into two fixed-size rings per entity —
// 24 hours at 1-minute buckets and 7 days at 15-minute buckets — and serves
// them from GET /api/metrics/history.
//
// History is deliberately NOT part of Store: it is per-install telemetry,
// never raft-replicated, and flushed on its own slower cadence so the 2s
// state.json loop doesn't rewrite megabytes of samples.

const (
	fineStep   = 60   // seconds per fine bucket
	fineKeep   = 1440 // 24h of fine buckets
	coarseStep = 900  // seconds per coarse bucket
	coarseKeep = 672  // 7d of coarse buckets
)

// Sample is one recorded point. For nodes TempC carries the hottest package
// temperature; service samples leave it zero.
type Sample struct {
	T    int64   `json:"t"` // unix seconds, bucket-aligned
	CPU  float64 `json:"cpuPct"`
	Mem  float64 `json:"memUsedMb"`
	Temp float64 `json:"tempC,omitempty"`
}

// series holds both resolutions for one entity. The unexported counts back
// the running average inside the current bucket; they are intentionally not
// persisted — after a restart the average simply restarts mid-bucket.
type series struct {
	Fine    []Sample `json:"fine"`
	Coarse  []Sample `json:"coarse"`
	fineN   int
	coarseN int
}

// History is the sampler + store. All methods are nil-safe so tests that
// build an API without history (and ingest before wiring) never panic.
type History struct {
	mu    sync.RWMutex
	path  string
	dirty bool

	Nodes    map[string]*series `json:"nodes"`
	Services map[string]*series `json:"services"`
}

// NewHistory loads the previous rings (if any) and starts the flush loop.
// A corrupt or missing file means starting fresh, never failing the server.
func NewHistory(dataDir string) *History {
	h := &History{
		path:     filepath.Join(dataDir, "metrics-history.json"),
		Nodes:    map[string]*series{},
		Services: map[string]*series{},
	}
	if data, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(data, h)
	}
	if h.Nodes == nil {
		h.Nodes = map[string]*series{}
	}
	if h.Services == nil {
		h.Services = map[string]*series{}
	}
	go h.flushLoop()
	return h
}

// Observe folds one agent report into the rings. Called from handleIngest on
// every report; cheap (map lookups + in-place averaging).
func (h *History) Observe(nodeID string, m types.Metrics, containers []types.ContainerStatus) {
	if h == nil {
		return
	}
	now := time.Now().Unix()
	h.mu.Lock()
	defer h.mu.Unlock()

	ns := h.Nodes[nodeID]
	if ns == nil {
		ns = &series{}
		h.Nodes[nodeID] = ns
	}
	ns.record(now, m.CPUPct, float64(m.MemUsedMB), m.TempC)

	for _, cs := range containers {
		// Tunnel helpers and the edge pseudo-service are plumbing, not apps.
		if cs.ServiceID == "" || cs.Tunnel || cs.ServiceID == ingressServiceID {
			continue
		}
		ss := h.Services[cs.ServiceID]
		if ss == nil {
			ss = &series{}
			h.Services[cs.ServiceID] = ss
		}
		ss.record(now, cs.CPUPct, float64(cs.MemUsedMB), 0)
	}
	h.dirty = true
}

// record folds a reading into both rings with a running average per bucket.
func (s *series) record(now int64, cpu, mem, temp float64) {
	s.Fine, s.fineN = fold(s.Fine, s.fineN, now-now%fineStep, fineKeep, cpu, mem, temp)
	s.Coarse, s.coarseN = fold(s.Coarse, s.coarseN, now-now%coarseStep, coarseKeep, cpu, mem, temp)
}

func fold(ring []Sample, n int, bucket int64, keep int, cpu, mem, temp float64) ([]Sample, int) {
	if len(ring) > 0 && ring[len(ring)-1].T == bucket {
		last := &ring[len(ring)-1]
		fn := float64(n)
		last.CPU = round1((last.CPU*fn + cpu) / (fn + 1))
		last.Mem = round1((last.Mem*fn + mem) / (fn + 1))
		last.Temp = round1((last.Temp*fn + temp) / (fn + 1))
		return ring, n + 1
	}
	ring = append(ring, Sample{T: bucket, CPU: round1(cpu), Mem: round1(mem), Temp: round1(temp)})
	if len(ring) > keep {
		ring = ring[len(ring)-keep:]
	}
	return ring, 1
}

// points returns a copy of one resolution of one entity's ring.
func (h *History) points(kind map[string]*series, id string, fine bool) []Sample {
	if h == nil {
		return []Sample{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := kind[id]
	if s == nil {
		return []Sample{}
	}
	src := s.Coarse
	if fine {
		src = s.Fine
	}
	out := make([]Sample, len(src))
	copy(out, src)
	return out
}

func (h *History) NodePoints(id string, fine bool) []Sample {
	if h == nil {
		return []Sample{}
	}
	return h.points(h.Nodes, id, fine)
}

func (h *History) ServicePoints(id string, fine bool) []Sample {
	if h == nil {
		return []Sample{}
	}
	return h.points(h.Services, id, fine)
}

// FleetPoints merges node rings across an org: CPU averages over the nodes
// that reported in a bucket, memory sums. Offline machines simply leave
// gaps in their own series and drop out of the average — never zero-filled.
func (h *History) FleetPoints(nodeIDs []string, fine bool) []Sample {
	if h == nil {
		return []Sample{}
	}
	type acc struct {
		cpu, mem float64
		n        int
	}
	merged := map[int64]*acc{}
	h.mu.RLock()
	for _, id := range nodeIDs {
		s := h.Nodes[id]
		if s == nil {
			continue
		}
		src := s.Coarse
		if fine {
			src = s.Fine
		}
		for _, p := range src {
			a := merged[p.T]
			if a == nil {
				a = &acc{}
				merged[p.T] = a
			}
			a.cpu += p.CPU
			a.mem += p.Mem
			a.n++
		}
	}
	h.mu.RUnlock()

	out := make([]Sample, 0, len(merged))
	for t, a := range merged {
		out = append(out, Sample{T: t, CPU: round1(a.cpu / float64(a.n)), Mem: round1(a.mem)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

// flushLoop persists the rings once a minute when dirty, and prunes series
// whose newest sample has aged past the coarse window (deleted services,
// removed machines) so the file never grows without bound.
func (h *History) flushLoop() {
	for range time.Tick(60 * time.Second) {
		h.mu.Lock()
		if !h.dirty {
			h.mu.Unlock()
			continue
		}
		cutoff := time.Now().Unix() - coarseStep*coarseKeep
		for _, kind := range []map[string]*series{h.Nodes, h.Services} {
			for id, s := range kind {
				if len(s.Coarse) == 0 || s.Coarse[len(s.Coarse)-1].T < cutoff {
					delete(kind, id)
				}
			}
		}
		data, err := json.Marshal(h)
		h.dirty = false
		h.mu.Unlock()
		if err == nil {
			tmp := h.path + ".tmp"
			if os.WriteFile(tmp, data, 0o600) == nil {
				_ = os.Rename(tmp, h.path)
			}
		}
	}
}

// ---- HTTP ----

type historyResponse struct {
	Scope   string   `json:"scope"`
	ID      string   `json:"id,omitempty"`
	Window  string   `json:"window"`
	StepSec int      `json:"stepSec"`
	Points  []Sample `json:"points"`
}

// handleMetricsHistory serves ?scope=fleet|node|service&id=…&window=24h|7d.
// Node and service scopes 404 outside the caller's active org, same as the
// live endpoints — history must not leak across tenants.
func (a *API) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	q := r.URL.Query()
	scope := q.Get("scope")
	if scope == "" {
		scope = "fleet"
	}
	window := q.Get("window")
	if window == "" {
		window = "24h"
	}
	if window != "24h" && window != "7d" {
		http.Error(w, "window must be 24h or 7d", http.StatusBadRequest)
		return
	}
	fine := window == "24h"
	step := fineStep
	if !fine {
		step = coarseStep
	}
	id := q.Get("id")

	var points []Sample
	switch scope {
	case "fleet":
		var ids []string
		a.Store.View(func() {
			for nid, n := range a.Store.Nodes {
				if n.OrgID == org.ID {
					ids = append(ids, nid)
				}
			}
		})
		points = a.History.FleetPoints(ids, fine)
	case "node":
		ok := false
		a.Store.View(func() {
			n := a.Store.Nodes[id]
			ok = n != nil && n.OrgID == org.ID
		})
		if !ok {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		points = a.History.NodePoints(id, fine)
	case "service":
		ok := false
		a.Store.View(func() {
			if s := a.Store.Services[id]; s != nil {
				ok = a.serviceInOrg(s, org.ID)
			}
		})
		if !ok {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		points = a.History.ServicePoints(id, fine)
	default:
		http.Error(w, "scope must be fleet, node, or service", http.StatusBadRequest)
		return
	}

	writeJSON(w, historyResponse{Scope: scope, ID: id, Window: window, StepSec: step, Points: points})
}
