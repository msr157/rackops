package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---- reset-email: relay, 503 when unconfigured, rate limiting ----

func resetBody(email string) map[string]any {
	return map[string]any{
		"installId":    testInstallID,
		"instanceName": "acme-lab",
		"email":        email,
		"link":         "https://rackops.acme.test/reset?token=abc123",
		"version":      "0.4.0",
	}
}

func TestResetEmailSend(t *testing.T) {
	env := newTestEnv(t)

	resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("casey@acme.test"), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if env.mailer.sentCount() != 1 {
		t.Fatalf("sent = %d, want 1", env.mailer.sentCount())
	}
	m := env.mailer.sent[0]
	if m.To != "casey@acme.test" {
		t.Fatalf("to = %q", m.To)
	}
	if m.Subject != "Reset your RackOps password" {
		t.Fatalf("subject = %q", m.Subject)
	}
	for _, want := range []string{"acme-lab", "https://rackops.acme.test/reset?token=abc123", "1 hour", "ignore"} {
		if !strings.Contains(m.Text, want) {
			t.Fatalf("text body missing %q:\n%s", want, m.Text)
		}
	}
	if !strings.Contains(m.HTML, "Reset password") {
		t.Fatal("html body missing button")
	}
	// Send attempt logged.
	logs := resetLogRows(t, env)
	if len(logs) != 1 || logs[0].Status != "sent" {
		t.Fatalf("reset_emails log = %+v, want one 'sent' row", logs)
	}
}

func TestResetEmailValidation(t *testing.T) {
	env := newTestEnv(t)
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"bad installId", func(m map[string]any) { m["installId"] = "abc" }},
		{"bad email", func(m map[string]any) { m["email"] = "not-an-email" }},
		{"non-http link", func(m map[string]any) { m["link"] = "javascript:alert(1)" }},
		{"missing link", func(m map[string]any) { m["link"] = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := resetBody("casey@acme.test")
			tc.mutate(body)
			resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", body, nil)
			if resp.StatusCode != 400 {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
	if env.mailer.sentCount() != 0 {
		t.Fatalf("invalid requests must not send email (sent %d)", env.mailer.sentCount())
	}
}

func TestResetEmailUnconfiguredSMTP(t *testing.T) {
	env := newTestEnv(t)
	env.mailer.configured = false

	resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("casey@acme.test"), nil)
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Error != "email relay not configured" {
		t.Fatalf("error = %q, want \"email relay not configured\"", out.Error)
	}
	logs := resetLogRows(t, env)
	if len(logs) != 1 || logs[0].Status != "unconfigured" {
		t.Fatalf("log = %+v, want one 'unconfigured' row", logs)
	}
}

func TestResetEmailRateLimit(t *testing.T) {
	t.Run("per email address", func(t *testing.T) {
		env := newTestEnv(t)
		env.api.resetEmailLimiter = newRateLimiter(5.0/60.0, 5) // production limit

		for i := range 5 {
			resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("victim@acme.test"), nil)
			if resp.StatusCode != 200 {
				t.Fatalf("send %d: status %d, want 200", i+1, resp.StatusCode)
			}
		}
		resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("victim@acme.test"), nil)
		if resp.StatusCode != 429 {
			t.Fatalf("6th send: status %d, want 429", resp.StatusCode)
		}
		if env.mailer.sentCount() != 5 {
			t.Fatalf("sent = %d, want 5", env.mailer.sentCount())
		}
		// A different address still goes through — the limit is per email.
		resp = env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("other@acme.test"), nil)
		if resp.StatusCode != 200 {
			t.Fatalf("other address: status %d, want 200", resp.StatusCode)
		}
	})

	t.Run("per IP", func(t *testing.T) {
		env := newTestEnv(t)
		env.api.resetIPLimiter = newRateLimiter(5.0/60.0, 5) // production limit

		for i := range 5 {
			resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "",
				resetBody("user"+string(rune('a'+i))+"@acme.test"), nil)
			if resp.StatusCode != 200 {
				t.Fatalf("send %d: status %d, want 200", i+1, resp.StatusCode)
			}
		}
		// Different address, same IP: still blocked.
		resp := env.doJSON(t, "POST", "/ingest/v1/reset-email", "", resetBody("fresh@acme.test"), nil)
		if resp.StatusCode != 429 {
			t.Fatalf("6th send from same IP: status %d, want 429", resp.StatusCode)
		}
	})
}

func TestIngestRateLimit(t *testing.T) {
	env := newTestEnv(t)
	env.api.ingestLimiter = newRateLimiter(60, 3) // tiny burst to trip fast

	trip := false
	for range 5 {
		resp := env.doJSON(t, "POST", "/ingest/v1/snapshots", "", map[string]any{
			"installId": testInstallID, "installName": "acme-lab", "version": "0.4.0",
			"usage": map[string]any{"version": "0.4.0", "os": "linux", "arch": "amd64"},
		}, nil)
		if resp.StatusCode == 429 {
			trip = true
			break
		}
	}
	if !trip {
		t.Fatal("per-IP ingest rate limit never tripped")
	}
	// Admin API is not throttled by the ingest limiter.
	token := env.login(t)
	resp := env.doJSON(t, "GET", "/api/overview", token, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("admin API throttled by ingest limiter: %d", resp.StatusCode)
	}
}

func resetLogRows(t *testing.T, env *testEnv) []ResetEmail {
	t.Helper()
	rows, err := env.api.Store.db.Query(`SELECT id, install_id, email, instance_name, at, status FROM reset_emails ORDER BY id`)
	if err != nil {
		t.Fatalf("query reset_emails: %v", err)
	}
	defer rows.Close()
	var out []ResetEmail
	for rows.Next() {
		var re ResetEmail
		if err := rows.Scan(&re.ID, &re.InstallID, &re.Email, &re.InstanceName, &re.At, &re.Status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, re)
	}
	return out
}

// ---- snapshot ingest ----

func TestSnapshotIngest(t *testing.T) {
	env := newTestEnv(t)
	post := func(nodes int) *http.Response {
		return env.doJSON(t, "POST", "/ingest/v1/snapshots", "", map[string]any{
			"installId": testInstallID, "installName": "acme-lab", "version": "0.4.1",
			"usage": map[string]any{
				"version": "0.4.1", "os": "linux", "arch": "arm64",
				"nodes": nodes, "nodesOnline": nodes, "services": 2,
				"servicesByKind": map[string]int{"app": 2},
				"featuresUsed":   []string{"deploys"},
			},
		}, nil)
	}
	if resp := post(2); resp.StatusCode != 200 {
		t.Fatalf("snapshot: status %d", resp.StatusCode)
	}
	if resp := post(4); resp.StatusCode != 200 {
		t.Fatalf("snapshot 2: status %d", resp.StatusCode)
	}

	in, err := env.api.Store.GetInstall(testInstallID)
	if err != nil || in == nil {
		t.Fatalf("get install: %v", err)
	}
	if in.Version != "0.4.1" || in.OS != "linux" || in.Arch != "arm64" {
		t.Fatalf("install fields: %+v", in)
	}
	if in.Usage == nil || in.Usage.Nodes != 4 {
		t.Fatalf("last snapshot not refreshed: %+v", in.Usage)
	}
	hist, err := env.api.Store.SnapshotHistory(testInstallID, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history rows = %d, want 2", len(hist))
	}
	if hist[0].Data.Nodes != 4 {
		t.Fatalf("history order: newest first expected, got nodes=%d", hist[0].Data.Nodes)
	}

	t.Run("missing usage rejected", func(t *testing.T) {
		resp := env.doJSON(t, "POST", "/ingest/v1/snapshots", "",
			map[string]any{"installId": testInstallID, "installName": "x", "version": "1"}, nil)
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
}

// ---- admin auth ----

func TestAdminAuth(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name       string
		email      string
		password   string
		wantStatus int
	}{
		{"valid credentials", testAdminEmail, testAdminPassword, 200},
		{"case-insensitive email", "OPS@rackops.IN", testAdminPassword, 200},
		{"wrong password", testAdminEmail, "wrong-password", 401},
		{"unknown email", "mallory@evil.test", testAdminPassword, 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.doJSON(t, "POST", "/api/auth/login", "",
				map[string]string{"email": tc.email, "password": tc.password}, nil)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}

	t.Run("api requires token", func(t *testing.T) {
		for _, path := range []string{"/api/overview", "/api/installs", "/api/tickets"} {
			resp := env.doJSON(t, "GET", path, "", nil, nil)
			if resp.StatusCode != 401 {
				t.Fatalf("GET %s without token: %d, want 401", path, resp.StatusCode)
			}
		}
	})
	t.Run("garbage token rejected", func(t *testing.T) {
		resp := env.doJSON(t, "GET", "/api/overview", "not.a.jwt", nil, nil)
		if resp.StatusCode != 401 {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})
	t.Run("expired token rejected", func(t *testing.T) {
		expired := signJWT(env.api.Store.SessionSecret(), jwtClaims{Sub: testAdminEmail, Exp: time.Now().Add(-time.Hour).Unix()})
		resp := env.doJSON(t, "GET", "/api/overview", expired, nil, nil)
		if resp.StatusCode != 401 {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})
	t.Run("forged signature rejected", func(t *testing.T) {
		forged := signJWT([]byte("attacker-secret-attacker-secret!"), jwtClaims{Sub: testAdminEmail, Exp: time.Now().Add(time.Hour).Unix()})
		resp := env.doJSON(t, "GET", "/api/overview", forged, nil, nil)
		if resp.StatusCode != 401 {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})
	t.Run("valid token grants access", func(t *testing.T) {
		token := env.login(t)
		var ov Overview
		resp := env.doJSON(t, "GET", "/api/overview", token, nil, &ov)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})
	t.Run("ingest stays open without token", func(t *testing.T) {
		resp, _ := env.forwardTicket(t, "tkt-auth", "", "m1")
		if resp.StatusCode != 200 {
			t.Fatalf("ingest blocked by auth middleware: %d", resp.StatusCode)
		}
	})
}

// ---- overview aggregation ----

func TestOverview(t *testing.T) {
	env := newTestEnv(t)
	env.forwardTicket(t, "tkt-ov", "", "m1")
	// A second install on an older version, no mesh, fewer features.
	otherID := "99999999-8888-7777-6666-555555555555"
	env.doJSON(t, "POST", "/ingest/v1/snapshots", "", map[string]any{
		"installId": otherID, "installName": "beta-site", "version": "0.3.0",
		"usage": map[string]any{
			"version": "0.3.0", "os": "linux", "arch": "amd64",
			"nodes": 1, "nodesOnline": 1, "services": 1,
			"servicesByKind": map[string]int{"app": 1},
			"featuresUsed":   []string{"deploys"},
		},
	}, nil)

	token := env.login(t)
	var ov Overview
	resp := env.doJSON(t, "GET", "/api/overview", token, nil, &ov)
	if resp.StatusCode != 200 {
		t.Fatalf("overview: %d", resp.StatusCode)
	}
	if ov.Installs != 2 || ov.ActiveWeek != 2 || ov.OpenTickets != 1 {
		t.Fatalf("counts: %+v", ov)
	}
	if len(ov.VersionDist) != 2 {
		t.Fatalf("versionDist = %+v", ov.VersionDist)
	}
	// "deploys" is used by both installs and must rank first.
	if len(ov.FeatureUsage) == 0 || ov.FeatureUsage[0].Feature != "deploys" || ov.FeatureUsage[0].Count != 2 {
		t.Fatalf("featureUsage = %+v", ov.FeatureUsage)
	}
	if ov.ByKindTotals["app"] != 5 { // 4 from forwardTicket's snapshot + 1
		t.Fatalf("byKindTotals = %+v", ov.ByKindTotals)
	}
	if len(ov.RecentTickets) != 1 {
		t.Fatalf("recentTickets = %d", len(ov.RecentTickets))
	}
}

// ---- installs API ----

func TestInstallDetail(t *testing.T) {
	env := newTestEnv(t)
	env.forwardTicket(t, "tkt-d", "", "m1")
	token := env.login(t)

	var list []Install
	env.doJSON(t, "GET", "/api/installs", token, nil, &list)
	if len(list) != 1 || list[0].ID != testInstallID || list[0].OpenTickets != 1 {
		t.Fatalf("installs list = %+v", list)
	}

	var detail struct {
		Install   Install    `json:"install"`
		Snapshots []Snapshot `json:"snapshots"`
		Tickets   []Ticket   `json:"tickets"`
	}
	resp := env.doJSON(t, "GET", "/api/installs/"+testInstallID, token, nil, &detail)
	if resp.StatusCode != 200 {
		t.Fatalf("detail: %d", resp.StatusCode)
	}
	if detail.Install.Name != "acme-lab" || len(detail.Snapshots) != 1 || len(detail.Tickets) != 1 {
		t.Fatalf("detail = %+v", detail)
	}

	resp = env.doJSON(t, "GET", "/api/installs/00000000-0000-0000-0000-000000000000", token, nil, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown install: %d, want 404", resp.StatusCode)
	}
}

// ---- oversized body cap ----

func TestBodySizeCap(t *testing.T) {
	env := newTestEnv(t)
	huge := strings.Repeat("x", maxBodyBytes+1024)
	resp := env.doJSON(t, "POST", "/ingest/v1/snapshots", "", map[string]any{
		"installId": testInstallID, "installName": huge, "version": "1",
		"usage": map[string]any{"version": "1"},
	}, nil)
	if resp.StatusCode != 413 {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}
