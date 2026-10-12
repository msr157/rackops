package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestReplyChunksResumeAndVerifyBeforeCursorAdvance(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.Store.Mutate(func() { a.Store.Tickets["ticket"] = &types.SupportTicket{ID: "ticket", PollSecret: "proof"} })
	raw, _ := json.Marshal(types.TicketMessage{ID: "historical", Author: "agent", Body: strings.Repeat("界", 250000)})
	sum := sha256.Sum256(raw)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Poll-Secret") != "proof" {
			t.Error("missing credential")
		}
		if r.URL.Query().Get("chunk") != "1" {
			w.WriteHeader(422)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		end := offset + (256 << 10)
		if end > len(raw) {
			end = len(raw)
		}
		json.NewEncoder(w).Encode(map[string]any{"chunk": map[string]any{"sequence": 9, "offset": offset, "total": len(raw), "sha256": hex.EncodeToString(sum[:]), "data": raw[offset:end]}})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.pollTicketReplies("ticket")
	ticket := a.Store.Tickets["ticket"]
	if ticket.ReplyAssembly == nil || ticket.ReplyCursor != 0 || len(ticket.Messages) != 0 || ticket.CRMRevision != nil {
		t.Fatal("partial message exposed")
	}
	if ticket.Sanitized().ReplyAssembly != nil {
		t.Fatal("partial state exposed in API")
	}
	a.Store.Close()
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	for i := 0; i < 5 && restored.Tickets["ticket"].ReplyCursor == 0; i++ {
		a.pollTicketReplies("ticket")
	}
	ticket = restored.Tickets["ticket"]
	if ticket.ReplyCursor != 9 || ticket.ReplyAssembly != nil || len(ticket.Messages) != 1 || ticket.Messages[0].Body != strings.Repeat("界", 250000) {
		t.Fatal("chunk recovery lost message bytes")
	}
}

func TestCorruptReplyChunkNeverAdvancesCursor(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.Store.Mutate(func() { a.Store.Tickets["ticket"] = &types.SupportTicket{ID: "ticket", PollSecret: "proof"} })
	raw := []byte(`{"id":"reply","author":"agent","body":"test"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"chunk": map[string]any{"sequence": 9, "offset": 0, "total": len(raw), "sha256": strings.Repeat("0", 64), "data": raw}})
	}))
	defer srv.Close()
	t.Setenv("RACKOPS_CRM_URL", srv.URL)
	a.pollReplyChunk("ticket", "proof", 0)
	ticket := a.Store.Tickets["ticket"]
	if ticket.ReplyCursor != 0 || len(ticket.Messages) != 0 || ticket.ReplyAssembly != nil || ticket.ReplySyncError == "" {
		t.Fatal("corrupt message accepted")
	}
}
