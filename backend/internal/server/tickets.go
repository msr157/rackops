package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Support tickets live locally (the self-hosted instance owns its data) and
// are forwarded — together with an anonymous usage snapshot — to the CRM
// ingest endpoint configured in Settings, so support and product teams see
// what customers run and where they struggle.

// ServerVersion is stamped by main() so API responses and CRM payloads can
// report the running build.
var ServerVersion = "dev"

// buildUsageSnapshot assembles the anonymous context bundle for the CRM.
func (a *API) buildUsageSnapshot() types.UsageSnapshot {
	snap := types.UsageSnapshot{
		Version:        ServerVersion,
		ServicesByKind: map[string]int{},
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
	features := map[string]bool{}
	a.Store.View(func() {
		snap.Nodes = len(a.Store.Nodes)
		snap.Services = len(a.Store.Services)
		snap.DomainsCount = len(a.Store.Domains)
		snap.EdgeEnabled = a.Store.Ingress.Enabled
		snap.HAEnabled = len(a.Store.ClusterHTTP) > 1
		for _, s := range a.Store.Services {
			snap.ServicesByKind[string(s.Kind)]++
			if s.Adopted {
				snap.AdoptedCount++
			}
			if s.QuickTunnelURL != "" {
				features["quick-tunnel"] = true
			}
			if s.TemplateID != "" {
				features["templates"] = true
			}
			if s.GPU {
				features["gpu"] = true
			}
			if s.ComposeYML != "" {
				features["compose"] = true
			}
			if s.GitRepo != "" {
				features["git-deploy"] = true
			}
		}
		if snap.AdoptedCount > 0 {
			features["adoption"] = true
		}
		if a.Store.Ingress.ACMEEmail != "" {
			features["lets-encrypt"] = true
		}
		if a.Store.Settings.AIProvider != "" || a.Store.Settings.AnthropicKey != "" ||
			a.Store.Settings.GeminiKey != "" || a.Store.Settings.OpenAIKey != "" {
			features["copilot"] = true
		}
	})
	if a.Mesh != nil {
		snap.MeshEnabled = true
		features["mesh"] = true
	}
	for f := range features {
		snap.FeaturesUsed = append(snap.FeaturesUsed, f)
	}
	sort.Strings(snap.FeaturesUsed)
	return snap
}

// forwardTicket delivers a ticket to the CRM endpoint (best-effort, retried
// on the next message if it fails). Payload: ticket + usage snapshot.
func (a *API) forwardTicket(t *types.SupportTicket) {
	var endpoint, key string
	a.Store.View(func() {
		endpoint = a.Store.Settings.CRMEndpoint
		key = a.Store.Settings.CRMKey
	})
	if endpoint == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"ticket": t,
		"usage":  a.buildUsageSnapshot(),
	})
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.Store.AddEvent("support", fmt.Sprintf("Ticket %q: CRM forward failed (%v) — kept locally, will retry on next update", t.Subject, err), "", "")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		a.Store.AddEvent("support", fmt.Sprintf("Ticket %q: CRM forward rejected (HTTP %d) — kept locally", t.Subject, resp.StatusCode), "", "")
		return
	}
	a.Store.Mutate(func() { t.Forwarded = true })
}

func (a *API) handleListTickets(w http.ResponseWriter, r *http.Request) {
	var out []*types.SupportTicket
	a.Store.View(func() {
		for _, t := range a.Store.Tickets {
			out = append(out, t)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	writeJSON(w, out)
}

func (a *API) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" {
		http.Error(w, "subject and description are required", http.StatusBadRequest)
		return
	}
	claims := claimsFrom(r)
	author, authorID := "operator", ""
	if claims != nil {
		authorID = claims.Sub
		a.Store.View(func() {
			if u := a.Store.Users[claims.Sub]; u != nil {
				author = u.FullName
				if author == "" {
					author = u.Email
				}
				if req.Email == "" {
					req.Email = u.Email
				}
			}
		})
	}
	cat := req.Category
	switch cat {
	case "bug", "question", "feature-request", "billing", "other":
	default:
		cat = "other"
	}
	sev := req.Severity
	switch sev {
	case "low", "normal", "high", "urgent":
	default:
		sev = "normal"
	}
	now := time.Now()
	t := &types.SupportTicket{
		ID: NewID(), Subject: strings.TrimSpace(req.Subject),
		Category: cat, Severity: sev, Status: types.TicketOpen,
		CreatedBy: authorID, Email: strings.TrimSpace(req.Email),
		Messages: []types.TicketMessage{{
			ID: NewID(), AuthorID: authorID, Author: author,
			Body: strings.TrimSpace(req.Body), CreatedAt: now,
		}},
		CreatedAt: now, UpdatedAt: now,
	}
	a.Store.Mutate(func() { a.Store.Tickets[t.ID] = t })
	a.Store.AddEvent("support", "Support ticket opened: "+t.Subject, "", "")
	go a.forwardTicket(t)
	writeJSON(w, t)
}

func (a *API) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	var t *types.SupportTicket
	a.Store.View(func() { t = a.Store.Tickets[r.PathValue("id")] })
	if t == nil {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	writeJSON(w, t)
}

// handleTicketReply appends a message and re-forwards to the CRM.
func (a *API) handleTicketReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body   string `json:"body"`
		Status string `json:"status"` // optional status change with the reply
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var t *types.SupportTicket
	a.Store.View(func() { t = a.Store.Tickets[r.PathValue("id")] })
	if t == nil {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	claims := claimsFrom(r)
	author, authorID := "operator", ""
	if claims != nil {
		authorID = claims.Sub
		a.Store.View(func() {
			if u := a.Store.Users[claims.Sub]; u != nil {
				author = u.FullName
				if author == "" {
					author = u.Email
				}
			}
		})
	}
	a.Store.Mutate(func() {
		if body := strings.TrimSpace(req.Body); body != "" {
			t.Messages = append(t.Messages, types.TicketMessage{
				ID: NewID(), AuthorID: authorID, Author: author, Body: body, CreatedAt: time.Now(),
			})
		}
		switch types.TicketStatus(req.Status) {
		case types.TicketOpen, types.TicketPending, types.TicketResolved, types.TicketClosed:
			t.Status = types.TicketStatus(req.Status)
		}
		t.UpdatedAt = time.Now()
		t.Forwarded = false // re-forward with the new content
	})
	go a.forwardTicket(t)
	writeJSON(w, t)
}
