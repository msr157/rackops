package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Hub fans out server-sent events to connected dashboard clients.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

// NewHub creates an SSE hub.
func NewHub() *Hub {
	return &Hub{clients: map[chan []byte]bool{}}
}

// Broadcast sends a typed event to all connected clients.
func (h *Hub) Broadcast(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client; drop message rather than block telemetry
		}
	}
}

// ServeHTTP implements the SSE endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = true
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
