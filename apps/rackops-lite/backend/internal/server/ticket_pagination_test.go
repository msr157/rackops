package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestReplyCursorDeduplicatesAndPersists(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.Store.Mutate(func() {
		a.Store.Tickets["thread"] = &types.SupportTicket{ID: "thread", PollSecret: "proof", Status: types.TicketResolved}
	})
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		want := "0"
		if calls > 1 {
			want = "7"
		}
		if r.URL.Query().Get("cursor") != want || r.URL.Query().Has("since") {
			t.Error("wrong cursor")
		}
		reply := map[string]any{"id": "reply", "body": "late reply", "at": time.Now().Add(-time.Hour)}
		json.NewEncoder(w).Encode(map[string]any{"status": "pending", "nextCursor": 7, "replies": []any{reply, reply}})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.pollTicketReplies("thread")
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Tickets["thread"].ReplyCursor != 7 || len(restored.Tickets["thread"].Messages) != 1 {
		t.Fatal("reply and cursor were not persisted together")
	}
	a.pollTicketReplies("thread")
	a.Store.View(func() {
		ticket := a.Store.Tickets["thread"]
		if ticket.ReplyCursor != 7 || len(ticket.Messages) != 1 || ticket.Status != types.TicketPending {
			t.Errorf("unexpected ticket: %+v", ticket.Sanitized())
		}
		if ticket.Sanitized().ReplyCursor != 0 {
			t.Error("cursor leaked")
		}
	})
}

func TestOversizedReplyReportsSyncErrorWithoutAdvancing(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	version := int64(4)
	a.Store.Mutate(func() {
		a.Store.Tickets["thread"] = &types.SupportTicket{ID: "thread", PollSecret: "proof", ReplyCursor: 7, CRMRevision: &version}
	})
	failing := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			w.WriteHeader(422)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "open", "nextCursor": 7, "revision": 5, "replies": []any{}})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.pollTicketReplies("thread")
	ticket := a.Store.Tickets["thread"]
	if ticket.ReplyCursor != 7 || ticket.ReplySyncError == "" || ticket.CRMRevision != nil {
		t.Fatal("error not retained safely")
	}
	failing = false
	a.pollTicketReplies("thread")
	if ticket.ReplySyncError != "" || ticket.CRMRevision == nil {
		t.Fatal("sync did not recover")
	}
}
