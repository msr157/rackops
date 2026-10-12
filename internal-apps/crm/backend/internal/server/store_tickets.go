package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// hashSecret returns the hex SHA-256 of a poll secret. Only the hash is
// ever stored.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// newPollSecret mints a 40-hex-char secret for a newly forwarded ticket.
func newPollSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate poll secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// GetTicket returns a ticket with its full thread (including internal
// notes) or nil when unknown. installID may be empty for admin lookups by
// ticket id alone — if two installs minted the same ticket id, the most
// recently updated one wins, so admin routes should pass both when known.
func (s *Store) GetTicket(id, installID string) (*Ticket, error) {
	q := `
SELECT t.id, t.install_id, COALESCE(i.name, ''), t.subject, t.category, t.severity,
	t.status, t.email, t.created_at, t.updated_at
FROM tickets t LEFT JOIN installs i ON i.id = t.install_id
WHERE t.id = ?`
	args := []any{id}
	if installID != "" {
		q += ` AND t.install_id = ?`
		args = append(args, installID)
	}
	q += ` ORDER BY t.updated_at DESC LIMIT 1`
	var t Ticket
	err := s.db.QueryRow(q, args...).Scan(&t.ID, &t.InstallID, &t.InstallName, &t.Subject,
		&t.Category, &t.Severity, &t.Status, &t.Email, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	msgs, err := s.ticketMessages(t.ID, t.InstallID, time.Time{}, nil)
	if err != nil {
		return nil, err
	}
	t.Messages = msgs
	return &t, nil
}

// ticketPollHash returns the stored poll-secret hash, or "" when the ticket
// doesn't exist.
func (s *Store) ticketPollHash(id, installID string) (string, error) {
	var h string
	err := s.db.QueryRow(`SELECT poll_secret_hash FROM tickets WHERE id = ? AND install_id = ?`,
		id, installID).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h, err
}

// CheckPollSecret verifies a presented poll secret against the stored hash.
// Returns (exists, ok).
func (s *Store) CheckPollSecret(id, installID, secret string) (bool, bool, error) {
	h, err := s.ticketPollHash(id, installID)
	if err != nil {
		return false, false, err
	}
	if h == "" {
		return false, false, nil
	}
	ok := subtle.ConstantTimeCompare([]byte(h), []byte(hashSecret(secret))) == 1
	return true, ok, nil
}

// UpsertTicket creates or updates a forwarded ticket and merges its
// messages by id. On first create it preserves a validated client credential
// or mints one for legacy clients. Authorization is checked in the transaction;
// on update it returns "" (the instance already holds it).
//
// Behavior on update: subject/category/severity/email refresh from the
// instance, but status is only forced back to "open" when the forward adds a
// new customer message — resolving/pending are agent-side decisions.
var errTicketCredential = errors.New("invalid ticket credential")

func (s *Store) UpsertTicket(t *Ticket, now time.Time, presentedSecret string) (pollSecret string, created bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var existingHash string
	err = tx.QueryRow(`SELECT poll_secret_hash FROM tickets WHERE id = ? AND install_id = ?`, t.ID, t.InstallID).Scan(&existingHash)
	if err != nil && err != sql.ErrNoRows {
		return "", false, err
	}
	created = err == sql.ErrNoRows
	if !created && subtle.ConstantTimeCompare([]byte(existingHash), []byte(hashSecret(presentedSecret))) != 1 {
		return "", false, errTicketCredential
	}

	if created {
		pollSecret = presentedSecret
		if pollSecret == "" {
			pollSecret, err = newPollSecret()
			if err != nil {
				return "", false, err
			}
		} else if !clientPollSecret.MatchString(pollSecret) {
			return "", false, errTicketCredential
		}
		status := t.Status
		if !validStatuses[status] || status == "" {
			status = "open"
		}
		createdAt := t.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		if _, err = tx.Exec(`
INSERT INTO tickets (id, install_id, subject, category, severity, status, email, created_at, updated_at, poll_secret_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.InstallID, t.Subject, t.Category, t.Severity, status, t.Email,
			createdAt.UTC(), now.UTC(), hashSecret(pollSecret)); err != nil {
			return "", false, fmt.Errorf("insert ticket: %w", err)
		}
	} else if _, err = tx.Exec(`
UPDATE tickets SET subject = ?, category = ?, severity = ?, email = ?, updated_at = ?
WHERE id = ? AND install_id = ?`,
		t.Subject, t.Category, t.Severity, t.Email, now.UTC(), t.ID, t.InstallID); err != nil {
		return "", false, fmt.Errorf("update ticket: %w", err)
	}

	// Merge messages by id; only customer messages arrive from instances.
	newUserMsg := false
	for _, m := range t.Messages {
		if m.Author != "user" {
			continue // instances may only forward customer messages
		}
		at := m.At
		if at.IsZero() {
			at = now
		}
		res, err := tx.Exec(`
INSERT INTO ticket_messages (id, ticket_id, install_id, author, name, body, at)
VALUES (?, ?, ?, 'user', ?, ?, ?)
ON CONFLICT(id, ticket_id, install_id) DO NOTHING`,
			m.ID, t.ID, t.InstallID, m.Name, m.Body, at.UTC())
		if err != nil {
			return "", false, fmt.Errorf("merge message: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			newUserMsg = true
		}
	}
	// A fresh customer message re-opens the ticket: it needs an agent.
	if !created && newUserMsg {
		if _, err = tx.Exec(`UPDATE tickets SET status = 'open' WHERE id = ? AND install_id = ?`,
			t.ID, t.InstallID); err != nil {
			return "", false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return pollSecret, created, nil
}

// ticketMessages returns thread messages, oldest first. When since is
// non-zero only messages strictly after it are returned. authors filters to
// the given author set when non-nil.
func (s *Store) ticketMessages(ticketID, installID string, since time.Time, authors []string) ([]TicketMessage, error) {
	q := `SELECT id, author, name, body, at FROM ticket_messages WHERE ticket_id = ? AND install_id = ?`
	args := []any{ticketID, installID}
	if !since.IsZero() {
		q += ` AND at > ?`
		args = append(args, since.UTC())
	}
	if len(authors) > 0 {
		q += ` AND author IN (?` + repeat(",?", len(authors)-1) + `)`
		for _, a := range authors {
			args = append(args, a)
		}
	}
	q += ` ORDER BY at ASC, id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketMessage{}
	for rows.Next() {
		var m TicketMessage
		if err := rows.Scan(&m.ID, &m.Author, &m.Name, &m.Body, &m.At); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

// AgentRepliesSince returns agent replies strictly after since, plus the
// ticket's current status. Internal notes are never included.
func (s *Store) AgentRepliesSince(ticketID, installID string, since time.Time) (string, []TicketMessage, error) {
	var status string
	err := s.db.QueryRow(`SELECT status FROM tickets WHERE id = ? AND install_id = ?`,
		ticketID, installID).Scan(&status)
	if err != nil {
		return "", nil, err
	}
	msgs, err := s.ticketMessages(ticketID, installID, since, []string{"agent"})
	if err != nil {
		return "", nil, err
	}
	return status, msgs, nil
}

// AgentRepliesPage uses persisted insertion sequence, never message timestamps.
var errReplyTooLarge = errors.New("historical reply exceeds delivery limit")

// Late/backdated replies are therefore visible and internal notes stay private.
func (s *Store) AgentRepliesPage(ticketID, installID string, after int64) (string, []TicketMessage, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, after, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM tickets WHERE id = ? AND install_id = ?`, ticketID, installID).Scan(&status); err != nil {
		return "", nil, after, err
	}
	rows, err := tx.Query(`SELECT d.sequence, m.id, m.author, m.name, m.body, m.at
 FROM ticket_message_delivery d JOIN ticket_messages m
 ON m.id = d.message_id AND m.ticket_id = d.ticket_id AND m.install_id = d.install_id
 WHERE d.ticket_id = ? AND d.install_id = ? AND d.sequence > ? AND m.author = 'agent'
 ORDER BY d.sequence LIMIT 100`, ticketID, installID, after)
	if err != nil {
		return "", nil, after, err
	}
	defer rows.Close()
	messages := []TicketMessage{}
	encodedSize := 0
	for rows.Next() {
		var m TicketMessage
		var sequence int64
		if err := rows.Scan(&sequence, &m.ID, &m.Author, &m.Name, &m.Body, &m.At); err != nil {
			return "", nil, after, err
		}
		// Including author makes this conservative relative to the public wire
		// representation. Leave ample room for the response envelope.
		raw, err := json.Marshal(m)
		if err != nil {
			return "", nil, after, err
		}
		if encodedSize+len(raw)+1 > 6<<20 {
			if len(messages) == 0 {
				return "", nil, after, errReplyTooLarge
			}
			break
		}
		encodedSize += len(raw) + 1
		after = sequence
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return "", nil, after, err
	}
	return status, messages, after, nil
}

// AddMessage appends an agent reply or internal note from the dashboard.
// Agent replies flip the ticket to "pending" (waiting on the customer);
// notes leave status untouched.
func (s *Store) AddMessage(ticketID, installID string, m TicketMessage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
INSERT INTO ticket_messages (id, ticket_id, install_id, author, name, body, at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, ticketID, installID, m.Author, m.Name, m.Body, m.At.UTC()); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	if m.Author == "agent" {
		if _, err := tx.Exec(`UPDATE tickets SET status = 'pending', updated_at = ? WHERE id = ? AND install_id = ?`,
			m.At.UTC(), ticketID, installID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`UPDATE tickets SET updated_at = ? WHERE id = ? AND install_id = ?`,
			m.At.UTC(), ticketID, installID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTicketStatus manually moves a ticket between open/pending/resolved.
func (s *Store) SetTicketStatus(ticketID, installID, status string, now time.Time) error {
	res, err := s.db.Exec(`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ? AND install_id = ?`,
		status, now.UTC(), ticketID, installID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListTickets returns tickets (without threads), optionally filtered by
// status, most recently updated first.
func (s *Store) ListTickets(status string, limit int) ([]*Ticket, error) {
	q := `
SELECT t.id, t.install_id, COALESCE(i.name, ''), t.subject, t.category, t.severity,
	t.status, t.email, t.created_at, t.updated_at
FROM tickets t LEFT JOIN installs i ON i.id = t.install_id`
	args := []any{}
	if status != "" {
		q += ` WHERE t.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY t.updated_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Ticket{}
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.InstallID, &t.InstallName, &t.Subject, &t.Category,
			&t.Severity, &t.Status, &t.Email, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// TicketsForInstall lists an install's tickets, newest activity first.
func (s *Store) TicketsForInstall(installID string) ([]*Ticket, error) {
	rows, err := s.db.Query(`
SELECT t.id, t.install_id, COALESCE(i.name, ''), t.subject, t.category, t.severity,
	t.status, t.email, t.created_at, t.updated_at
FROM tickets t LEFT JOIN installs i ON i.id = t.install_id
WHERE t.install_id = ? ORDER BY t.updated_at DESC`, installID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Ticket{}
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.InstallID, &t.InstallName, &t.Subject, &t.Category,
			&t.Severity, &t.Status, &t.Email, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// LogResetEmail records a password-reset relay attempt.
func (s *Store) LogResetEmail(installID, email, instanceName, status string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO reset_emails (install_id, email, instance_name, at, status) VALUES (?, ?, ?, ?, ?)`,
		installID, email, instanceName, at.UTC(), status)
	return err
}
