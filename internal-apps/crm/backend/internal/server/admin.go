package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// newID mints a random 16-hex id for agent messages and notes.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- GET /api/overview ----

func (a *API) handleOverview(w http.ResponseWriter, _ *http.Request) {
	ov, err := a.Store.BuildOverview(time.Now())
	if err != nil {
		serverError(w, "build overview", err)
		return
	}
	writeJSON(w, ov)
}

// ---- GET /api/installs ----

func (a *API) handleListInstalls(w http.ResponseWriter, _ *http.Request) {
	installs, err := a.Store.ListInstalls()
	if err != nil {
		serverError(w, "list installs", err)
		return
	}
	if installs == nil {
		installs = []*Install{}
	}
	writeJSON(w, installs)
}

// ---- GET /api/installs/{id} ----

func (a *API) handleGetInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	install, err := a.Store.GetInstall(id)
	if err != nil {
		serverError(w, "get install", err)
		return
	}
	if install == nil {
		writeError(w, http.StatusNotFound, "install not found")
		return
	}
	history, err := a.Store.SnapshotHistory(id, 50)
	if err != nil {
		serverError(w, "snapshot history", err)
		return
	}
	if history == nil {
		history = []*Snapshot{}
	}
	tickets, err := a.Store.TicketsForInstall(id)
	if err != nil {
		serverError(w, "install tickets", err)
		return
	}
	writeJSON(w, map[string]any{"install": install, "snapshots": history, "tickets": tickets})
}

// ---- GET /api/tickets ----

func (a *API) handleListTickets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !validStatuses[status] {
		writeError(w, http.StatusBadRequest, "invalid status filter")
		return
	}
	tickets, err := a.Store.ListTickets(status, 0)
	if err != nil {
		serverError(w, "list tickets", err)
		return
	}
	writeJSON(w, tickets)
}

// ---- GET /api/tickets/{id} ----

// adminTicket resolves a ticket for admin routes. Ticket ids are minted by
// instances, so an optional ?installId= disambiguates collisions.
func (a *API) adminTicket(w http.ResponseWriter, r *http.Request) *Ticket {
	t, err := a.Store.GetTicket(r.PathValue("id"), r.URL.Query().Get("installId"))
	if err != nil {
		serverError(w, "get ticket", err)
		return nil
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return nil
	}
	return t
}

func (a *API) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	if t := a.adminTicket(w, r); t != nil {
		writeJSON(w, t)
	}
}

// ---- POST /api/tickets/{id}/reply · /note ----

func (a *API) handleTicketReply(w http.ResponseWriter, r *http.Request) {
	a.addTicketMessage(w, r, "agent", "RackOps Support")
}

func (a *API) handleTicketNote(w http.ResponseWriter, r *http.Request) {
	a.addTicketMessage(w, r, "note", "Internal note")
}

func (a *API) addTicketMessage(w http.ResponseWriter, r *http.Request, author, name string) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	t := a.adminTicket(w, r)
	if t == nil {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len(req.Body) > 16<<10 {
		writeError(w, http.StatusBadRequest, "message must be at most 16 KiB")
		return
	}
	msg := TicketMessage{ID: newID(), Author: author, Name: name, Body: req.Body, At: time.Now()}
	if err := a.Store.AddMessage(t.ID, t.InstallID, msg); err != nil {
		serverError(w, "add message", err)
		return
	}
	// Return the refreshed thread so the UI can render without a refetch.
	updated, err := a.Store.GetTicket(t.ID, t.InstallID)
	if err != nil || updated == nil {
		serverError(w, "reload ticket", err)
		return
	}
	writeJSON(w, updated)
}

// ---- POST /api/tickets/{id}/status ----

func (a *API) handleTicketStatus(w http.ResponseWriter, r *http.Request) {
	t := a.adminTicket(w, r)
	if t == nil {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be open, pending or resolved")
		return
	}
	if err := a.Store.SetTicketStatus(t.ID, t.InstallID, req.Status, time.Now()); err != nil {
		serverError(w, "set status", err)
		return
	}
	updated, err := a.Store.GetTicket(t.ID, t.InstallID)
	if err != nil || updated == nil {
		serverError(w, "reload ticket", err)
		return
	}
	writeJSON(w, updated)
}
