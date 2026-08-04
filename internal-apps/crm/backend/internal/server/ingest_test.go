package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ---- test harness ----

// fakeMailer records sends and can simulate an unconfigured relay.
type fakeMailer struct {
	mu         sync.Mutex
	configured bool
	fail       bool
	sent       []fakeMail
}

type fakeMail struct{ To, Subject, Text, HTML string }

func (m *fakeMailer) Configured() bool { return m.configured }

func (m *fakeMailer) Send(to, subject, text, html string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return fmt.Errorf("simulated smtp failure")
	}
	m.sent = append(m.sent, fakeMail{to, subject, text, html})
	return nil
}

func (m *fakeMailer) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

type testEnv struct {
	api    *API
	srv    *httptest.Server
	mailer *fakeMailer
}

const (
	testAdminEmail    = "ops@rackops.in"
	testAdminPassword = "hunter2hunter2"
	testInstallID     = "11111111-2222-3333-4444-555555555555"
)

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	admin, err := NewAdminAuth(testAdminEmail, testAdminPassword)
	if err != nil {
		t.Fatalf("admin auth: %v", err)
	}
	mailer := &fakeMailer{configured: true}
	api := NewAPI(store, mailer, admin, nil)
	// Generous limits so ordinary tests never trip them; the rate-limit
	// tests install strict limiters explicitly.
	api.ingestLimiter = newRateLimiter(600000, 100000)
	api.resetIPLimiter = newRateLimiter(600000, 100000)
	api.resetEmailLimiter = newRateLimiter(600000, 100000)
	api.loginLimiter = newRateLimiter(600000, 100000)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &testEnv{api: api, srv: srv, mailer: mailer}
}

// doJSON posts/gets JSON and decodes the response into out (when non-nil).
func (e *testEnv) doJSON(t *testing.T, method, path, token string, body any, out any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, e.srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode response of %s %s: %v", method, path, err)
		}
	}
	return resp
}

func (e *testEnv) login(t *testing.T) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	resp := e.doJSON(t, "POST", "/api/auth/login", "",
		map[string]string{"email": testAdminEmail, "password": testAdminPassword}, &out)
	if resp.StatusCode != 200 {
		t.Fatalf("login: status %d", resp.StatusCode)
	}
	if out.Token == "" {
		t.Fatal("login: empty token")
	}
	return out.Token
}

// forwardTicket sends a standard ticket upsert; msgIDs become user messages.
func (e *testEnv) forwardTicket(t *testing.T, ticketID, pollSecret string, msgIDs ...string) (*http.Response, string) {
	t.Helper()
	msgs := []map[string]any{}
	for i, id := range msgIDs {
		msgs = append(msgs, map[string]any{
			"id": id, "author": "user", "name": "Casey Customer",
			"body": "message " + id,
			"at":   time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339),
		})
	}
	body := map[string]any{
		"installId":   testInstallID,
		"installName": "acme-lab",
		"version":     "0.4.0",
		"pollSecret":  pollSecret,
		"ticket": map[string]any{
			"id": ticketID, "subject": "Deploys hang", "category": "deploys",
			"severity": "high", "status": "open", "email": "casey@acme.test",
			"createdAt": time.Now().UTC().Format(time.RFC3339),
			"messages":  msgs,
		},
		"usage": map[string]any{
			"version": "0.4.0", "os": "linux", "arch": "amd64",
			"nodes": 3, "nodesOnline": 3, "services": 7,
			"servicesByKind": map[string]int{"app": 4, "database": 2, "compose": 1},
			"adopted":        1, "edgeEnabled": true, "meshEnabled": false, "ha": false,
			"orgs": 1, "users": 4, "domains": 2,
			"featuresUsed": []string{"deploys", "domains", "edge"},
		},
	}
	var out struct {
		OK         bool   `json:"ok"`
		PollSecret string `json:"pollSecret"`
	}
	resp := e.doJSON(t, "POST", "/ingest/v1/tickets", "", body, &out)
	return resp, out.PollSecret
}

// ---- ticket upsert + poll-secret auth + message merge ----

func TestTicketUpsert(t *testing.T) {
	env := newTestEnv(t)

	// First forward creates the ticket and mints a 40-hex poll secret.
	resp, secret := env.forwardTicket(t, "tkt-1", "", "m1")
	if resp.StatusCode != 200 {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	if len(secret) != 40 {
		t.Fatalf("poll secret: want 40 hex chars, got %q", secret)
	}

	tests := []struct {
		name       string
		secret     string
		msgIDs     []string
		wantStatus int
	}{
		{"wrong secret rejected", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", []string{"m2"}, 403},
		{"empty secret on existing ticket rejected", "", []string{"m2"}, 403},
		{"correct secret merges new message", secret, []string{"m1", "m2"}, 200},
		{"duplicate ids are idempotent", secret, []string{"m1", "m2"}, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := env.forwardTicket(t, "tkt-1", tc.secret, tc.msgIDs...)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}

	// The merged thread holds exactly m1 + m2 once each.
	tk, err := env.api.Store.GetTicket("tkt-1", testInstallID)
	if err != nil || tk == nil {
		t.Fatalf("get ticket: %v", err)
	}
	if len(tk.Messages) != 2 {
		t.Fatalf("messages = %d, want 2 (merge by id)", len(tk.Messages))
	}
	if tk.Status != "open" {
		t.Fatalf("status = %q, want open (customer message pending)", tk.Status)
	}
	// The install record was stored/refreshed with the usage snapshot.
	in, err := env.api.Store.GetInstall(testInstallID)
	if err != nil || in == nil {
		t.Fatalf("get install: %v", err)
	}
	if in.Name != "acme-lab" || in.Usage == nil || in.Usage.Nodes != 3 {
		t.Fatalf("install not refreshed from forward: %+v", in)
	}
}

func TestTicketUpsertValidation(t *testing.T) {
	env := newTestEnv(t)
	base := func() map[string]any {
		return map[string]any{
			"installId":  testInstallID,
			"pollSecret": "",
			"ticket": map[string]any{
				"id": "tkt-v", "subject": "s", "category": "general",
				"severity": "low", "status": "open", "email": "a@b.c",
			},
		}
	}
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"bad installId", func(m map[string]any) { m["installId"] = "not-a-uuid" }},
		{"missing ticket", func(m map[string]any) { delete(m, "ticket") }},
		{"unknown category", func(m map[string]any) { m["ticket"].(map[string]any)["category"] = "spaceships" }},
		{"unknown severity", func(m map[string]any) { m["ticket"].(map[string]any)["severity"] = "catastrophic" }},
		{"unknown status", func(m map[string]any) { m["ticket"].(map[string]any)["status"] = "escalated" }},
		{"bad email", func(m map[string]any) { m["ticket"].(map[string]any)["email"] = "nope" }},
		{"empty subject", func(m map[string]any) { m["ticket"].(map[string]any)["subject"] = " " }},
		{"bad ticket id", func(m map[string]any) { m["ticket"].(map[string]any)["id"] = "has spaces!" }},
		{"non-user author", func(m map[string]any) {
			m["ticket"].(map[string]any)["messages"] = []map[string]any{
				{"id": "x1", "author": "agent", "name": "Mallory", "body": "spoofed"},
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := base()
			tc.mutate(body)
			resp := env.doJSON(t, "POST", "/ingest/v1/tickets", "", body, nil)
			if resp.StatusCode != 400 {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// ---- replies polling: since filtering + note exclusion ----

func TestRepliesSinceAndNoteExclusion(t *testing.T) {
	env := newTestEnv(t)
	resp, secret := env.forwardTicket(t, "tkt-2", "", "m1")
	if resp.StatusCode != 200 {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	token := env.login(t)

	// Agent posts a reply, then an internal note, then a second reply.
	if r := env.doJSON(t, "POST", "/api/tickets/tkt-2/reply", token, map[string]string{"body": "first reply"}, nil); r.StatusCode != 200 {
		t.Fatalf("reply 1: %d", r.StatusCode)
	}
	cut := time.Now()
	time.Sleep(20 * time.Millisecond) // strictly-after boundary
	if r := env.doJSON(t, "POST", "/api/tickets/tkt-2/note", token, map[string]string{"body": "internal only"}, nil); r.StatusCode != 200 {
		t.Fatalf("note: %d", r.StatusCode)
	}
	if r := env.doJSON(t, "POST", "/api/tickets/tkt-2/reply", token, map[string]string{"body": "second reply"}, nil); r.StatusCode != 200 {
		t.Fatalf("reply 2: %d", r.StatusCode)
	}

	poll := func(since string, pollSecret string) (*http.Response, struct {
		Status  string `json:"status"`
		Replies []struct {
			ID   string    `json:"id"`
			Name string    `json:"name"`
			Body string    `json:"body"`
			At   time.Time `json:"at"`
		} `json:"replies"`
	}) {
		var out struct {
			Status  string `json:"status"`
			Replies []struct {
				ID   string    `json:"id"`
				Name string    `json:"name"`
				Body string    `json:"body"`
				At   time.Time `json:"at"`
			} `json:"replies"`
		}
		url := "/ingest/v1/tickets/tkt-2/replies?installId=" + testInstallID
		if since != "" {
			url += "&since=" + since
		}
		req, _ := http.NewRequest("GET", env.srv.URL+url, nil)
		req.Header.Set("X-Poll-Secret", pollSecret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode == 200 {
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode poll: %v", err)
			}
		}
		return resp, out
	}

	t.Run("bad secret is 403", func(t *testing.T) {
		resp, _ := poll("", "0000000000000000000000000000000000000000")
		if resp.StatusCode != 403 {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
	t.Run("unknown ticket is 403", func(t *testing.T) {
		req, _ := http.NewRequest("GET", env.srv.URL+"/ingest/v1/tickets/nope/replies?installId="+testInstallID, nil)
		req.Header.Set("X-Poll-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
	t.Run("all agent replies, notes excluded", func(t *testing.T) {
		resp, out := poll("", secret)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if len(out.Replies) != 2 {
			t.Fatalf("replies = %d, want 2 (note must be excluded)", len(out.Replies))
		}
		for _, r := range out.Replies {
			if r.Body == "internal only" {
				t.Fatal("internal note leaked to instance poll")
			}
			if r.Name != "RackOps Support" {
				t.Fatalf("reply name = %q, want RackOps Support", r.Name)
			}
		}
		if out.Status != "pending" {
			t.Fatalf("status = %q, want pending after agent reply", out.Status)
		}
	})
	t.Run("since filters strictly after", func(t *testing.T) {
		resp, out := poll(cut.UTC().Format(time.RFC3339Nano), secret)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if len(out.Replies) != 1 || out.Replies[0].Body != "second reply" {
			t.Fatalf("since filter: got %+v, want only the second reply", out.Replies)
		}
	})
	t.Run("bad since is 400", func(t *testing.T) {
		resp, _ := poll("yesterday", secret)
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
}

// ---- ticket status semantics ----

func TestTicketStatusSemantics(t *testing.T) {
	env := newTestEnv(t)
	_, secret := env.forwardTicket(t, "tkt-3", "", "m1")
	token := env.login(t)

	// Agent reply → pending.
	var tk Ticket
	env.doJSON(t, "POST", "/api/tickets/tkt-3/reply", token, map[string]string{"body": "on it"}, &tk)
	if tk.Status != "pending" {
		t.Fatalf("after agent reply: status = %q, want pending", tk.Status)
	}
	// New customer message → open.
	env.forwardTicket(t, "tkt-3", secret, "m1", "m2")
	got, _ := env.api.Store.GetTicket("tkt-3", testInstallID)
	if got.Status != "open" {
		t.Fatalf("after customer message: status = %q, want open", got.Status)
	}
	// Manual resolve.
	env.doJSON(t, "POST", "/api/tickets/tkt-3/status", token, map[string]string{"status": "resolved"}, &tk)
	if tk.Status != "resolved" {
		t.Fatalf("after resolve: status = %q, want resolved", tk.Status)
	}
	// Invalid status rejected.
	resp := env.doJSON(t, "POST", "/api/tickets/tkt-3/status", token, map[string]string{"status": "closed"}, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("invalid status: %d, want 400", resp.StatusCode)
	}
	// A forward WITHOUT new customer messages must not reopen a resolved ticket.
	env.forwardTicket(t, "tkt-3", secret, "m1", "m2")
	got, _ = env.api.Store.GetTicket("tkt-3", testInstallID)
	if got.Status != "resolved" {
		t.Fatalf("idempotent forward reopened ticket: status = %q", got.Status)
	}
}
