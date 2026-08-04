package server

import (
	"encoding/json"
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
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	taskID := NewID()
	ch := make(chan string, 1)
	pendingLogs.Store(taskID, ch)
	defer pendingLogs.Delete(taskID)

	since := ""
	if !svc.LogsClearedAt.IsZero() {
		since = svc.LogsClearedAt.Format(time.RFC3339)
	}
	// ComposeYML routes stack services to `compose logs`; ContainerID is the
	// fallback when the named container is gone but an old one still exists.
	payload, _ := json.Marshal(DeployPayload{
		ServiceID: svc.ID, Name: svc.Name, Since: since,
		ComposeYML: svc.ComposeYML, ExternalName: svc.ExternalName, ContainerID: svc.ContainerID,
	})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: taskID, Kind: types.TaskLogs, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
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

// logChunk is one increment of a live log stream, relayed to the dashboard over
// SSE as a `log_chunk` event. Stream is the correlation ID (deployment ID for
// deploy streams, a client-generated ID for runtime follows).
type logChunk struct {
	Stream    string `json:"stream"`
	ServiceID string `json:"serviceId,omitempty"`
	Kind      string `json:"kind"` // "deploy" | "runtime"
	Text      string `json:"text,omitempty"`
	End       bool   `json:"end,omitempty"`
}

// handleLogStream ingests an agent's live log stream (chunked POST body) and
// fans each chunk out to dashboard clients over SSE. For deploy streams it also
// appends to the in-progress Deployment.Log so the deployment record and any
// late-joining viewer stay complete. Auth: the agent join token.
func (a *API) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if a.JoinToken != "" && r.Header.Get("X-Rackops-Token") != a.JoinToken {
		http.Error(w, "invalid join token", http.StatusUnauthorized)
		return
	}
	stream := r.Header.Get("X-Rackops-Stream")
	if stream == "" {
		http.Error(w, "missing stream id", http.StatusBadRequest)
		return
	}
	serviceID := r.Header.Get("X-Rackops-Service")
	kind := r.Header.Get("X-Rackops-Kind")
	if kind == "" {
		kind = "runtime"
	}

	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			text := string(buf[:n])
			a.Hub.Broadcast("log_chunk", logChunk{Stream: stream, ServiceID: serviceID, Kind: kind, Text: text})
			// Keep the deployment record growing live so the Deployments tab and
			// reloads show progress, not just the final blob.
			if kind == "deploy" {
				a.appendDeploymentLog(stream, text)
			}
		}
		if err != nil {
			break // EOF or client gone
		}
	}
	a.Hub.Broadcast("log_chunk", logChunk{Stream: stream, ServiceID: serviceID, Kind: kind, End: true})
	w.WriteHeader(http.StatusNoContent)
}

// appendDeploymentLog appends streamed output to a deployment's log as it
// arrives. Bounded so a runaway build can't grow the record without limit; the
// final authoritative log still arrives in the deploy TaskResult.
func (a *API) appendDeploymentLog(deploymentID, text string) {
	a.Store.Mutate(func() {
		d := a.Store.Deployments[deploymentID]
		if d == nil {
			return
		}
		if len(d.Log) > 200_000 {
			return // cap live growth; TaskResult delivers the trimmed final log
		}
		d.Log += text
	})
}

// handleStartLogStream queues a logs-follow task so the owning agent begins
// streaming the service's runtime logs live over the log-stream channel. The
// client passes a stream ID it will match against `log_chunk` SSE events.
func (a *API) handleStartLogStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		StreamID string `json:"streamId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.StreamID == "" {
		http.Error(w, "streamId required", http.StatusBadRequest)
		return
	}
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[id] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	since := ""
	if !svc.LogsClearedAt.IsZero() {
		since = svc.LogsClearedAt.Format(time.RFC3339)
	}
	payload, _ := json.Marshal(DeployPayload{
		ServiceID: svc.ID, Name: svc.Name, Since: since,
		ComposeYML: svc.ComposeYML, ExternalName: svc.ExternalName, ContainerID: svc.ContainerID,
		StreamID: req.StreamID,
	})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskLogsFollow, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
	writeJSON(w, map[string]string{"streamId": req.StreamID})
}

// handleStopLogStream queues a logs-stop task ending a runtime follow.
func (a *API) handleStopLogStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	streamID := r.URL.Query().Get("streamId")
	if streamID == "" {
		http.Error(w, "streamId required", http.StatusBadRequest)
		return
	}
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[id] })
	if svc == nil {
		w.WriteHeader(http.StatusNoContent) // already gone; nothing to stop
		return
	}
	payload, _ := json.Marshal(DeployPayload{ServiceID: svc.ID, Name: svc.Name, StreamID: streamID})
	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskLogsStop, ServiceID: svc.ID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleServiceDeployments returns build history for a service, newest first.
func (a *API) handleServiceDeployments(w http.ResponseWriter, r *http.Request) {
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	id := svc.ID
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
