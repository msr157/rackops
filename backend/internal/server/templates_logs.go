package server

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/catalog"
	"github.com/rackops/rackops/internal/types"
)

func (a *API) handleTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, catalog.All())
}

// pendingLogs holds waiters for log-fetch round-trips keyed by task ID.
var pendingLogs sync.Map // taskID -> chan string

// handleServiceLogs queues a logs task for the owning agent and waits for
// the result to come back on the next agent report (~2-4s).
func (a *API) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[id] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	taskID := NewID()
	ch := make(chan string, 1)
	pendingLogs.Store(taskID, ch)
	defer pendingLogs.Delete(taskID)

	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: taskID, Kind: types.TaskLogs, ServiceID: svc.ID,
		Payload: `{"serviceId":"` + svc.ID + `","name":"` + svc.Name + `"}`, CreatedAt: time.Now(),
	})

	select {
	case logs := <-ch:
		writeJSON(w, map[string]string{"logs": logs})
	case <-time.After(10 * time.Second):
		http.Error(w, "timed out waiting for agent (node offline?)", http.StatusGatewayTimeout)
	case <-r.Context().Done():
	}
}

// deliverLogs routes a logs task result to its waiting HTTP request.
func deliverLogs(taskID, logs string) bool {
	if v, ok := pendingLogs.Load(taskID); ok {
		select {
		case v.(chan string) <- logs:
		default:
		}
		return true
	}
	return false
}

// handleServiceDeployments returns build history for a service, newest first.
func (a *API) handleServiceDeployments(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var out []*types.Deployment
	a.Store.View(func() {
		for _, d := range a.Store.Deployments {
			if d.ServiceID == id {
				out = append(out, d)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	writeJSON(w, out)
}
