package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Hub fans out server-sent events to connected dashboard clients. Each client
// watches ONE organization: org-scoped events (telemetry, service updates,
// alerts) reach only that org's viewers, while install-wide events (org "")
// reach everyone. Filtering happens here, server-side — a browser must never
// receive another org's data just to discard it.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]string // client -> org ID it watches ("" = all)
}

// NewHub creates an SSE hub.
func NewHub() *Hub {
	return &Hub{clients: map[chan []byte]string{}}
}

// Broadcast sends an install-wide event to every connected client.
func (h *Hub) Broadcast(event string, payload any) {
	h.BroadcastOrg("", event, payload)
}

// BroadcastOrg sends an event to clients watching the given org. An empty
// orgID means install-wide: every client receives it.
func (h *Hub) BroadcastOrg(orgID, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, watching := range h.clients {
		if orgID != "" && watching != "" && watching != orgID {
			continue
		}
		select {
		case ch <- msg:
		default: // slow client; drop message rather than block telemetry
		}
	}
}

// Serve streams events to one client, scoped to orgID.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, orgID string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Buffer generously: live log streams (deploy builds, `logs -f` tails) can
	// burst many chunks between flushes. Full still drops rather than blocks
	// (see BroadcastOrg) so a stalled client never wedges telemetry.
	ch := make(chan []byte, 512)
	h.mu.Lock()
	h.clients[ch] = orgID
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()

	for {
		select {
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ServeHTTP keeps Hub usable as a bare handler (unscoped; used in tests).
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, "")
}
