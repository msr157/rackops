package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rackops/rackops/internal/types"
)

// testAPI builds an API over a throwaway store with one user per given role.
// Returned map: role -> user ID.
func testAPI(t *testing.T, roles ...types.UserRole) (*API, map[types.UserRole]string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	users := map[types.UserRole]string{}
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-horse"), bcrypt.MinCost)
	for _, role := range roles {
		u := &types.User{
			ID: NewID(), Email: string(role) + "@test.local", Role: role,
			PasswordHash: string(hash), CreatedAt: time.Now(),
		}
		store.Mutate(func() { store.Users[u.ID] = u })
		users[role] = u.ID
	}
	// Real servers always run the org migration at startup.
	store.EnsureDefaultOrg()
	return &API{Store: store, Hub: NewHub()}, users
}

// do runs one request through the full middleware chain.
func do(a *API, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)
	return rec
}

func loginToken(t *testing.T, a *API, email string) string {
	t.Helper()
	rec := do(a, http.MethodPost, "/api/auth/login",
		`{"email":"`+email+`","password":"correct-horse"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Token
}

func createKey(t *testing.T, a *API, jwt, body string) (id, secret string) {
	t.Helper()
	rec := do(a, http.MethodPost, "/api/tokens", body, map[string]string{"Authorization": "Bearer " + jwt})
	if rec.Code != http.StatusOK {
		t.Fatalf("create token: status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.ID, out.Token
}

func TestChangePassword(t *testing.T) {
	a, ids := testAPI(t, types.RoleMember)
	jwt := loginToken(t, a, "member@test.local")

	tests := []struct {
		name string
		body string
		want int
	}{
		{"wrong current", `{"current":"nope","new":"new-password-1"}`, http.StatusForbidden},
		{"too short", `{"current":"correct-horse","new":"short"}`, http.StatusBadRequest},
		{"ok", `{"current":"correct-horse","new":"new-password-1"}`, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(a, http.MethodPost, "/api/auth/change-password", tt.body,
				map[string]string{"Authorization": "Bearer " + jwt})
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
		})
	}

	// New password now logs in; the old one is dead.
	var user *types.User
	a.Store.View(func() { user = a.Store.Users[ids[types.RoleMember]] })
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("new-password-1")) != nil {
		t.Fatal("new password not applied")
	}
}

func TestResetTokenLifecycle(t *testing.T) {
	a, ids := testAPI(t, types.RoleMember)

	// Request a reset: uniform 200 whether or not the account exists.
	for _, email := range []string{"member@test.local", "ghost@test.local"} {
		rec := do(a, http.MethodPost, "/api/auth/forgot-password", `{"email":"`+email+`"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("forgot-password(%s): status %d", email, rec.Code)
		}
	}

	var user *types.User
	a.Store.View(func() { user = a.Store.Users[ids[types.RoleMember]] })
	if user.ResetTokenHash == "" || user.ResetExpires.IsZero() {
		t.Fatal("reset token not issued for existing account")
	}

	// The stored hash is not the token; recover a usable token by re-issuing
	// deterministically: instead, drive the real flow — grab the token by
	// resetting via the handler with a token we control.
	// Simulate: overwrite with a known token's hash, as the CRM/log would carry the raw token.
	known := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sum := sha256Hex(known)
	a.Store.Mutate(func() { user.ResetTokenHash = sum })

	// Bad token rejected.
	rec := do(a, http.MethodPost, "/api/auth/reset-password",
		`{"token":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","password":"brand-new-pass"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad token: status %d, want 403", rec.Code)
	}

	// Good token resets and signs in.
	rec = do(a, http.MethodPost, "/api/auth/reset-password",
		`{"token":"`+known+`","password":"brand-new-pass"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: status %d: %s", rec.Code, rec.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"token"`)) {
		t.Fatal("reset should return a session token")
	}

	// Single use: the same token is now dead.
	rec = do(a, http.MethodPost, "/api/auth/reset-password",
		`{"token":"`+known+`","password":"another-pass-1"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("token reuse: status %d, want 403", rec.Code)
	}

	// Expired tokens are rejected.
	a.Store.Mutate(func() {
		user.ResetTokenHash = sum
		user.ResetExpires = time.Now().Add(-time.Minute)
	})
	rec = do(a, http.MethodPost, "/api/auth/reset-password",
		`{"token":"`+known+`","password":"another-pass-1"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expired token: status %d, want 403", rec.Code)
	}
}

func TestAPIKeyActsAsUser(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner, types.RoleViewer)
	ownerJWT := loginToken(t, a, "owner@test.local")
	viewerJWT := loginToken(t, a, "viewer@test.local")

	_, ownerKey := createKey(t, a, ownerJWT, `{"name":"ci"}`)
	_, viewerKey := createKey(t, a, viewerJWT, `{"name":"readonly"}`)

	// Owner key: full access, both header styles.
	for _, hdr := range []map[string]string{
		{"Authorization": "Bearer " + ownerKey},
		{"X-Api-Key": ownerKey},
	} {
		if rec := do(a, http.MethodGet, "/api/nodes", "", hdr); rec.Code != http.StatusOK {
			t.Fatalf("owner key GET: status %d", rec.Code)
		}
	}

	// Viewer key inherits viewer RBAC: reads yes, writes no.
	if rec := do(a, http.MethodGet, "/api/nodes", "", map[string]string{"X-Api-Key": viewerKey}); rec.Code != http.StatusOK {
		t.Fatalf("viewer key GET: status %d", rec.Code)
	}
	if rec := do(a, http.MethodPost, "/api/projects", `{"name":"x"}`,
		map[string]string{"X-Api-Key": viewerKey}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer key POST: status %d, want 403", rec.Code)
	}

	// Garbage key rejected.
	if rec := do(a, http.MethodGet, "/api/nodes", "", map[string]string{"X-Api-Key": "rk_nope"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad key: status %d, want 401", rec.Code)
	}
}

func TestAPIKeyExpiry(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	jwt := loginToken(t, a, "owner@test.local")

	if rec := do(a, http.MethodPost, "/api/tokens", `{"name":"x","expiresIn":"eventually"}`,
		map[string]string{"Authorization": "Bearer " + jwt}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad expiresIn: status %d, want 400", rec.Code)
	}

	id, key := createKey(t, a, jwt, `{"name":"short","expiresIn":"7d"}`)
	if rec := do(a, http.MethodGet, "/api/nodes", "", map[string]string{"X-Api-Key": key}); rec.Code != http.StatusOK {
		t.Fatalf("fresh key: status %d", rec.Code)
	}
	// Force-expire it.
	a.Store.Mutate(func() { a.Store.Tokens[id].ExpiresAt = time.Now().Add(-time.Hour) })
	if rec := do(a, http.MethodGet, "/api/nodes", "", map[string]string{"X-Api-Key": key}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired key: status %d, want 401", rec.Code)
	}
}

func TestTokenOwnership(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner, types.RoleMember)
	ownerJWT := loginToken(t, a, "owner@test.local")
	memberJWT := loginToken(t, a, "member@test.local")

	ownerKeyID, _ := createKey(t, a, ownerJWT, `{"name":"owners"}`)
	memberKeyID, _ := createKey(t, a, memberJWT, `{"name":"members"}`)

	// Members list only their own keys.
	rec := do(a, http.MethodGet, "/api/tokens", "", map[string]string{"Authorization": "Bearer " + memberJWT})
	var listed []types.APIToken
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed) != 1 || listed[0].ID != memberKeyID {
		t.Fatalf("member should list exactly their own key, got %d", len(listed))
	}

	// A member cannot revoke someone else's key…
	if rec := do(a, http.MethodDelete, "/api/tokens/"+ownerKeyID, "",
		map[string]string{"Authorization": "Bearer " + memberJWT}); rec.Code != http.StatusForbidden {
		t.Fatalf("member deleting owner key: status %d, want 403", rec.Code)
	}
	// …but admins/owners can revoke anyone's.
	if rec := do(a, http.MethodDelete, "/api/tokens/"+memberKeyID, "",
		map[string]string{"Authorization": "Bearer " + ownerJWT}); rec.Code != http.StatusNoContent {
		t.Fatalf("owner deleting member key: status %d, want 204", rec.Code)
	}
}

func TestListUsersAdminOnly(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner, types.RoleMember)
	ownerJWT := loginToken(t, a, "owner@test.local")
	memberJWT := loginToken(t, a, "member@test.local")

	if rec := do(a, http.MethodGet, "/api/users", "", map[string]string{"Authorization": "Bearer " + memberJWT}); rec.Code != http.StatusForbidden {
		t.Fatalf("member list users: status %d, want 403", rec.Code)
	}
	if rec := do(a, http.MethodGet, "/api/users", "", map[string]string{"Authorization": "Bearer " + ownerJWT}); rec.Code != http.StatusOK {
		t.Fatalf("owner list users: status %d, want 200", rec.Code)
	}
}
