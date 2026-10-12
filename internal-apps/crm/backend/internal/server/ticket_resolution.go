package server

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// ResolveTicket authorizes, checks the observed revision, and records the
// outcome in one transaction. A replay never executes the status change again.
func (s *Store) ResolveTicket(ticketID, installID, secret, actionID string, expected int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var hash string
	if err := tx.QueryRow(`SELECT poll_secret_hash FROM tickets WHERE id=? AND install_id=?`, ticketID, installID).Scan(&hash); err != nil {
		if err == sql.ErrNoRows {
			return false, errTicketCredential
		}
		return false, err
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(hashSecret(secret))) != 1 {
		return false, errTicketCredential
	}
	var previous int64
	var applied bool
	err = tx.QueryRow(`SELECT expected_revision, applied FROM ticket_resolutions WHERE ticket_id=? AND install_id=? AND action_id=?`, ticketID, installID, actionID).Scan(&previous, &applied)
	if err == nil {
		if previous != expected {
			return false, errors.New("resolution identity reused")
		}
		return applied, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	var revision int64
	if err := tx.QueryRow(`SELECT COALESCE((SELECT revision FROM ticket_revisions WHERE ticket_id=? AND install_id=?),0)`, ticketID, installID).Scan(&revision); err != nil {
		return false, err
	}
	applied = revision == expected
	if applied {
		if _, err := tx.Exec(`UPDATE tickets SET status='resolved', updated_at=? WHERE id=? AND install_id=?`, time.Now().UTC(), ticketID, installID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO ticket_resolutions(ticket_id,install_id,action_id,expected_revision,applied) VALUES(?,?,?,?,?)`, ticketID, installID, actionID, expected, applied); err != nil {
		return false, err
	}
	return applied, tx.Commit()
}

func (a *API) handleIngestResolution(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstallID        string `json:"installId"`
		PollSecret       string `json:"pollSecret"`
		ActionID         string `json:"actionId"`
		ExpectedRevision *int64 `json:"expectedRevision"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("ticketId")
	if !idRe.MatchString(id) || !installIDRe.MatchString(req.InstallID) || !idRe.MatchString(req.ActionID) || req.ExpectedRevision == nil || *req.ExpectedRevision < 0 {
		writeError(w, 400, "invalid resolution request")
		return
	}
	applied, err := a.Store.ResolveTicket(id, req.InstallID, req.PollSecret, req.ActionID, *req.ExpectedRevision)
	if err != nil {
		if errors.Is(err, errTicketCredential) {
			writeError(w, 403, "invalid poll secret")
			return
		}
		serverError(w, "resolve ticket", err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "actionId": req.ActionID, "applied": applied})
}
