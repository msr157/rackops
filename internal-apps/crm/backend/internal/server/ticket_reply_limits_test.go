package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAgentMessageLimits(t *testing.T) {
	e := newTestEnv(t)
	response, _ := e.forwardTicket(t, "limits", strings.Repeat("a", 64), "initial")
	if response.StatusCode != 200 {
		t.Fatal("fixture forward failed")
	}
	token := e.login(t)
	for _, kind := range []string{"reply", "note"} {
		path := "/api/tickets/limits/" + kind + "?installId=" + testInstallID
		for _, size := range []int{16 << 10, (16 << 10) + 1, 200 << 10} {
			r := e.doJSON(t, "POST", path, token, map[string]string{"body": strings.Repeat("x", size)}, nil)
			expected := 400
			if size == 16<<10 {
				expected = 200
			}
			if size == 200<<10 {
				expected = 413
			}
			if r.StatusCode != expected {
				t.Fatalf("%s size %d: got %d want %d", kind, size, r.StatusCode, expected)
			}
		}
	}
	ticket, err := e.api.Store.GetTicket("limits", testInstallID)
	if err != nil || len(ticket.Messages) != 3 {
		t.Fatal("invalid message was saved", err)
	}
}

func TestHistoricalReplyPagesRespectWireLimit(t *testing.T) {
	e := newTestEnv(t)
	secret := strings.Repeat("a", 64)
	e.forwardTicket(t, "history", secret, "initial")
	for i := 0; i < 3; i++ {
		if err := e.api.Store.AddMessage("history", testInstallID, TicketMessage{ID: fmt.Sprint(i), Author: "agent", Body: strings.Repeat("<", 512<<10), At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	cursor := int64(0)
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/ingest/v1/tickets/history/replies?installId=%s&cursor=%d", e.srv.URL, testInstallID, cursor), nil)
		req.Header.Set("X-Poll-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || len(raw) >= 8<<20 {
			t.Fatal("oversized or failed page", err)
		}
		var page struct {
			Next     int64           `json:"nextCursor"`
			Revision *int64          `json:"revision"`
			Replies  []TicketMessage `json:"replies"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Replies) != 1 || page.Replies[0].ID != fmt.Sprint(i) || page.Next <= cursor {
			t.Fatal("reply was skipped")
		}
		if (page.Revision != nil) != (i == 2) {
			t.Fatal("partial history allowed resolution")
		}
		cursor = page.Next
	}
	if err := e.api.Store.AddMessage("history", testInstallID, TicketMessage{ID: "huge", Author: "agent", Body: strings.Repeat("<", 2<<20), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, _, next, err := e.api.Store.AgentRepliesPage("history", testInstallID, cursor)
	if err != errReplyTooLarge || next != cursor {
		t.Fatal("huge reply silently skipped")
	}
	ticket, err := e.api.Store.GetTicket("history", testInstallID)
	if err != nil || len(ticket.Messages[4].Body) != 2<<20 {
		t.Fatal("historical data truncated")
	}
}
