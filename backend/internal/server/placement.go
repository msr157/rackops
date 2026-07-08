package server

import (
	"fmt"
	"sort"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// PlaceWorkload is the multi-constraint scheduler: it scores every online
// node against the request across CPU, memory, storage, thermal headroom,
// and hardware capability, and returns a ranked list.
//
// Score = weighted sum of headroom factors; hard constraints zero-out a node.
func (s *Store) PlaceWorkload(req types.PlacementRequest) []types.PlacementScore {
	nodes := s.SortedNodes()
	scores := make([]types.PlacementScore, 0, len(nodes))

	for _, n := range nodes {
		sc := types.PlacementScore{NodeID: n.ID, Viable: true}

		if n.Status != types.NodeOnline {
			sc.Viable = false
			sc.Reasons = append(sc.Reasons, "node offline")
			scores = append(scores, sc)
			continue
		}

		// ---- Hard constraints ----
		memFreeMB := n.Hardware.MemTotalMB - n.Metrics.MemUsedMB
		if req.MemMB > 0 && memFreeMB < req.MemMB {
			sc.Viable = false
			sc.Reasons = append(sc.Reasons, fmt.Sprintf("insufficient memory: %d MB free < %d MB required", memFreeMB, req.MemMB))
		}
		if req.NeedsGPU {
			ok := false
			for _, g := range n.Hardware.GPUs {
				if g.VRAMTotal-g.VRAMUsed >= req.MinVRAMMB {
					ok = true
					break
				}
			}
			if !ok {
				sc.Viable = false
				sc.Reasons = append(sc.Reasons, "no GPU with required free VRAM")
			}
		}
		var maxFreeGB float64
		hasNVMe := false
		for _, d := range n.Hardware.Drives {
			free := d.SizeGB - d.UsedGB
			if free > maxFreeGB {
				maxFreeGB = free
			}
			if d.Type == "nvme" && d.MountPoint != "" {
				hasNVMe = true
			}
		}
		if req.StorageGB > 0 && maxFreeGB < float64(req.StorageGB) {
			sc.Viable = false
			sc.Reasons = append(sc.Reasons, fmt.Sprintf("insufficient storage: %.0f GB free < %d GB required", maxFreeGB, req.StorageGB))
		}
		if !sc.Viable {
			scores = append(scores, sc)
			continue
		}

		// ---- Soft scoring (0..100) ----
		cpuHeadroom := (100 - n.Metrics.CPUPct) / 100 // 0..1
		memHeadroom := float64(memFreeMB) / float64(max64(n.Hardware.MemTotalMB, 1))

		// Thermal headroom: 45C or less = perfect, 90C+ = zero.
		thermal := 1.0
		if t := n.Metrics.TempC; t > 45 {
			thermal = 1 - (t-45)/45
			if thermal < 0 {
				thermal = 0
			}
		}

		// Storage headroom relative to request (or general free space).
		storage := 1.0
		if req.StorageGB > 0 && maxFreeGB > 0 {
			storage = clamp01((maxFreeGB - float64(req.StorageGB)) / maxFreeGB)
		}

		score := 35*cpuHeadroom + 30*memHeadroom + 20*thermal + 15*storage

		// Capability bonuses.
		if req.PreferNVMe && hasNVMe {
			score += 5
			sc.Reasons = append(sc.Reasons, "NVMe storage available")
		}
		if req.NeedsGPU {
			sc.Reasons = append(sc.Reasons, "GPU with sufficient free VRAM")
		}

		sc.Reasons = append(sc.Reasons,
			fmt.Sprintf("CPU %.0f%% used", n.Metrics.CPUPct),
			fmt.Sprintf("%.1f GB memory free", float64(memFreeMB)/1024),
			fmt.Sprintf("%.0f°C package temp", n.Metrics.TempC),
			fmt.Sprintf("%.0f GB max free storage", maxFreeGB),
		)
		if score > 100 {
			score = 100
		}
		sc.Score = round1(score)
		scores = append(scores, sc)
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Viable != scores[j].Viable {
			return scores[i].Viable
		}
		return scores[i].Score > scores[j].Score
	})
	return scores
}

// ---- Alert engine ----

// alertRule evaluates one condition against a node.
type alertRule struct {
	kind  string
	level types.AlertLevel
	check func(n *types.Node) (bool, string)
}

var nodeRules = []alertRule{
	{"cpu_high", types.AlertWarning, func(n *types.Node) (bool, string) {
		return n.Metrics.CPUPct > 90, fmt.Sprintf("CPU at %.0f%% on %s", n.Metrics.CPUPct, n.Name)
	}},
	{"mem_high", types.AlertWarning, func(n *types.Node) (bool, string) {
		return n.Metrics.MemPct > 90, fmt.Sprintf("Memory at %.0f%% on %s", n.Metrics.MemPct, n.Name)
	}},
	{"thermal", types.AlertCritical, func(n *types.Node) (bool, string) {
		return n.Metrics.TempC > 85, fmt.Sprintf("Package temperature %.0f°C on %s — thermal throttling likely", n.Metrics.TempC, n.Name)
	}},
	{"disk_full", types.AlertCritical, func(n *types.Node) (bool, string) {
		for _, d := range n.Hardware.Drives {
			if d.SizeGB > 0 && d.UsedGB/d.SizeGB > 0.92 {
				return true, fmt.Sprintf("Drive %s on %s is %.0f%% full", d.Device, n.Name, d.UsedGB/d.SizeGB*100)
			}
		}
		return false, ""
	}},
}

// EvaluateAlerts runs alert rules for a node, opening/resolving alerts.
func (s *Store) EvaluateAlerts(n *types.Node) []*types.Alert {
	var raised []*types.Alert
	for _, r := range nodeRules {
		firing, msg := r.check(n)
		key := r.kind + ":" + n.ID

		s.mu.Lock()
		var existing *types.Alert
		for _, a := range s.Alerts {
			if !a.Resolved && a.NodeID == n.ID && a.Kind == r.kind {
				existing = a
				break
			}
		}
		if firing && existing == nil {
			a := &types.Alert{
				ID: NewID(), NodeID: n.ID, Level: r.level, Kind: r.kind,
				Message: msg, CreatedAt: time.Now(),
			}
			s.Alerts[a.ID] = a
			s.dirty = true
			raised = append(raised, a)
		} else if !firing && existing != nil {
			existing.Resolved = true
			s.dirty = true
		}
		s.mu.Unlock()
		_ = key
	}
	return raised
}

// MarkOfflineNodes flags nodes that haven't reported within the deadline.
func (s *Store) MarkOfflineNodes(deadline time.Duration) []*types.Node {
	var wentOffline []*types.Node
	s.Mutate(func() {
		for _, n := range s.Nodes {
			if n.Status == types.NodeOnline && time.Since(n.LastSeen) > deadline {
				n.Status = types.NodeOffline
				a := &types.Alert{
					ID: NewID(), NodeID: n.ID, Level: types.AlertCritical, Kind: "node_down",
					Message: fmt.Sprintf("Node %s stopped reporting (last seen %s ago)", n.Name, time.Since(n.LastSeen).Round(time.Second)),
					CreatedAt: time.Now(),
				}
				s.Alerts[a.ID] = a
				wentOffline = append(wentOffline, n)
			}
		}
	})
	return wentOffline
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
