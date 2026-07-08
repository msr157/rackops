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
	"os"
	"path/filepath"
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

// DeriveJWTSecret derives the signing secret from the cluster join token so
// every server in an HA cluster agrees on it — sessions survive leader
// failover — and it stays stable across restarts. Falls back to a persisted
// random secret only if no join token is available.
func DeriveJWTSecret(dataDir, joinToken string) []byte {
	if joinToken != "" {
		sum := sha256.Sum256([]byte("rackops-jwt-v1:" + joinToken))
		return sum[:]
	}
	path := filepath.Join(dataDir, "jwt-secret")
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	_ = os.WriteFile(path, b, 0o600)
	return b
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
				claims, _ = parseJWT(a.JWTSecret, tok)
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
	a.Store.Mutate(func() { a.Store.Users[u.ID] = u })
	a.Store.AddEvent("auth", "User created: "+u.Email+" ("+string(role)+")", "", "")

	token := signJWT(a.JWTSecret, jwtClaims{Sub: u.ID, Role: string(u.Role), Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
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
	token := signJWT(a.JWTSecret, jwtClaims{Sub: user.ID, Role: string(user.Role), Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
	writeJSON(w, map[string]any{"token": token, "user": user.Sanitized()})
}

// handleListUsers returns all accounts (admin+).
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
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
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	claims := claimsFrom(r)
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	secret := "rk_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	t := &types.APIToken{
		ID: NewID(), Name: req.Name, TokenHash: hex.EncodeToString(sum[:]), CreatedAt: time.Now(),
	}
	if claims != nil {
		t.UserID = claims.Sub
	}
	a.Store.Mutate(func() {
		if a.Store.Tokens == nil {
			a.Store.Tokens = map[string]*types.APIToken{}
		}
		a.Store.Tokens[t.ID] = t
	})
	// The secret is shown exactly once.
	writeJSON(w, map[string]any{"id": t.ID, "name": t.Name, "token": secret})
}

func (a *API) handleListTokens(w http.ResponseWriter, r *http.Request) {
	var out []*types.APIToken
	a.Store.View(func() {
		for _, t := range a.Store.Tokens {
			out = append(out, t.Sanitized())
		}
	})
	writeJSON(w, out)
}

func (a *API) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	a.Store.Mutate(func() { delete(a.Store.Tokens, r.PathValue("id")) })
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
			p == "/api/traefik/config" ||
			p == "/api/cluster" ||
			p == "/api/cluster/join"
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
			token = r.URL.Query().Get("token") // SSE can't set headers
		}
		if token == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		// API tokens (rk_...) or JWT.
		if strings.HasPrefix(token, "rk_") {
			sum := sha256.Sum256([]byte(token))
			hash := hex.EncodeToString(sum[:])
			ok := false
			a.Store.View(func() {
				for _, t := range a.Store.Tokens {
					if t.TokenHash == hash {
						ok = true
					}
				}
			})
			if !ok {
				http.Error(w, "invalid API token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		claims, err := parseJWT(a.JWTSecret, token)
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}
		// Coarse RBAC: viewers are read-only.
		if claims.Role == string(types.RoleViewer) && r.Method != http.MethodGet {
			http.Error(w, "viewers have read-only access", http.StatusForbidden)
			return
		}
		ctx := contextWithClaims(r, claims)
		next.ServeHTTP(w, ctx)
	})
}

func contextWithClaims(r *http.Request, c *jwtClaims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), claimsKey, c))
}
