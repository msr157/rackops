package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// fakeCRM implements just enough of the CRM ingest contract for the relay
// client: mints a pollSecret on first forward, records payloads, serves one
// agent reply.
type fakeCRM struct {
	mu         sync.Mutex
	forwards   []map[string]any
	pollSecret string
	replies    []map[string]any
	status     string
}

func newFakeCRM(t *testing.T) (*fakeCRM, *httptest.Server) {
	t.Helper()
	f := &fakeCRM{pollSecret: strings.Repeat("ab", 20), status: "pending"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ingest/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"ticketClientCredentials": true})
	})
	mux.HandleFunc("POST /ingest/v1/tickets", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.forwards = append(f.forwards, body)
		if len(f.forwards) == 1 {
			if supplied, _ := body["pollSecret"].(string); supplied != "" {
				f.pollSecret = supplied
			}
		}
		// After the first forward the client must present the secret.
		if len(f.forwards) > 1 {
			if got, _ := body["pollSecret"].(string); got != f.pollSecret {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "pollSecret": f.pollSecret})
	})
	mux.HandleFunc("GET /ingest/v1/tickets/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-Poll-Secret") != f.pollSecret {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": f.status, "replies": f.replies})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeCRM) forwarded() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.forwards...)
}
func (f *fakeCRM) secret() string { f.mu.Lock(); defer f.mu.Unlock(); return f.pollSecret }

func TestTicketForwardAndReplyPoll(t *testing.T) {
	crm, srv := newFakeCRM(t)
	t.Setenv("RACKOPS_CRM_URL", srv.URL)

	a, _ := testAPI(t, types.RoleOwner)
	a.InstallID = "11111111-2222-3333-4444-555555555555"
	jwt := loginToken(t, a, "owner@test.local")

	// Create a ticket via the API; the async forward should hit the fake CRM.
	rec := do(a, http.MethodPost, "/api/support/tickets",
		`{"subject":"Edge won't start","category":"networking","severity":"high","body":"Traefik loops on port 80"}`,
		map[string]string{"Authorization": "Bearer " + jwt})
	if rec.Code != http.StatusOK {
		t.Fatalf("create ticket: %d %s", rec.Code, rec.Body)
	}
	var ticket types.SupportTicket
	_ = json.Unmarshal(rec.Body.Bytes(), &ticket)
	if ticket.PollSecret != "" {
		t.Fatal("API response must never contain the poll secret")
	}

	waitFor(t, func() bool { return len(crm.forwarded()) >= 1 })
	fw := crm.forwarded()[0]
	if fw["installId"] != a.InstallID {
		t.Fatalf("forward installId = %v", fw["installId"])
	}
	if _, ok := fw["usage"].(map[string]any); !ok {
		t.Fatal("forward must carry the usage snapshot")
	}
	// The secret must now be persisted server-side.
	waitFor(t, func() bool {
		var s string
		pending := true
		a.Store.View(func() { s = a.Store.Tickets[ticket.ID].PollSecret; pending = a.Store.Tickets[ticket.ID].ForwardPending })
		return s == crm.secret() && !pending
	})

	// A support agent answers; the poll loop logic should merge it in.
	crm.mu.Lock()
	crm.replies = append(crm.replies, map[string]any{
		"id": "agent-msg-1", "name": "RackOps Support",
		"body": "Port 80 is taken by nginx — stop it or move the edge.", "at": time.Now().Format(time.RFC3339Nano),
	})
	crm.mu.Unlock()
	a.pollTicketReplies(ticket.ID)

	rec = do(a, http.MethodGet, "/api/support/tickets/"+ticket.ID, "",
		map[string]string{"Authorization": "Bearer " + jwt})
	var got types.SupportTicket
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Messages) != 2 || got.Messages[1].Author != "agent" {
		t.Fatalf("agent reply not merged: %d messages", len(got.Messages))
	}
	if got.Status != types.TicketPending {
		t.Fatalf("status = %s, want pending (CRM said so)", got.Status)
	}
	// Idempotent: polling again must not duplicate the reply.
	a.pollTicketReplies(ticket.ID)
	a.Store.View(func() {
		if n := len(a.Store.Tickets[ticket.ID].Messages); n != 2 {
			t.Fatalf("duplicate merge: %d messages", n)
		}
	})

	// Customer reply re-forwards (with the secret) and reopens.
	rec = do(a, http.MethodPost, "/api/support/tickets/"+ticket.ID+"/reply",
		`{"body":"That fixed it, thanks!"}`, map[string]string{"Authorization": "Bearer " + jwt})
	if rec.Code != http.StatusOK {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}
	waitFor(t, func() bool { return len(crm.forwarded()) >= 2 })
	// Only author=user messages go up.
	forwards := crm.forwarded()
	last := forwards[len(forwards)-1]
	tk, _ := last["ticket"].(map[string]any)
	msgs, _ := tk["messages"].([]any)
	for _, m := range msgs {
		if mm, _ := m.(map[string]any); mm["author"] != "user" {
			t.Fatalf("non-user message forwarded: %v", mm)
		}
	}
}

func TestTicketScoping(t *testing.T) {
	_, srv := newFakeCRM(t)
	t.Setenv("RACKOPS_CRM_URL", srv.URL)

	a, _ := testAPI(t, types.RoleOwner, types.RoleMember)
	ownerJWT := loginToken(t, a, "owner@test.local")
	memberJWT := loginToken(t, a, "member@test.local")

	rec := do(a, http.MethodPost, "/api/support/tickets",
		`{"subject":"member's problem","body":"details"}`,
		map[string]string{"Authorization": "Bearer " + memberJWT})
	var ticket types.SupportTicket
	_ = json.Unmarshal(rec.Body.Bytes(), &ticket)
	// The API starts delivery asynchronously. Drain it before Store/TempDir
	// cleanup so persistence cannot recreate files while the directory is removed.
	t.Cleanup(func() {
		waitFor(t, func() bool {
			done := false
			a.Store.View(func() {
				if current := a.Store.Tickets[ticket.ID]; current != nil {
					done = !current.ForwardPending
				}
			})
			return done
		})
		if value, ok := forwardMu.Load(ticket.ID); ok {
			mu := value.(*sync.Mutex)
			mu.Lock()
			mu.Unlock()
		}
	})

	// A second member sees nothing; the creator and admins see it.
	third := &types.User{ID: NewID(), Email: "other@test.local", Role: types.RoleMember,
		CreatedAt: time.Now(), PasswordHash: a.mustHash(t, "correct-horse")}
	a.Store.Mutate(func() { a.Store.Users[third.ID] = third })
	otherJWT := loginToken(t, a, "other@test.local")

	for _, tt := range []struct {
		jwt     string
		wantLen int
	}{
		{memberJWT, 1}, {ownerJWT, 1}, {otherJWT, 0},
	} {
		rec := do(a, http.MethodGet, "/api/support/tickets", "", map[string]string{"Authorization": "Bearer " + tt.jwt})
		var list []types.SupportTicket
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
		if len(list) != tt.wantLen {
			t.Fatalf("ticket list length = %d, want %d", len(list), tt.wantLen)
		}
	}
	// Direct access by the outsider: 404, not 403 (no existence oracle).
	if rec := do(a, http.MethodGet, "/api/support/tickets/"+ticket.ID, "",
		map[string]string{"Authorization": "Bearer " + otherJWT}); rec.Code != http.StatusNotFound {
		t.Fatalf("outsider get ticket: %d, want 404", rec.Code)
	}
}

func TestTicketValidation(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	jwt := loginToken(t, a, "owner@test.local")
	for _, tt := range []struct {
		name, body string
	}{
		{"no subject", `{"body":"x"}`},
		{"no body", `{"subject":"x"}`},
		{"bad category", `{"subject":"x","body":"y","category":"gossip"}`},
		{"bad severity", `{"subject":"x","body":"y","severity":"apocalyptic"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(a, http.MethodPost, "/api/support/tickets", tt.body,
				map[string]string{"Authorization": "Bearer " + jwt}); rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
		})
	}
}

// waitFor polls a condition for up to 2s — the forward runs on a goroutine.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
