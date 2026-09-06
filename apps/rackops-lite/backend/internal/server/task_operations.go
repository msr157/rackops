package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func (a *API) handleServiceOperations(w http.ResponseWriter, r *http.Request) {
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	type summary struct {
		ID        string         `json:"id"`
		Kind      types.TaskKind `json:"kind"`
		State     string         `json:"state"`
		CreatedAt time.Time      `json:"createdAt"`
	}
	out := []summary{}
	a.Store.View(func() {
		for _, op := range a.Store.TaskOperations {
			if op.NodeID == svc.NodeID && op.Task.ServiceID == svc.ID && op.State != "completed" {
				out = append(out, summary{op.Task.ID, op.Task.Kind, op.State, op.Task.CreatedAt})
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	writeJSON(w, out)
}

func (a *API) handleResolveServiceOperation(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil || !requireServiceControl(w, member) {
		return
	}
	var req struct {
		Inspected bool `json:"inspected"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil || !req.Inspected {
		http.Error(w, "inspect the machine and application data before resuming automation", http.StatusBadRequest)
		return
	}
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	resolved := false
	a.Store.Mutate(func() {
		op := a.Store.TaskOperations[r.PathValue("operation")]
		if op != nil && op.NodeID == svc.NodeID && op.Task.ServiceID == svc.ID && op.State == "recovery_required" {
			op.State = "completed"
			resolved = true
		}
	})
	if !resolved {
		http.Error(w, "operation is not awaiting recovery for this app", http.StatusConflict)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		a.Store.Mutate(func() { a.Store.TaskOperations[r.PathValue("operation")].State = "recovery_required" })
		http.Error(w, "recovery acknowledgement could not be saved", http.StatusServiceUnavailable)
		return
	}
	a.serviceChanged(svc)
	writeJSON(w, map[string]bool{"ok": true})
}

// TaskOperation retains delivery ownership until a result is persisted. A
// retransmission always carries the same ID; the agent journals execution.
// Terminal records are tombstones and deliberately retain no task payload.
type TaskOperation struct {
	Task         types.AgentTask `json:"task"`
	NodeID       string          `json:"nodeId"`
	State        string          `json:"state"`
	LastDelivery time.Time       `json:"lastDelivery"`
	Attempts     int             `json:"attempts"`
}

// deliverTasks persists assignment before returning it to the network. Lease
// expiry permits transport redelivery, never a new execution identity.
func (s *Store) deliverTasks(nodeID string, slots int, now time.Time) ([]types.AgentTask, error) {
	var tasks []types.AgentTask
	s.Mutate(func() {
		var active []*TaskOperation
		for _, op := range s.TaskOperations {
			if op.NodeID == nodeID && op.State == "delivered" {
				active = append(active, op)
			}
		}
		sort.Slice(active, func(i, j int) bool { return active[i].Task.CreatedAt.Before(active[j].Task.CreatedAt) })
		for _, op := range active {
			if now.Sub(op.LastDelivery) >= 15*time.Second {
				op.LastDelivery = now
				op.Attempts++
				tasks = append(tasks, op.Task)
			}
		}
		// Count outstanding deliveries too: an agent may not have received a
		// response yet and therefore overestimate its free local slots.
		count := min(max(0, slots), max(0, 64-len(active)), len(s.TaskQueue[nodeID]))
		for _, task := range s.TaskQueue[nodeID][:count] {
			if s.TaskOperations[task.ID] != nil {
				continue
			}
			s.TaskOperations[task.ID] = &TaskOperation{Task: task, NodeID: nodeID, State: "delivered", LastDelivery: now, Attempts: 1}
			tasks = append(tasks, task)
		}
		s.TaskQueue[nodeID] = s.TaskQueue[nodeID][count:]
	})
	if err := s.persistOnce(); err != nil {
		return nil, err
	}
	return tasks, nil
}

// acceptTaskResult refuses a node reporting another node's operation or a
// forged service ID. Caller serializes result processing with deliveryMu.
func (a *API) acceptTaskResult(nodeID string, result types.TaskResult) bool {
	var found, done bool
	a.Store.View(func() {
		op := a.Store.TaskOperations[result.TaskID]
		if op != nil && op.NodeID == nodeID && op.Task.ServiceID == result.ServiceID {
			found, done = true, op.State == "completed" || op.State == "recovery_required"
		}
	})
	if !found {
		return false
	}
	if done {
		return true
	}
	a.applyTaskResult(nodeID, result)
	var changed *types.Service
	a.Store.Mutate(func() {
		op := a.Store.TaskOperations[result.TaskID]
		op.State = "completed"
		if result.Uncertain {
			op.State = "recovery_required"
		}
		op.Task.Payload = ""
		// Compose containers do not yet supply per-service inventory labels.
		// Apply acknowledged lifecycle outcomes instead of leaving a restarted
		// stack permanently marked stopped in the dashboard.
		if op.Task.Kind == types.TaskStart || op.Task.Kind == types.TaskStop || op.Task.Kind == types.TaskRestart {
			if svc := a.Store.Services[op.Task.ServiceID]; svc != nil && svc.NodeID == nodeID {
				if result.OK && !result.Uncertain {
					if op.Task.Kind == types.TaskStop {
						svc.Status = types.StatusStopped
					} else {
						svc.Status = types.StatusRunning
					}
				} else {
					svc.Status = types.StatusFailed
				}
				svc.UpdatedAt = time.Now()
				copy := *svc
				changed = &copy
			}
		}
	})
	if changed != nil {
		a.serviceChanged(changed)
	}
	return true
}

// Caller holds the store lock. Automated reconciliation must not issue a new
// operation while the prior one is running or needs human reconciliation.
func (s *Store) taskBlocksHealing(nodeID, serviceID string) bool {
	for _, op := range s.TaskOperations {
		if op.NodeID == nodeID && op.Task.ServiceID == serviceID && (op.State == "delivered" || op.State == "recovery_required") {
			return true
		}
	}
	for _, task := range s.TaskQueue[nodeID] {
		if task.ServiceID == serviceID {
			return true
		}
	}
	return false
}
