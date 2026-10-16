package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Support tickets live locally and are forwarded (outbound-only) to the
// RackOps CRM, which support agents answer from. Replies come back via
// polling — a per-ticket secret saved before first forward
// authenticates every later exchange. Instances behind NAT need nothing
// but outbound HTTPS.

// ticketCategories/Severities mirror the CRM's validation exactly.
var ticketCategories = map[string]bool{
	"general": true, "deploys": true, "networking": true,
	"updates": true, "billing": true, "bug": true,
}
var ticketSeverities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

// ---- HTTP handlers ----

func (a *API) handleListTickets(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	admin := claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin)
	var out []*types.SupportTicket
	a.Store.View(func() {
		for _, t := range a.Store.Tickets {
			// Own tickets always; admins see the install's whole queue.
			if admin || t.CreatedBy == claims.Sub {
				out = append(out, t.Sanitized())
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	writeJSON(w, out)
}

func (a *API) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var req struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		Body     string `json:"body"`
		Email    string `json:"email"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	if len(req.Subject) > 512 || len(req.Body) > 16<<10 {
		http.Error(w, "subject must be at most 512 bytes and message at most 16 KiB", 400)
		return
	}
	if req.Subject == "" || req.Body == "" {
		http.Error(w, "subject and message are required", http.StatusBadRequest)
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	if req.Severity == "" {
		req.Severity = "normal"
	}
	if !ticketCategories[req.Category] || !ticketSeverities[req.Severity] {
		http.Error(w, "invalid category or severity", http.StatusBadRequest)
		return
	}

	var authorName, authorEmail string
	a.Store.View(func() {
		if u := a.Store.Users[claims.Sub]; u != nil {
			authorName = u.FullName
			if authorName == "" {
				authorName = u.Email
			}
			authorEmail = u.Email
		}
	})
	if e := strings.TrimSpace(req.Email); e != "" {
		authorEmail = e
	}
	at := strings.Index(authorEmail, "@")
	if len(authorEmail) < 3 || len(authorEmail) > 254 || at <= 0 || at == len(authorEmail)-1 || strings.ContainsAny(authorEmail, " \t\r\n") {
		http.Error(w, "valid reply email required", 400)
		return
	}

	now := time.Now()
	t := &types.SupportTicket{
		ID: NewID(), Subject: req.Subject, Category: req.Category, Severity: req.Severity,
		Status: types.TicketOpen, Email: authorEmail, CreatedBy: claims.Sub,
		ForwardPending: true,
		CreatedAt:      now, UpdatedAt: now,
		Messages: []types.TicketMessage{{
			ID: NewID(), Author: "user", Name: authorName, Body: req.Body, At: now,
		}},
	}
	a.Store.Mutate(func() {
		if a.Store.Tickets == nil {
			a.Store.Tickets = map[string]*types.SupportTicket{}
		}
		a.Store.Tickets[t.ID] = t
	})
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not save ticket; inspect your ticket list before retrying", 503)
		return
	}
	var response *types.SupportTicket
	a.Store.View(func() { response = t.Sanitized() })
	go a.forwardTicket(t.ID)
	writeJSON(w, response)
}

// ticketForUser loads {id} enforcing visibility (own, or admin).
func (a *API) ticketForUser(w http.ResponseWriter, r *http.Request) *types.SupportTicket {
	claims := claimsFrom(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil
	}
	admin := claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin)
	var t *types.SupportTicket
	a.Store.View(func() { t = a.Store.Tickets[r.PathValue("id")] })
	if t == nil || (!admin && t.CreatedBy != claims.Sub) {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return nil
	}
	return t
}

func (a *API) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	if t := a.ticketForUser(w, r); t != nil {
		var response *types.SupportTicket
		a.Store.View(func() { response = t.Sanitized() })
		writeJSON(w, response)
	}
}

func (a *API) handleReplyTicket(w http.ResponseWriter, r *http.Request) {
	t := a.ticketForUser(w, r)
	if t == nil {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Body) == "" || len(req.Body) > 16<<10 {
		http.Error(w, "message required (at most 16 KiB)", http.StatusBadRequest)
		return
	}
	claims := claimsFrom(r)
	var name string
	a.Store.View(func() {
		if u := a.Store.Users[claims.Sub]; u != nil {
			name = u.FullName
			if name == "" {
				name = u.Email
			}
		}
	})
	a.Store.Mutate(func() {
		t.Messages = append(t.Messages, types.TicketMessage{
			ID: NewID(), Author: "user", Name: name, Body: strings.TrimSpace(req.Body), At: time.Now(),
		})
		t.Status = types.TicketOpen // a customer message reopens the thread
		t.Resolution = nil
		t.ResolutionPending = false
		t.CRMRevision = nil
		t.UpdatedAt = time.Now()
		t.ForwardPending = true
	})
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not save reply; inspect the ticket before retrying", 503)
		return
	}
	var response *types.SupportTicket
	a.Store.View(func() { response = t.Sanitized() })
	go a.forwardTicket(t.ID)
	writeJSON(w, response)
}

func (a *API) handleResolveTicket(w http.ResponseWriter, r *http.Request) {
	t := a.ticketForUser(w, r)
	if t == nil {
		return
	}
	ready := false
	a.Store.Mutate(func() {
		if t.Resolution != nil {
			ready = true
			return
		}
		if t.ForwardPending || t.CRMRevision == nil {
			return
		}
		t.Resolution = &types.TicketResolution{ID: NewID(), Revision: *t.CRMRevision}
		t.ResolutionPending = true
		t.DeliveryError = ""
		t.UpdatedAt = time.Now()
		ready = true
	})
	if !ready {
		http.Error(w, "Wait for support synchronization before resolving this ticket. Admin may need an update.", 409)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not save ticket status", 503)
		return
	}
	var response *types.SupportTicket
	a.Store.View(func() { response = t.Sanitized() })
	go a.resolveTicket(t.ID)
	writeJSON(w, response)
}

func (a *API) resolveTicket(id string) {
	// Share serialization with customer-message delivery. If a new message
	// arrives during the request it is forwarded afterwards and reopens Admin.
	muAny, _ := forwardMu.LoadOrStore(id, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	var action types.TicketResolution
	var secret string
	a.Store.View(func() {
		if t := a.Store.Tickets[id]; t != nil && t.Resolution != nil {
			action = *t.Resolution
			secret = t.PollSecret
		}
	})
	if action.ID == "" || secret == "" {
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		return
	}
	var result struct {
		OK       bool   `json:"ok"`
		ActionID string `json:"actionId"`
		Applied  bool   `json:"applied"`
	}
	err := crmPost("/ingest/v1/tickets/"+id+"/resolve", map[string]any{"installId": a.InstallID, "pollSecret": secret, "actionId": action.ID, "expectedRevision": action.Revision}, &result)
	if err != nil || !result.OK || result.ActionID != action.ID {
		return
	}
	a.Store.Mutate(func() {
		t := a.Store.Tickets[id]
		if t == nil || t.Resolution == nil || t.Resolution.ID != action.ID || t.PollSecret != secret {
			return
		}
		t.Resolution = nil
		t.ResolutionPending = false
		t.CRMRevision = nil
		if result.Applied {
			t.Status = types.TicketResolved
		} else {
			t.DeliveryError = "Support activity changed before resolution. Review the latest replies and resolve again."
		}
		t.UpdatedAt = time.Now()
	})
	a.Store.persistOnce()
}

// ---- Usage snapshot (disclosed in the UI, sent with each forward) ----

// buildUsageSnapshot summarizes what this install uses — counts and feature
// flags only, never names, domains, env vars, or addresses.
func (a *API) buildUsageSnapshot() map[string]any {
	s := map[string]any{
		"version": ServerVersion, "os": runtime.GOOS, "arch": runtime.GOARCH,
	}
	a.Store.View(func() {
		nodes, online := 0, 0
		for _, n := range a.Store.Nodes {
			nodes++
			if n.Status == types.NodeOnline {
				online++
			}
		}
		byKind := map[string]int{}
		adopted := 0
		for _, svc := range a.Store.Services {
			byKind[string(svc.Kind)]++
			if svc.Adopted {
				adopted++
			}
		}
		var features []string
		if a.Store.Ingress.Enabled {
			features = append(features, "edge")
		}
		if a.Mesh != nil {
			features = append(features, "native-mesh")
		}
		if a.Cluster != nil {
			features = append(features, "ha-cluster")
		}
		if nodes > 1 {
			features = append(features, "multi-node")
		}
		if len(a.Store.Users) > 1 {
			features = append(features, "multi-user")
		}
		if len(a.Store.Orgs) > 1 {
			features = append(features, "multi-org")
		}

		hasDatabase := false
		hasBackups := false
		hasCompose := false
		hasGit := false
		hasQuickTunnel := false
		hasTemplate := false
		for _, svc := range a.Store.Services {
			img := strings.ToLower(svc.Image)
			if svc.Kind == types.KindDatabase || strings.Contains(img, "postgres") || strings.Contains(img, "mysql") || strings.Contains(img, "mariadb") {
				hasDatabase = true
			}
			if svc.QuickTunnelURL != "" {
				hasQuickTunnel = true
			}
			if svc.GitRepo != "" {
				hasGit = true
			}
			if svc.Kind == types.KindCompose {
				hasCompose = true
			}
			if svc.Kind == types.KindTemplate {
				hasTemplate = true
			}
		}
		if _, err := os.Stat(filepath.Join(a.DataDir, "backups")); err == nil {
			hasBackups = true
		}
		if hasDatabase {
			features = append(features, "database-management")
		}
		if hasBackups {
			features = append(features, "database-backups")
		}
		if hasGit {
			features = append(features, "git-deploys")
		}
		if hasQuickTunnel {
			features = append(features, "quick-tunnel")
		}
		if hasCompose {
			features = append(features, "docker-compose")
		}
		if hasTemplate {
			features = append(features, "templates")
		}
		if adopted > 0 {
			features = append(features, "adopted-workloads")
		}
		if len(a.Store.Domains) > 0 {
			features = append(features, "domains")
		}
		for _, cfg := range a.Store.OrgSettings {
			if cfg.AIProvider != "" || cfg.AnthropicKey != "" || cfg.GeminiKey != "" || cfg.OpenAIKey != "" {
				features = append(features, "copilot")
				break
			}
		}
		s["nodes"] = nodes
		s["nodesOnline"] = online
		s["services"] = len(a.Store.Services)
		s["servicesByKind"] = byKind
		s["adopted"] = adopted
		s["edgeEnabled"] = a.Store.Ingress.Enabled
		s["meshEnabled"] = a.Mesh != nil
		s["ha"] = a.Cluster != nil
		s["orgs"] = len(a.Store.Orgs)
		s["users"] = len(a.Store.Users)
		s["domains"] = len(a.Store.Domains)
		s["featuresUsed"] = features
	})
	return s
}

// ---- CRM forward + reply polling ----

// forwardMu serializes forwards per ticket so concurrent replies can't race
// the pollSecret issuance.
var forwardMu sync.Map // ticketID -> *sync.Mutex

func (a *API) forwardTicket(ticketID string) {
	muAny, _ := forwardMu.LoadOrStore(ticketID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	// Persist the credential before the first network request. Admin stores its
	// hash, so a lost response can be retried without losing polling access.
	var prepareErr error
	a.Store.Mutate(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		t.ForwardPending = true
		if t.PollSecret == "" {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				prepareErr = err
				return
			}
			t.PollSecret = hex.EncodeToString(raw)
		}
	})
	if prepareErr != nil {
		log.Print("support: could not prepare ticket credential")
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		log.Print("support: could not persist ticket before forwarding")
		return
	}

	var payload map[string]any
	var instanceName string
	var sentAt time.Time
	var sentSecret string
	var sentIDs []string
	var batchError string
	usage := a.buildUsageSnapshot()
	a.Store.View(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		instanceName = a.Store.InstanceName
		sentAt = t.UpdatedAt
		sentSecret = t.PollSecret
		// Only author=user messages go up; the CRM merges by message ID.
		msgs := make([]map[string]any, 0, len(t.Messages))
		for _, m := range t.Messages {
			if m.Author != "user" || t.DeliveredMessages[m.ID] {
				continue
			}
			msgs = append(msgs, map[string]any{
				"id": m.ID, "author": "user", "name": m.Name, "body": m.Body,
				"at": m.At.Format(time.RFC3339Nano),
			})
		}
		payload = map[string]any{
			"usage":       usage,
			"installId":   a.InstallID,
			"installName": instanceName,
			"version":     ServerVersion,
			"pollSecret":  t.PollSecret,
			"ticket": map[string]any{
				"id": t.ID, "subject": t.Subject, "category": t.Category,
				"severity": t.Severity, "status": string(t.Status), "email": t.Email,
				"createdAt": t.CreatedAt.Format(time.RFC3339Nano), "messages": msgs,
			},
		}
	})
	if payload != nil {
		ticket := payload["ticket"].(map[string]any)
		messages := ticket["messages"].([]map[string]any)
		batch := make([]map[string]any, 0)
		ticket["messages"] = batch
		encoded, err := json.Marshal(payload)
		if err != nil || len(encoded) > 192<<10 {
			batchError = "Ticket metadata exceeds delivery limits; contact support to recover this thread."
		} else {
			for _, message := range messages {
				candidate := append(batch, message)
				ticket["messages"] = candidate
				encoded, err := json.Marshal(payload)
				if err != nil || len(encoded) > 192<<10 {
					if len(batch) == 0 {
						batchError = "An existing message exceeds delivery limits; contact support to recover this thread. No messages were discarded."
					}
					break
				}
				batch = candidate
				sentIDs = append(sentIDs, message["id"].(string))
			}
		}
		ticket["messages"] = batch
	}
	if batchError != "" {
		a.Store.Mutate(func() {
			if t := a.Store.Tickets[ticketID]; t != nil {
				t.DeliveryError = batchError
			}
		})
		a.Store.persistOnce()
		return
	}
	if payload == nil {
		return
	}
	if len(sentSecret) == 64 {
		// Older Admin versions ignore the proposed first credential and issue a
		// different one. Check support before POST so rollout cannot orphan a
		// newly-created ticket if that incompatible response is lost.
		response, err := crmClient.Get(crmURL() + "/ingest/v1/capabilities")
		if err != nil {
			log.Print("support: Admin capability check unavailable; delivery remains queued")
			return
		}
		var capabilities struct {
			TicketClientCredentials bool `json:"ticketClientCredentials"`
		}
		decodeErr := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4096)).Decode(&capabilities)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || !capabilities.TicketClientCredentials {
			log.Print("support: update Admin before delivering tickets with durable client credentials")
			return
		}
	}

	var resp struct {
		OK         bool   `json:"ok"`
		PollSecret string `json:"pollSecret"`
	}
	if err := crmPost("/ingest/v1/tickets", payload, &resp); err != nil || !resp.OK || resp.PollSecret != sentSecret {
		log.Print("support: ticket forward not acknowledged; will retry (check Admin version and connectivity)")
		a.Store.Mutate(func() {
			if t := a.Store.Tickets[ticketID]; t != nil {
				t.ForwardPending = true
			}
		})
		return
	}
	a.Store.Mutate(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		// An older acknowledgement must not clear a newer local reply/status.
		if t.PollSecret != sentSecret {
			return
		}
		if t.DeliveredMessages == nil {
			t.DeliveredMessages = map[string]bool{}
		}
		for _, id := range sentIDs {
			t.DeliveredMessages[id] = true
		}
		t.DeliveryError = ""
		remaining := false
		for _, m := range t.Messages {
			if m.Author == "user" && !t.DeliveredMessages[m.ID] {
				remaining = true
				break
			}
		}
		if t.UpdatedAt.Equal(sentAt) && !remaining {
			t.ForwardPending = false
		}
	})
	if err := a.Store.persistOnce(); err != nil {
		log.Print("support: could not persist ticket acknowledgement; retry may repeat the same messages")
	}
}

// RunSupportSync is the background loop: retries failed forwards and polls
// the CRM for support-agent replies on open/pending tickets (~60s cadence).
// Leader-only under HA (followers would double-poll).
func (a *API) RunSupportSync() {
	for {
		select {
		case <-a.Store.stopCh:
			return
		case <-time.After(time.Minute):
		}
		if a.Cluster != nil && !a.Cluster.IsLeader() {
			continue
		}
		a.retryResetEmails(time.Now())
		var retry, poll, resolve []string
		a.Store.View(func() {
			for id, t := range a.Store.Tickets {
				if t.Resolution != nil {
					resolve = append(resolve, id)
				}
				if t.ForwardPending {
					retry = append(retry, id)
				}
				if t.PollSecret != "" {
					poll = append(poll, id)
				}
			}
		})
		for _, id := range retry {
			a.forwardTicket(id)
		}
		for _, id := range resolve {
			a.resolveTicket(id)
		}
		for _, id := range poll {
			a.pollTicketReplies(id)
		}
		a.retryEarlyAccess()
	}
}

// pollTicketReplies fetches agent replies newer than what we have.
func (a *API) pollTicketReplies(ticketID string) {
	var secret string
	var cursor int64
	assembling := false
	a.Store.View(func() {
		if t := a.Store.Tickets[ticketID]; t != nil {
			secret, cursor = t.PollSecret, t.ReplyCursor
			assembling = t.ReplyAssembly != nil
		}
	})
	if secret == "" {
		return
	}
	if assembling {
		a.pollReplyChunk(ticketID, secret, cursor)
		return
	}
	// Old Admin ignores cursor and returns the whole thread. Deduplication keeps
	// that fallback safe without dropping timestamp ties or backdated messages.
	url := fmt.Sprintf("%s/ingest/v1/tickets/%s/replies?installId=%s&cursor=%d", crmURL(), ticketID, a.InstallID, cursor)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Poll-Secret", secret)
	resp, err := crmClient.Do(req)
	if err != nil {
		return // offline is fine; next tick retries
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity {
		a.Store.Mutate(func() {
			if t := a.Store.Tickets[ticketID]; t != nil && t.PollSecret == secret && t.ReplyCursor == cursor {
				t.ReplySyncError = "A historical support reply could not be synchronized. Contact support to recover the retained message."
				t.CRMRevision = nil
			}
		})
		a.Store.persistOnce()
		a.pollReplyChunk(ticketID, secret, cursor)
		return
	}
	if resp.StatusCode != http.StatusOK {
		return
	}
	var out struct {
		Revision   *int64 `json:"revision"`
		NextCursor *int64 `json:"nextCursor"`
		Status     string `json:"status"`
		Replies    []struct {
			ID   string    `json:"id"`
			Name string    `json:"name"`
			Body string    `json:"body"`
			At   time.Time `json:"at"`
		} `json:"replies"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return
	}

	var updated *types.SupportTicket
	a.Store.Mutate(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil || t.PollSecret != secret || t.ReplyCursor != cursor {
			return
		}
		have := map[string]bool{}
		for _, m := range t.Messages {
			have[m.ID] = true
		}
		changed := false
		if t.ReplySyncError != "" {
			t.ReplySyncError = ""
			changed = true
		}
		if out.NextCursor != nil {
			if *out.NextCursor < cursor || (len(out.Replies) == 0 && *out.NextCursor != cursor) {
				return
			}
			t.ReplyCursor = *out.NextCursor
		}
		t.CRMRevision = out.Revision
		for _, r := range out.Replies {
			if have[r.ID] {
				continue
			}
			t.Messages = append(t.Messages, types.TicketMessage{
				ID: r.ID, Author: "agent", Name: r.Name, Body: r.Body, At: r.At,
			})
			have[r.ID] = true
			changed = true
		}
		switch out.Status {
		case "pending", "resolved", "open":
			if !t.ForwardPending && !t.ResolutionPending && string(t.Status) != out.Status {
				t.Status = types.TicketStatus(out.Status)
				changed = true
			}
		}
		if changed {
			t.UpdatedAt = time.Now()
			updated = t.Sanitized()
		}
	})
	a.Store.persistOnce()
	if updated != nil {
		a.Hub.Broadcast("ticket_update", updated)
	}
}
