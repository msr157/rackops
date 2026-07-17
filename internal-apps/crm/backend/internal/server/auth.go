package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ---- Minimal JWT (HS256) — same style as RackOps Lite's auth.go ----

type jwtClaims struct {
	Sub string `json:"sub"` // admin email
	Exp int64  `json:"exp"`
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

// ---- Admin credentials ----

// AdminAuth holds the single admin account, sourced from CRM_ADMIN_EMAIL and
// CRM_ADMIN_PASSWORD. The password is bcrypt-hashed once at startup and the
// plaintext dropped; login compares against the hash.
type AdminAuth struct {
	Email        string
	passwordHash []byte
}

// NewAdminAuth hashes the configured password. Both values are required.
func NewAdminAuth(email, password string) (*AdminAuth, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("CRM_ADMIN_EMAIL and CRM_ADMIN_PASSWORD must be set")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash admin password: %w", err)
	}
	return &AdminAuth{Email: email, passwordHash: hash}, nil
}

// Check verifies a login attempt.
func (a *AdminAuth) Check(email, password string) bool {
	if strings.ToLower(strings.TrimSpace(email)) != a.Email {
		// Burn comparable time so a valid email isn't discoverable by timing.
		_ = bcrypt.CompareHashAndPassword(a.passwordHash, []byte("timing-equalizer"))
		return false
	}
	return bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)) == nil
}

// ---- Handlers & middleware ----

// handleLogin authenticates the admin and issues a 7-day JWT.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.loginLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	if !a.Admin.Check(req.Email, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token := signJWT(a.Store.SessionSecret(), jwtClaims{
		Sub: a.Admin.Email,
		Exp: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	writeJSON(w, map[string]any{"token": token})
}

// authMiddleware guards /api/*. Open paths: login, /ingest/* (secured by
// poll secrets + rate limits), and the dashboard assets.
func (a *API) authMiddleware(next http.Handler) http.Handler {
	open := func(p string) bool {
		return !strings.HasPrefix(p, "/api/") || p == "/api/auth/login"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if open(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if _, err := parseJWT(a.Store.SessionSecret(), token); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
