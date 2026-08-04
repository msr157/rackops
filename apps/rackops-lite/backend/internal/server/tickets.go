package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Support tickets live locally and are forwarded (outbound-only) to the
// RackOps CRM, which support agents answer from. Replies come back via
// polling — a per-ticket secret issued by the CRM at first forward
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
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

	now := time.Now()
	t := &types.SupportTicket{
		ID: NewID(), Subject: req.Subject, Category: req.Category, Severity: req.Severity,
		Status: types.TicketOpen, Email: authorEmail, CreatedBy: claims.Sub,
		CreatedAt: now, UpdatedAt: now,
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
	a.Store.AddEvent("support", "Support ticket opened: "+t.Subject, "", "")
	go a.forwardTicket(t.ID)
	writeJSON(w, t.Sanitized())
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
		writeJSON(w, t.Sanitized())
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Body) == "" {
		http.Error(w, "message required", http.StatusBadRequest)
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
		t.UpdatedAt = time.Now()
	})
	go a.forwardTicket(t.ID)
	writeJSON(w, t.Sanitized())
}

func (a *API) handleResolveTicket(w http.ResponseWriter, r *http.Request) {
	t := a.ticketForUser(w, r)
	if t == nil {
		return
	}
	a.Store.Mutate(func() {
		t.Status = types.TicketResolved
		t.UpdatedAt = time.Now()
	})
	go a.forwardTicket(t.ID) // tell the CRM it's resolved
	writeJSON(w, t.Sanitized())
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
		for _, svc := range a.Store.Services {
			if svc.QuickTunnelURL != "" {
				features = append(features, "quick-tunnel")
				break
			}
		}
		for _, svc := range a.Store.Services {
			if svc.GitRepo != "" {
				features = append(features, "git-deploys")
				break
			}
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

	var payload map[string]any
	var instanceName string
	a.Store.View(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		instanceName = a.Store.InstanceName
		// Only author=user messages go up; the CRM merges by message ID.
		msgs := make([]map[string]any, 0, len(t.Messages))
		for _, m := range t.Messages {
			if m.Author != "user" {
				continue
			}
			msgs = append(msgs, map[string]any{
				"id": m.ID, "author": "user", "name": m.Name, "body": m.Body,
				"at": m.At.Format(time.RFC3339Nano),
			})
		}
		payload = map[string]any{
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
	if payload == nil {
		return
	}
	payload["usage"] = a.buildUsageSnapshot()

	var resp struct {
		OK         bool   `json:"ok"`
		PollSecret string `json:"pollSecret"`
	}
	if err := crmPost("/ingest/v1/tickets", payload, &resp); err != nil {
		log.Printf("support: ticket forward failed (will retry): %v", err)
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
		t.ForwardPending = false
		// The pollSecret from the FIRST forward must be persisted — losing it
		// means never reading replies for this ticket again.
		if t.PollSecret == "" && resp.PollSecret != "" {
			t.PollSecret = resp.PollSecret
		}
	})
}

// RunSupportSync is the background loop: retries failed forwards and polls
// the CRM for support-agent replies on open/pending tickets (~60s cadence).
// Leader-only under HA (followers would double-poll).
func (a *API) RunSupportSync() {
	for {
		time.Sleep(time.Minute)
		if a.Cluster != nil && !a.Cluster.IsLeader() {
			continue
		}
		var retry, poll []string
		a.Store.View(func() {
			for id, t := range a.Store.Tickets {
				if t.ForwardPending {
					retry = append(retry, id)
				}
				if t.PollSecret != "" && t.Status != types.TicketResolved {
					poll = append(poll, id)
				}
			}
		})
		for _, id := range retry {
			a.forwardTicket(id)
		}
		for _, id := range poll {
			a.pollTicketReplies(id)
		}
	}
}

// pollTicketReplies fetches agent replies newer than what we have.
func (a *API) pollTicketReplies(ticketID string) {
	var since, secret string
	a.Store.View(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		secret = t.PollSecret
		last := t.CreatedAt
		for _, m := range t.Messages {
			if m.Author == "agent" && m.At.After(last) {
				last = m.At
			}
		}
		since = last.Format(time.RFC3339Nano)
	})
	if secret == "" {
		return
	}

	url := fmt.Sprintf("%s/ingest/v1/tickets/%s/replies?installId=%s&since=%s",
		crmURL(), ticketID, a.InstallID, since)
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
	if resp.StatusCode != http.StatusOK {
		return
	}
	var out struct {
		Status  string `json:"status"`
		Replies []struct {
			ID   string    `json:"id"`
			Name string    `json:"name"`
			Body string    `json:"body"`
			At   time.Time `json:"at"`
		} `json:"replies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return
	}

	var updated *types.SupportTicket
	a.Store.Mutate(func() {
		t := a.Store.Tickets[ticketID]
		if t == nil {
			return
		}
		have := map[string]bool{}
		for _, m := range t.Messages {
			have[m.ID] = true
		}
		changed := false
		for _, r := range out.Replies {
			if have[r.ID] {
				continue
			}
			t.Messages = append(t.Messages, types.TicketMessage{
				ID: r.ID, Author: "agent", Name: r.Name, Body: r.Body, At: r.At,
			})
			changed = true
		}
		switch out.Status {
		case "pending", "resolved", "open":
			if string(t.Status) != out.Status {
				t.Status = types.TicketStatus(out.Status)
				changed = true
			}
		}
		if changed {
			t.UpdatedAt = time.Now()
			updated = t
		}
	})
	if updated != nil {
		a.Hub.Broadcast("ticket_update", updated.Sanitized())
	}
}
