package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestResolutionRetriesSameActionAfterRestart(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.Store.Mutate(func() {
		a.Store.Tickets["ticket"] = &types.SupportTicket{ID: "ticket", PollSecret: "proof", Status: types.TicketOpen, ResolutionPending: true, Resolution: &types.TicketResolution{ID: "action", Revision: 7}}
	})
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			ActionID string `json:"actionId"`
			Expected int64  `json:"expectedRevision"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.ActionID != "action" || req.Expected != 7 {
			t.Error("resolution identity changed")
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "actionId": "action", "applied": true})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.resolveTicket("ticket")
	if !a.Store.Tickets["ticket"].ResolutionPending {
		t.Fatal("lost acknowledgement cleared action")
	}
	a.Store.Close()
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	a.resolveTicket("ticket")
	ticket := restored.Tickets["ticket"]
	if ticket.ResolutionPending || ticket.Resolution != nil || ticket.Status != types.TicketResolved {
		t.Fatal("resolution did not finish")
	}
}

func TestResolutionConflictAndConcurrentReply(t *testing.T) {
	for _, mode := range []string{"conflict", "reply"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			ticket := &types.SupportTicket{ID: "ticket", PollSecret: "proof", Status: types.TicketOpen, ResolutionPending: true, Resolution: &types.TicketResolution{ID: "action", Revision: 7}}
			a.Store.Mutate(func() { a.Store.Tickets[ticket.ID] = ticket })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "reply" {
					a.Store.Mutate(func() {
						ticket.Resolution = nil
						ticket.ResolutionPending = false
						ticket.Status = types.TicketOpen
						ticket.ForwardPending = true
					})
				}
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "actionId": "action", "applied": mode == "reply"})
			}))
			defer srv.Close()
			t.Setenv("RACKOPS_CRM_URL", srv.URL)
			a.resolveTicket("ticket")
			if ticket.Status != types.TicketOpen || ticket.ResolutionPending {
				t.Fatal("stale resolution changed local status")
			}
			if mode == "conflict" && ticket.DeliveryError == "" {
				t.Fatal("conflict not explained")
			}
		})
	}
}
