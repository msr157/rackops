package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestResetIsSingleUseUnderConcurrencyAndRevokesSessions(t *testing.T) {
	a, ids := testAPI(t, types.RoleMember)
	oldToken := loginToken(t, a, "member@test.local")
	const proof = "test-reset-proof"
	a.Store.Mutate(func() {
		u := a.Store.Users[ids[types.RoleMember]]
		u.ResetTokenHash = sha256Hex(proof)
		u.ResetExpires = time.Now().Add(time.Hour)
		a.Store.Tokens["old-key"] = &types.APIToken{ID: "old-key", UserID: u.ID}
	})
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			responses <- do(a, "POST", "/api/auth/reset-password", `{"token":"test-reset-proof","password":"replacement-password"}`, nil)
		}()
	}
	close(start)
	success, denied := 0, 0
	for i := 0; i < 2; i++ {
		response := <-responses
		switch response.Code {
		case 200:
			success++
			var result struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if current := do(a, "GET", "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + result.Token}); current.Code != 200 {
				t.Fatal("new session invalid")
			}
		case 403:
			denied++
		default:
			t.Fatalf("unexpected reset response %d", response.Code)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatal("reset credential consumed more than once")
	}
	for _, path := range []string{"/api/services", "/api/auth/me"} {
		if response := do(a, "GET", path, "", map[string]string{"Authorization": "Bearer " + oldToken}); response.Code != 401 {
			t.Fatalf("old session accepted at %s", path)
		}
	}
	if len(a.Store.Tokens) != 0 {
		t.Fatal("account API keys survived recovery")
	}
	r := httptest.NewRequest("GET", "/api/stream?token="+oldToken, nil)
	if a.eventVisible(r, a.Store.DefaultOrgID(), []byte("event: services_changed\ndata: {}\n\n")) {
		t.Fatal("old open stream survived recovery")
	}
}

type recoveryLogCapture struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (c *recoveryLogCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data.Write(p)
}
func (c *recoveryLogCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data.String()
}

func TestRecoveryCredentialsOnlyGoToEmailRelay(t *testing.T) {
	a, ids := testAPI(t, types.RoleMember)
	a.Store.Mutate(func() { a.Store.Users[ids[types.RoleMember]].Email = "recovery-fixture@test.local" })
	relay := make(chan string, 1)
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Link string `json:"link"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		relay <- payload.Link
		w.WriteHeader(204)
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	var capture recoveryLogCapture
	previous := log.Writer()
	log.SetOutput(&capture)
	defer log.SetOutput(previous)
	response := do(a, "POST", "/api/auth/forgot-password", `{"email":"recovery-fixture@test.local"}`, nil)
	select {
	case link := <-relay:
		if !strings.Contains(link, "/reset-password?token=") {
			t.Fatal("relay did not receive credential")
		}
		if strings.Contains(capture.String(), "token=") || strings.Contains(response.Body.String(), "token=") {
			t.Fatal("reset credential leaked outside relay")
		}
	case <-time.After(time.Second):
		t.Fatal("email relay not called")
	}
}
