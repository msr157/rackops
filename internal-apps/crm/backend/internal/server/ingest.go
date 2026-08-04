package server

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// installIDRe: instances identify with the UUID they minted at first boot.
var installIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// idRe covers instance-minted ticket/message ids: short, URL-safe tokens.
var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validEmail(s string) bool {
	at := strings.Index(s, "@")
	return len(s) >= 3 && len(s) <= 254 && at > 0 && at < len(s)-1 && !strings.ContainsAny(s, " \t\r\n")
}

// ---- POST /ingest/v1/tickets ----

type ingestTicketMessage struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Name   string    `json:"name"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

type ingestTicket struct {
	ID        string                `json:"id"`
	Subject   string                `json:"subject"`
	Category  string                `json:"category"`
	Severity  string                `json:"severity"`
	Status    string                `json:"status"`
	Email     string                `json:"email"`
	CreatedAt time.Time             `json:"createdAt"`
	Messages  []ingestTicketMessage `json:"messages"`
}

type ingestTicketReq struct {
	InstallID   string         `json:"installId"`
	InstallName string         `json:"installName"`
	Version     string         `json:"version"`
	PollSecret  string         `json:"pollSecret"`
	Ticket      *ingestTicket  `json:"ticket"`
	Usage       *UsageSnapshot `json:"usage"`
}

// handleIngestTicket is the upsert-forward endpoint. First forward of a
// (ticketId, installId) pair creates the ticket and issues its poll secret;
// later forwards must present that secret. Messages merge by id.
func (a *API) handleIngestTicket(w http.ResponseWriter, r *http.Request) {
	var req ingestTicketReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if !installIDRe.MatchString(req.InstallID) {
		writeError(w, http.StatusBadRequest, "invalid installId")
		return
	}
	t := req.Ticket
	if t == nil {
		writeError(w, http.StatusBadRequest, "ticket is required")
		return
	}
	switch {
	case !idRe.MatchString(t.ID):
		writeError(w, http.StatusBadRequest, "invalid ticket id")
		return
	case strings.TrimSpace(t.Subject) == "":
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	case !validCategories[t.Category]:
		writeError(w, http.StatusBadRequest, "invalid category")
		return
	case !validSeverities[t.Severity]:
		writeError(w, http.StatusBadRequest, "invalid severity")
		return
	case t.Status != "" && !validStatuses[t.Status]:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	case !validEmail(t.Email):
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	}
	msgs := make([]TicketMessage, 0, len(t.Messages))
	for _, m := range t.Messages {
		if m.Author != "user" {
			writeError(w, http.StatusBadRequest, "messages must have author \"user\"")
			return
		}
		if !idRe.MatchString(m.ID) {
			writeError(w, http.StatusBadRequest, "invalid message id")
			return
		}
		if strings.TrimSpace(m.Body) == "" {
			writeError(w, http.StatusBadRequest, "message body is required")
			return
		}
		msgs = append(msgs, TicketMessage{ID: m.ID, Author: "user", Name: m.Name, Body: m.Body, At: m.At})
	}

	// Existing tickets demand the correct poll secret before anything merges.
	exists, ok, err := a.Store.CheckPollSecret(t.ID, req.InstallID, req.PollSecret)
	if err != nil {
		serverError(w, "check poll secret", err)
		return
	}
	if exists && !ok {
		writeError(w, http.StatusForbidden, "invalid poll secret")
		return
	}

	now := time.Now()
	// Refresh the install record (+ usage snapshot) on every forward.
	if err := a.Store.UpsertInstall(req.InstallID, req.InstallName, req.Version, req.Usage, now); err != nil {
		serverError(w, "upsert install", err)
		return
	}

	secret, created, err := a.Store.UpsertTicket(&Ticket{
		ID: t.ID, InstallID: req.InstallID, Subject: t.Subject, Category: t.Category,
		Severity: t.Severity, Status: t.Status, Email: t.Email, CreatedAt: t.CreatedAt,
		Messages: msgs,
	}, now)
	if err != nil {
		serverError(w, "upsert ticket", err)
		return
	}
	if !created {
		// The instance already holds the secret it just presented.
		secret = req.PollSecret
	}
	writeJSON(w, map[string]any{"ok": true, "pollSecret": secret})
}

// ---- GET /ingest/v1/tickets/{ticketId}/replies ----

// handleIngestReplies returns agent replies strictly after `since` plus the
// ticket's current status. Internal notes are never included. Unknown
// tickets and bad secrets are indistinguishable (both 403).
func (a *API) handleIngestReplies(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("ticketId")
	installID := r.URL.Query().Get("installId")
	if !idRe.MatchString(ticketID) || !installIDRe.MatchString(installID) {
		writeError(w, http.StatusBadRequest, "invalid ticket or install id")
		return
	}
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		var err error
		since, err = time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since timestamp (RFC3339)")
			return
		}
	}
	exists, ok, err := a.Store.CheckPollSecret(ticketID, installID, r.Header.Get("X-Poll-Secret"))
	if err != nil {
		serverError(w, "check poll secret", err)
		return
	}
	if !exists || !ok {
		writeError(w, http.StatusForbidden, "invalid poll secret")
		return
	}
	status, msgs, err := a.Store.AgentRepliesSince(ticketID, installID, since)
	if err != nil {
		serverError(w, "load replies", err)
		return
	}
	type reply struct {
		ID   string    `json:"id"`
		Name string    `json:"name"`
		Body string    `json:"body"`
		At   time.Time `json:"at"`
	}
	replies := make([]reply, 0, len(msgs))
	for _, m := range msgs {
		replies = append(replies, reply{ID: m.ID, Name: m.Name, Body: m.Body, At: m.At})
	}
	writeJSON(w, map[string]any{"status": status, "replies": replies})
}

// ---- POST /ingest/v1/reset-email ----

type resetEmailReq struct {
	InstallID    string `json:"installId"`
	InstanceName string `json:"instanceName"`
	Email        string `json:"email"`
	Link         string `json:"link"`
	Version      string `json:"version"`
}

// handleIngestResetEmail relays a password-reset email on behalf of an
// instance. Stricter rate limits apply: 5/hour per IP AND per address.
func (a *API) handleIngestResetEmail(w http.ResponseWriter, r *http.Request) {
	var req resetEmailReq
	if !decodeJSON(w, r, &req) {
		return
	}
	switch {
	case !installIDRe.MatchString(req.InstallID):
		writeError(w, http.StatusBadRequest, "invalid installId")
		return
	case !validEmail(req.Email):
		writeError(w, http.StatusBadRequest, "invalid email")
		return
	case !strings.HasPrefix(req.Link, "http://") && !strings.HasPrefix(req.Link, "https://"):
		writeError(w, http.StatusBadRequest, "invalid link")
		return
	case len(req.Link) > 2048 || len(req.InstanceName) > 200:
		writeError(w, http.StatusBadRequest, "field too long")
		return
	}
	if !a.resetIPLimiter.allow("ip:"+clientIP(r)) ||
		!a.resetEmailLimiter.allow("email:"+strings.ToLower(req.Email)) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	now := time.Now()
	// Keep the install roster fresh even for reset-only traffic.
	if err := a.Store.UpsertInstall(req.InstallID, req.InstanceName, req.Version, nil, now); err != nil {
		serverError(w, "upsert install", err)
		return
	}

	if !a.Mailer.Configured() {
		_ = a.Store.LogResetEmail(req.InstallID, req.Email, req.InstanceName, "unconfigured", now)
		writeError(w, http.StatusServiceUnavailable, "email relay not configured")
		return
	}
	name := req.InstanceName
	if name == "" {
		name = "RackOps"
	}
	text, html := resetEmailBodies(name, req.Link)
	if err := a.Mailer.Send(req.Email, "Reset your RackOps password", text, html); err != nil {
		_ = a.Store.LogResetEmail(req.InstallID, req.Email, req.InstanceName, "failed", now)
		serverError(w, "send reset email", err)
		return
	}
	if err := a.Store.LogResetEmail(req.InstallID, req.Email, req.InstanceName, "sent", now); err != nil {
		serverError(w, "log reset email", err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---- POST /ingest/v1/snapshots ----

type snapshotReq struct {
	InstallID   string         `json:"installId"`
	InstallName string         `json:"installName"`
	Version     string         `json:"version"`
	Usage       *UsageSnapshot `json:"usage"`
}

// handleIngestSnapshot receives opt-in periodic telemetry.
func (a *API) handleIngestSnapshot(w http.ResponseWriter, r *http.Request) {
	var req snapshotReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if !installIDRe.MatchString(req.InstallID) {
		writeError(w, http.StatusBadRequest, "invalid installId")
		return
	}
	if req.Usage == nil {
		writeError(w, http.StatusBadRequest, "usage is required")
		return
	}
	if err := a.Store.UpsertInstall(req.InstallID, req.InstallName, req.Version, req.Usage, time.Now()); err != nil {
		serverError(w, "upsert install", err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
