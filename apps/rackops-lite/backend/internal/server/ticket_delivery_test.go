package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestTicketDeliveryRecoversLostFirstResponseAfterRestart(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.InstallID = "11111111-2222-3333-4444-555555555555"
	a.Store.Mutate(func() {
		a.Store.Tickets = map[string]*types.SupportTicket{"ticket": {ID: "ticket", Subject: "Fixture", Category: "general", Severity: "normal", Status: types.TicketOpen, Email: "fixture@example.test", ForwardPending: true, CreatedAt: time.Now(), UpdatedAt: time.Now(), Messages: []types.TicketMessage{{ID: "message", Author: "user", Body: "fixture", At: time.Now()}}}}
	})
	var firstSecret string
	calls := 0
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ingest/v1/capabilities" {
			_ = json.NewEncoder(w).Encode(map[string]bool{"ticketClientCredentials": true})
			return
		}
		calls++
		var req struct {
			PollSecret string `json:"pollSecret"`
			Ticket     struct {
				Messages []types.TicketMessage `json:"messages"`
			} `json:"ticket"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.PollSecret) != 64 || len(req.Ticket.Messages) != 1 || req.Ticket.Messages[0].ID != "message" {
			t.Error("invalid or changed ticket identity")
			w.WriteHeader(400)
			return
		}
		// Prove the initial credential was on disk before Admin received it.
		sealed, readErr := os.ReadFile(filepath.Join(a.Store.dataDir, "state.json"))
		plain, decryptErr := decryptState(a.Store.stateKey, sealed)
		if readErr != nil || decryptErr != nil || !strings.Contains(string(plain), req.PollSecret) {
			t.Error("credential not persisted before network delivery")
		}
		if calls == 1 {
			firstSecret = req.PollSecret
			w.WriteHeader(503)
			return
		}
		if req.PollSecret != firstSecret {
			t.Error("retry lost first credential")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "pollSecret": req.PollSecret})
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	a.forwardTicket("ticket")
	if !a.Store.Tickets["ticket"].ForwardPending || a.Store.Tickets["ticket"].PollSecret != firstSecret {
		t.Fatal("uncertain delivery not retained")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	a.forwardTicket("ticket")
	if a.Store.Tickets["ticket"].ForwardPending || calls != 2 {
		t.Fatal("retry did not recover")
	}
	raw, _ := json.Marshal(a.Store.Tickets["ticket"].Sanitized())
	if strings.Contains(string(raw), firstSecret) {
		t.Fatal("ticket API exposes credential")
	}
}

func TestTicketAckCannotClearNewerReplyOrReplaceCredential(t *testing.T) {
	for _, mode := range []string{"newer-reply", "wrong-secret", "not-ok"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			original := time.Now()
			a.Store.Mutate(func() {
				a.Store.Tickets = map[string]*types.SupportTicket{"ticket": {ID: "ticket", PollSecret: strings.Repeat("a", 64), ForwardPending: true, UpdatedAt: original}}
			})
			crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ingest/v1/capabilities" {
					_ = json.NewEncoder(w).Encode(map[string]bool{"ticketClientCredentials": true})
					return
				}
				secret := strings.Repeat("a", 64)
				ok := true
				if mode == "newer-reply" {
					a.Store.Mutate(func() { a.Store.Tickets["ticket"].UpdatedAt = original.Add(time.Second) })
				}
				if mode == "wrong-secret" {
					secret = strings.Repeat("b", 64)
				}
				if mode == "not-ok" {
					ok = false
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "pollSecret": secret})
			}))
			defer crm.Close()
			t.Setenv("RACKOPS_CRM_URL", crm.URL)
			a.forwardTicket("ticket")
			if !a.Store.Tickets["ticket"].ForwardPending || a.Store.Tickets["ticket"].PollSecret != strings.Repeat("a", 64) {
				t.Fatal("stale or invalid acknowledgement changed delivery state")
			}
		})
	}
}

func TestOldAdminCannotOrphanNewTicket(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.Store.Mutate(func() {
		a.Store.Tickets = map[string]*types.SupportTicket{"ticket": {ID: "ticket", ForwardPending: true}}
	})
	posts := 0
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		w.WriteHeader(404)
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	a.forwardTicket("ticket")
	if posts != 0 || !a.Store.Tickets["ticket"].ForwardPending {
		t.Fatal("unsupported Admin received a new ticket")
	}
}
