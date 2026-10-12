package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestTicketBatchesResumeAfterLostAckAndRestart(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	ticket := &types.SupportTicket{ID: "batch", PollSecret: strings.Repeat("a", 40), ForwardPending: true, UpdatedAt: time.Now()}
	for i := 0; i < 20; i++ {
		ticket.Messages = append(ticket.Messages, types.TicketMessage{ID: fmt.Sprint(i), Author: "user", Body: strings.Repeat("<", 16<<10)})
	}
	a.Store.Mutate(func() { a.Store.Tickets[ticket.ID] = ticket })
	received := map[string]int{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 192<<10 {
			t.Error("batch exceeds wire size limit")
		}
		var body struct {
			Ticket struct {
				Messages []types.TicketMessage `json:"messages"`
			} `json:"ticket"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Error(err)
		}
		if len(body.Ticket.Messages) == 0 {
			t.Error("empty delivery before completion")
		}
		for _, m := range body.Ticket.Messages {
			received[m.ID]++
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "pollSecret": ticket.PollSecret})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.forwardTicket("batch")
	if len(ticket.DeliveredMessages) != 0 {
		t.Fatal("lost acknowledgement advanced delivery")
	}
	a.forwardTicket("batch")
	if len(ticket.DeliveredMessages) == 0 || !ticket.ForwardPending {
		t.Fatal("partial delivery incorrectly completed")
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
	for i := 0; i < 25 && restored.Tickets["batch"].ForwardPending; i++ {
		a.forwardTicket("batch")
	}
	current := restored.Tickets["batch"]
	if current.ForwardPending || len(current.DeliveredMessages) != 20 || len(current.Messages) != 20 || len(received) != 20 {
		t.Fatal("batch delivery lost data or failed to finish")
	}
	if received["0"] != 2 || received["19"] != 1 {
		t.Fatal("acknowledged messages were resent after restart")
	}
}

func TestOversizedLegacyMessageRemainsVisibleAndQueued(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	ticket := &types.SupportTicket{ID: "large", Messages: []types.TicketMessage{{ID: "old", Author: "user", Body: strings.Repeat("<", 200<<10)}}}
	a.Store.Mutate(func() { a.Store.Tickets[ticket.ID] = ticket })
	a.forwardTicket(ticket.ID)
	if !ticket.ForwardPending || ticket.DeliveryError == "" || len(ticket.Messages[0].Body) != 200<<10 || len(ticket.DeliveredMessages) != 0 {
		t.Fatal("oversized message was lost or falsely acknowledged")
	}
}

func TestTicketRejectsUndeliverableInput(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	token := loginToken(t, a, "owner@test.local")
	for _, body := range []map[string]string{
		{"subject": "test", "body": strings.Repeat("x", (16<<10)+1)},
		{"subject": "test", "body": "test", "email": "invalid"},
	} {
		raw, _ := json.Marshal(body)
		response := do(a, http.MethodPost, "/api/support/tickets", string(raw), map[string]string{"Authorization": "Bearer " + token})
		if response.Code != 400 {
			t.Fatal("invalid input accepted", response.Code)
		}
	}
	if len(a.Store.Tickets) != 0 {
		t.Fatal("invalid request queued a ticket")
	}
}
