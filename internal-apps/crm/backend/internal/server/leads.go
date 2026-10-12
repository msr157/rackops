package server

import (
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

func leadOffset(r *http.Request) int {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

type Lead struct {
	InstallID  string `json:"installId"`
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Industry   string `json:"industry"`
	Notes      string `json:"notes"`
	Status     string `json:"status"`
	ReceivedAt string `json:"receivedAt"`
}

func (a *API) handleIngestLead(w http.ResponseWriter, r *http.Request) {
	var lead Lead
	if !decodeJSON(w, r, &lead) {
		return
	}
	lead.Email = strings.TrimSpace(lead.Email)
	address, err := mail.ParseAddress(lead.Email)
	if err != nil || address.Address != lead.Email || len(lead.Email) > 320 || len(lead.ID) < 8 || len(lead.ID) > 128 || len(lead.InstallID) < 8 || len(lead.InstallID) > 128 || strings.TrimSpace(lead.Name) == "" || len(lead.Name) > 200 || strings.TrimSpace(lead.Industry) == "" || len(lead.Industry) > 200 || len(lead.Notes) > 8000 {
		writeError(w, 400, "invalid lead fields")
		return
	}
	// Public submissions are unverified. Retries cannot overwrite operator work
	// or the contents of an existing submission.
	_, err = a.Store.db.Exec(`INSERT INTO leads (install_id,id,email,name,industry,notes,received_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(install_id,id) DO NOTHING`, lead.InstallID, lead.ID, lead.Email, lead.Name, lead.Industry, lead.Notes, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		serverError(w, "record lead", err)
		return
	}
	writeJSON(w, map[string]string{"status": "recorded"})
}

func (a *API) handleListLeads(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.db.Query(`SELECT install_id,id,email,name,industry,notes,status,received_at FROM leads ORDER BY received_at DESC LIMIT 200 OFFSET ?`, leadOffset(r))
	if err != nil {
		serverError(w, "list leads", err)
		return
	}
	defer rows.Close()
	leads := []Lead{}
	for rows.Next() {
		var l Lead
		if err := rows.Scan(&l.InstallID, &l.ID, &l.Email, &l.Name, &l.Industry, &l.Notes, &l.Status, &l.ReceivedAt); err != nil {
			serverError(w, "read lead", err)
			return
		}
		leads = append(leads, l)
	}
	if err := rows.Err(); err != nil {
		serverError(w, "read leads", err)
		return
	}
	writeJSON(w, leads)
}

func (a *API) handleLeadStatus(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		InstallID string `json:"installId"`
		Status    string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "new", "contacted", "qualified", "closed", "spam":
	default:
		writeError(w, 400, "invalid status")
		return
	}
	result, err := a.Store.db.Exec(`UPDATE leads SET status=? WHERE install_id=? AND id=? AND status!='redacted'`, req.Status, req.InstallID, r.PathValue("id"))
	if err != nil {
		serverError(w, "update lead", err)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		serverError(w, "update lead", err)
		return
	}
	if count == 0 {
		writeError(w, 404, "lead not found")
		return
	}
	w.WriteHeader(204)
}

// Retain the submission identity as a tombstone so queued customer retries
// cannot recreate contact information after an operator removes it.
func (a *API) handleRedactLead(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		InstallID      string `json:"installId"`
		ExpectedStatus string `json:"expectedStatus"`
		ReceivedAt     string `json:"receivedAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.InstallID == "" || req.ReceivedAt == "" || (req.ExpectedStatus != "closed" && req.ExpectedStatus != "spam") {
		writeError(w, 400, "review a closed or spam lead before removing its contact details")
		return
	}
	result, err := a.Store.db.Exec(`UPDATE leads SET email='',name='',industry='',notes='',status='redacted' WHERE install_id=? AND id=? AND received_at=? AND status IN (?, 'redacted')`, req.InstallID, r.PathValue("id"), req.ReceivedAt, req.ExpectedStatus)
	if err != nil {
		serverError(w, "redact lead", err)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		serverError(w, "redact lead", err)
		return
	}
	if count == 0 {
		writeError(w, 409, "lead changed or unavailable; refresh before removing contact details")
		return
	}
	w.WriteHeader(204)
}
