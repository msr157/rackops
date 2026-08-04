package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rackops/rackops/internal/types"
)

// ---- Minimal JWT (HS256) ----

type jwtClaims struct {
	Sub  string `json:"sub"` // user ID
	Role string `json:"role"`
	Exp  int64  `json:"exp"`
}

func signJWT(secret []byte, c jwtClaims) string {
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(c)
	msg := header + "." + b64(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return msg + "." + b64(mac.Sum(nil))
}

func parseJWT(secret []byte, token string) (*jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return nil, fmt.Errorf("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c jwtClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	if time.Now().Unix() > c.Exp {
		return nil, fmt.Errorf("token expired")
	}
	return &c, nil
}

// ---- Handlers ----

// handleAuthStatus tells the UI whether setup is needed.
func (a *API) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	users := 0
	a.Store.View(func() { users = len(a.Store.Users) })
	writeJSON(w, map[string]any{"needsSetup": users == 0})
}

// handleSignup provisions an account. The first account becomes Owner;
// afterwards only admins can create users (enforced by middleware).
func (a *API) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		FullName string `json:"fullName"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		http.Error(w, "valid email and a password of 8+ characters required", http.StatusBadRequest)
		return
	}

	first := false
	var exists bool
	a.Store.View(func() {
		first = len(a.Store.Users) == 0
		for _, u := range a.Store.Users {
			if u.Email == req.Email {
				exists = true
			}
		}
	})
	if exists {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}
	if !first {
		// Only authenticated admins/owners may add users. Auth routes are
		// open in the middleware, so parse the bearer token here directly.
		claims := claimsFrom(r)
		if claims == nil {
			if tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); tok != "" {
				claims, _ = parseJWT(a.Store.SessionSecret(), tok)
			}
		}
		if claims == nil || (claims.Role != string(types.RoleOwner) && claims.Role != string(types.RoleAdmin)) {
			http.Error(w, "only admins can create users", http.StatusForbidden)
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	role := types.UserRole(req.Role)
	if first {
		role = types.RoleOwner
	} else if role == "" || role == types.RoleOwner {
		role = types.RoleMember
	}
	u := &types.User{
		ID: NewID(), Email: req.Email, FullName: strings.TrimSpace(req.FullName),
		PasswordHash: string(hash), Role: role, CreatedAt: time.Now(),
	}
	a.Store.Mutate(func() {
		a.Store.Users[u.ID] = u
		// Enroll into the default org so the account has a seat somewhere
		// (invitation signups enroll into their invited org instead — see
		// handleAcceptInvitation). Platform owner/admin hold the org.
		var def *types.Organization
		for _, o := range a.Store.Orgs {
			if def == nil || o.CreatedAt.Before(def.CreatedAt) {
				def = o
			}
		}
		if def != nil && def.Member(u.ID) == nil {
			orgRole := types.OrgRoleMember
			switch role {
			case types.RoleOwner:
				orgRole = types.OrgRoleOwner
			case types.RoleAdmin:
				orgRole = types.OrgRoleAdmin
			}
			def.Members = append(def.Members, types.OrgMember{
				UserID: u.ID, Role: orgRole, AddedAt: time.Now(),
				Permissions: defaultMemberPermissions(),
			})
		}
	})
	a.Store.AddEvent("auth", "User created: "+u.Email+" ("+string(role)+")", "", "")

	token := signJWT(a.Store.SessionSecret(), jwtClaims{Sub: u.ID, Role: string(u.Role), Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
	writeJSON(w, map[string]any{"token": token, "user": u.Sanitized()})
}

// handleLogin authenticates and issues a JWT.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	var user *types.User
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.Email == req.Email {
				user = u
			}
		}
	})
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token := signJWT(a.Store.SessionSecret(), jwtClaims{Sub: user.ID, Role: string(user.Role), Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
	// Remember where operators actually reach the dashboard — reset links are
	// built on this (see dashboardURL).
	a.recordDashboardURL(r)
	writeJSON(w, map[string]any{"token": token, "user": user.Sanitized()})
}

// handleListUsers returns all accounts (admin+ only: the account list is a
// reconnaissance goldmine and none of a member's business).
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil || (claims.Role != string(types.RoleOwner) && claims.Role != string(types.RoleAdmin)) {
		http.Error(w, "admin access required", http.StatusForbidden)
		return
	}
	var out []*types.User
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			out = append(out, u.Sanitized())
		}
	})
	writeJSON(w, out)
}

// ---- API tokens ----

func (a *API) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		ExpiresIn string `json:"expiresIn"` // "", "7d", "30d", "90d", "1y"
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	claims := claimsFrom(r)
	if claims == nil {
		// Keys act as their creating user; a key created by nobody would be
		// unattributable power. (Also blocks keys minting further keys.)
		http.Error(w, "sign in with your account to create API keys", http.StatusForbidden)
		return
	}
	var expires time.Time
	switch req.ExpiresIn {
	case "", "never":
	case "7d":
		expires = time.Now().AddDate(0, 0, 7)
	case "30d":
		expires = time.Now().AddDate(0, 0, 30)
	case "90d":
		expires = time.Now().AddDate(0, 0, 90)
	case "1y":
		expires = time.Now().AddDate(1, 0, 0)
	default:
		http.Error(w, "expiresIn must be one of: never, 7d, 30d, 90d, 1y", http.StatusBadRequest)
		return
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	secret := "rk_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	t := &types.APIToken{
		ID: NewID(), Name: req.Name, TokenHash: hex.EncodeToString(sum[:]),
		CreatedAt: time.Now(), UserID: claims.Sub, ExpiresAt: expires,
	}
	a.Store.Mutate(func() {
		if a.Store.Tokens == nil {
			a.Store.Tokens = map[string]*types.APIToken{}
		}
		a.Store.Tokens[t.ID] = t
	})
	// The secret is shown exactly once.
	writeJSON(w, map[string]any{"id": t.ID, "name": t.Name, "token": secret, "expiresAt": t.ExpiresAt})
}

func (a *API) handleListTokens(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	admin := claims != nil && (claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin))
	var out []*types.APIToken
	a.Store.View(func() {
		for _, t := range a.Store.Tokens {
			// Everyone sees their own keys; admins see the whole install's.
			if admin || (claims != nil && t.UserID == claims.Sub) {
				out = append(out, t.Sanitized())
			}
		}
	})
	writeJSON(w, out)
}

func (a *API) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	admin := claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin)
	id := r.PathValue("id")
	forbidden := false
	a.Store.Mutate(func() {
		t := a.Store.Tokens[id]
		if t == nil {
			return // deleting a gone token is fine
		}
		if !admin && t.UserID != claims.Sub {
			forbidden = true
			return
		}
		delete(a.Store.Tokens, id)
	})
	if forbidden {
		http.Error(w, "you can only revoke your own API keys", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Middleware ----

type ctxKey string

const claimsKey ctxKey = "claims"

func claimsFrom(r *http.Request) *jwtClaims {
	if c, ok := r.Context().Value(claimsKey).(*jwtClaims); ok {
		return c
	}
	return nil
}

// authMiddleware guards the API. Open paths: auth endpoints, agent ingest
// (join-token), traefik config, relay, SSE (token via query), dashboard assets.
func (a *API) authMiddleware(next http.Handler) http.Handler {
	open := func(p string) bool {
		return !strings.HasPrefix(p, "/api/") ||
			strings.HasPrefix(p, "/api/auth/") ||
			p == "/api/agent/ingest" ||
			p == "/api/agent/logstream" ||
			p == "/api/traefik/config" ||
			p == "/api/edge-probe" ||
			p == "/api/cluster" ||
			p == "/api/cluster/join" ||
			// The API *description* is public docs; every operation in it
			// still authenticates individually. Swagger's first fetch happens
			// before the user clicks Authorize.
			p == "/api/openapi.json"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if open(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// Zero users => setup mode: API open until the owner account exists.
		users := 0
		a.Store.View(func() { users = len(a.Store.Users) })
		if users == 0 {
			next.ServeHTTP(w, r)
			return
		}

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.Header.Get("X-Api-Key") // Dokploy-style header, same keys
		}
		if token == "" {
			token = r.URL.Query().Get("token") // SSE can't set headers
		}
		if token == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		var claims *jwtClaims
		if strings.HasPrefix(token, "rk_") {
			// API keys act as the user who created them — identical RBAC to a
			// session, never an anonymous super-credential.
			claims = a.claimsForAPIKey(token)
			if claims == nil {
				http.Error(w, "invalid or expired API key", http.StatusUnauthorized)
				return
			}
		} else {
			var err error
			claims, err = parseJWT(a.Store.SessionSecret(), token)
			if err != nil {
				http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
		}
		// Coarse RBAC: viewers are read-only on fleet state. Managing their
		// OWN API keys is account-scoped, not a fleet mutation — the token
		// handlers enforce ownership, and a viewer's key is itself read-only.
		if claims.Role == string(types.RoleViewer) && r.Method != http.MethodGet &&
			!strings.HasPrefix(r.URL.Path, "/api/tokens") {
			http.Error(w, "viewers have read-only access", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, contextWithClaims(r, claims))
	})
}

// claimsForAPIKey resolves an rk_ secret to claims for its owning user.
// Returns nil for unknown/expired keys and keys whose user no longer exists —
// deleting an account instantly kills its keys.
func (a *API) claimsForAPIKey(secret string) *jwtClaims {
	sum := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(sum[:])
	var tok *types.APIToken
	var user *types.User
	a.Store.View(func() {
		for _, t := range a.Store.Tokens {
			if hmac.Equal([]byte(t.TokenHash), []byte(hash)) {
				tok = t
				break
			}
		}
		if tok != nil {
			user = a.Store.Users[tok.UserID]
		}
	})
	if tok == nil || user == nil {
		return nil
	}
	if !tok.ExpiresAt.IsZero() && time.Now().After(tok.ExpiresAt) {
		return nil
	}
	// Throttled last-used stamp: at most one write per minute per key, so
	// polling clients don't turn every request into a raft proposal.
	if time.Since(tok.LastUsed) > time.Minute {
		a.Store.Mutate(func() { tok.LastUsed = time.Now() })
	}
	return &jwtClaims{Sub: user.ID, Role: string(user.Role), Exp: time.Now().Add(time.Minute).Unix()}
}

func contextWithClaims(r *http.Request, c *jwtClaims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), claimsKey, c))
}
