package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnrollmentPersistsCredentialBeforeExchangeAndReusesIt(t *testing.T) {
	j, err := openTaskJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var credential string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			NodeID     string `json:"nodeId"`
			Credential string `json:"credential"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.NodeID != "node-fixture" {
			t.Error("invalid exchange")
		}
		if credential == "" {
			credential = req.Credential
		}
		if req.Credential != credential {
			t.Error("retry minted different identity")
		}
		sealed, err := os.ReadFile(filepath.Join(j.dir, "node-credential"))
		if err != nil || strings.Contains(string(sealed), credential) {
			t.Error("credential not durably protected before exchange")
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nodeId":"node-fixture","orgId":"org-fixture"}`))
	}))
	defer server.Close()
	cfg := Config{ServerURL: server.URL, Token: "rkenroll_fixture", OrgID: "org-fixture"}
	if err := enrollAgent(context.Background(), &cfg, "node-fixture", j, server.Client()); err == nil {
		t.Fatal("expected retryable failure")
	}
	if err := enrollAgent(context.Background(), &cfg, "node-fixture", j, server.Client()); err != nil {
		t.Fatal(err)
	}
	if cfg.Token != credential {
		t.Fatal("agent kept enrollment token")
	}
	cfg.Token = "rkenroll_expired"
	if err := enrollAgent(context.Background(), &cfg, "node-fixture", j, server.Client()); err != nil || calls != 3 {
		t.Fatal("restart lost saved identity")
	}
	cfg.Token = "rkenroll_other"
	cfg.ServerURL = "http://192.0.2.1"
	if err := enrollAgent(context.Background(), &cfg, "node-fixture", j, server.Client()); err == nil {
		t.Fatal("unencrypted remote enrollment allowed")
	}
}
