package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestPasswordChangeRevokesAccountAccessAcrossRestart(t *testing.T) {
	a, ids := testAPI(t, types.RoleOwner, types.RoleMember)
	old := loginToken(t, a, "owner@test.local")
	other := loginToken(t, a, "member@test.local")
	_, key := createKey(t, a, old, `{"name":"old automation"}`)
	_, otherKey := createKey(t, a, other, `{"name":"other automation"}`)
	a.Store.Mutate(func() {
		u := a.Store.Users[ids[types.RoleOwner]]
		u.ResetTokenHash = sha256Hex("pending-reset-proof")
		u.ResetExpires = time.Now().Add(time.Hour)
		a.Store.ResetEmails = map[string]*ResetEmail{u.ID: {TokenHash: u.ResetTokenHash}}
	})
	response := do(a, "POST", "/api/auth/change-password", `{"current":"correct-horse","new":"replacement-password"}`, map[string]string{"Authorization": "Bearer " + old})
	if response.Code != 204 {
		t.Fatalf("change: %d %s", response.Code, response.Body)
	}
	verify := func() {
		t.Helper()
		for _, token := range []string{old, key} {
			for _, path := range []string{"/api/auth/me", "/api/services"} {
				if r := do(a, "GET", path, "", map[string]string{"Authorization": "Bearer " + token}); r.Code != 401 {
					t.Fatalf("revoked credential accepted: %s %d", path, r.Code)
				}
			}
		}
		for _, token := range []string{other, otherKey} {
			if r := do(a, "GET", "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + token}); r.Code != 200 {
				t.Fatal("another account was revoked")
			}
		}
		if r := do(a, "POST", "/api/auth/reset-password", `{"token":"pending-reset-proof","password":"another-password"}`, nil); r.Code != 403 {
			t.Fatal("old reset link survived password change")
		}
		if len(a.Store.ResetEmails) != 0 {
			t.Fatal("stale reset email retained")
		}
		request := httptest.NewRequest("GET", "/api/stream?token="+old, nil)
		if a.eventVisible(request, a.Store.DefaultOrgID(), []byte("event: services_changed\ndata: {}\n\n")) {
			t.Fatal("revoked session still receives events")
		}
		if r := do(a, "POST", "/api/auth/login", `{"email":"owner@test.local","password":"replacement-password"}`, nil); r.Code != 200 {
			t.Fatal("new password cannot sign in")
		}
	}
	verify()
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	verify()
}

func TestConcurrentPasswordChangesHaveOnlyOneWinner(t *testing.T) {
	a, _ := testAPI(t, types.RoleMember)
	token := loginToken(t, a, "member@test.local")
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, password := range []string{"replacement-one", "replacement-two"} {
		go func(password string) {
			<-start
			body, _ := json.Marshal(map[string]string{"current": "correct-horse", "new": password})
			results <- do(a, "POST", "/api/auth/change-password", string(body), map[string]string{"Authorization": "Bearer " + token}).Code
		}(password)
	}
	close(start)
	winners := 0
	for i := 0; i < 2; i++ {
		code := <-results
		if code == 204 {
			winners++
		} else if code != 401 && code != 403 && code != 409 {
			t.Fatalf("unexpected concurrent result: %d", code)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d password-change winners", winners)
	}
}

func TestPasswordChangeRejectsOversizedPasswordWithoutRevocation(t *testing.T) {
	a, _ := testAPI(t, types.RoleMember)
	token := loginToken(t, a, "member@test.local")
	body, _ := json.Marshal(map[string]string{"current": "correct-horse", "new": strings.Repeat("x", 73)})
	if r := do(a, "POST", "/api/auth/change-password", string(body), map[string]string{"Authorization": "Bearer " + token}); r.Code != 400 {
		t.Fatal("oversized password accepted")
	}
	if r := do(a, "GET", "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + token}); r.Code != 200 {
		t.Fatal("invalid password request revoked session")
	}
}
