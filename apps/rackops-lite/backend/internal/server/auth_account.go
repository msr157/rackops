package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rackops/rackops/internal/types"
)

// ---- Account endpoints: me, change-password, forgot/reset-password ----

// authedClaims resolves claims for handlers under /api/auth/, which the
// middleware leaves open (login/signup/reset must work signed-out). Accepts
// the same credentials the middleware does: session JWT or rk_ API key.
func (a *API) authedClaims(r *http.Request) *jwtClaims {
	if c := claimsFrom(r); c != nil {
		return c
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.Header.Get("X-Api-Key")
	}
	if token == "" {
		return nil
	}
	if strings.HasPrefix(token, "rk_") {
		return a.claimsForAPIKey(token)
	}
	c, _ := parseJWT(a.Store.SessionSecret(), token)
	if c != nil {
		valid := false
		a.Store.View(func() {
			if user := a.Store.Users[c.Sub]; user != nil && user.SessionVersion == c.Version {
				c.Role = string(user.Role)
				valid = true
			}
		})
		if !valid {
			return nil
		}
	}
	return c
}

// handleMe returns the authenticated user's own account.
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := a.authedClaims(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var user *types.User
	a.Store.View(func() {
		if u, ok := a.Store.Users[claims.Sub]; ok {
			user = u.Sanitized()
		}
	})
	if user == nil {
		http.Error(w, "account no longer exists", http.StatusUnauthorized)
		return
	}
	writeJSON(w, user)
}

// handleChangePassword verifies the current password before setting a new one.
func (a *API) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := a.authedClaims(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid password-change request", http.StatusBadRequest)
		return
	}
	if len(req.New) < 8 || len(req.New) > 72 {
		http.Error(w, "new password must contain 8 to 72 bytes", http.StatusBadRequest)
		return
	}
	var user types.User
	a.Store.View(func() { if current := a.Store.Users[claims.Sub]; current != nil { user = *current } })
	if user.ID == "" {
		http.Error(w, "account no longer exists", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Current)) != nil {
		http.Error(w, "current password is incorrect", http.StatusForbidden)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	changed := false
	a.Store.Mutate(func() {
		current := a.Store.Users[claims.Sub]
		// Password verification ran outside the lock; refuse to overwrite a
		// concurrent recovery or password change with that stale verification.
		if current == nil || current.PasswordHash != user.PasswordHash || current.SessionVersion != user.SessionVersion { return }
		current.PasswordHash = string(hash)
		current.SessionVersion++
		current.ResetTokenHash = ""
		current.ResetExpires = time.Time{}
		delete(a.Store.ResetEmails, current.ID)
		for id, key := range a.Store.Tokens { if key.UserID == current.ID { delete(a.Store.Tokens, id) } }
		changed = true
	})
	if !changed { http.Error(w, "account changed while checking the password; sign in again", http.StatusConflict); return }
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "password change could not be saved; contact your installation administrator", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetLimiter rate-limits the open forgot-password endpoint. Fixed windows
// are plenty here: the goal is stopping email floods, not precise shaping.
type resetLimiter struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	limit int
	win   time.Duration
}

func newResetLimiter(limit int, win time.Duration) *resetLimiter {
	return &resetLimiter{hits: map[string][]time.Time{}, limit: limit, win: win}
}

func (l *resetLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.win {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

var (
	// Per-IP: 5 requests/hour; per-email: 3/hour. Both must pass.
	resetByIP    = newResetLimiter(5, time.Hour)
	resetByEmail = newResetLimiter(3, time.Hour)
)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// sha256Hex is the at-rest form for every secret we store (API keys, reset
// tokens): compare by hash, never keep the raw value.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// handleForgotPassword issues a single-use, 1-hour reset token. The response
// is identical whether or not the account exists (no enumeration). The reset
// link is sent only to the email relay. Logs and event feeds must never carry
// account recovery credentials.
func (a *API) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	uniform := func() {
		writeJSON(w, map[string]any{
			"ok":      true,
			"message": "If that account exists and email delivery is available, a reset link will be sent. If it does not arrive, contact your installation administrator.",
		})
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		uniform() // don't even confirm the format was rejected
		return
	}
	if !resetByIP.allow(clientIP(r)) || !resetByEmail.allow(req.Email) {
		// Still uniform: a limiter hit must not reveal whether the email exists.
		uniform()
		return
	}

	var user *types.User
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.Email == req.Email {
				user = u
			}
		}
	})
	if user != nil {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			uniform()
			return
		}
		token := hex.EncodeToString(raw)
		link := a.dashboardURL(r) + "/reset-password?token=" + token
		a.Store.Mutate(func() {
			user.ResetTokenHash = sha256Hex(token)
			user.ResetExpires = time.Now().Add(time.Hour)
			a.Store.ResetEmails[user.ID] = &ResetEmail{UserID: user.ID, Email: user.Email, Link: link, TokenHash: user.ResetTokenHash, ExpiresAt: user.ResetExpires}
		})
		if err := a.Store.persistOnce(); err != nil {
			uniform()
			return
		}
		go a.deliverResetEmail(user.ID, time.Now())
	}
	uniform()
}

// handleResetPassword consumes a reset token and sets the new password.
func (a *API) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(req.Token)))
	want := hex.EncodeToString(sum[:])

	var user *types.User
	a.Store.View(func() {
		now := time.Now()
		for _, u := range a.Store.Users {
			if u.ResetTokenHash != "" && !now.After(u.ResetExpires) &&
				subtle.ConstantTimeCompare([]byte(u.ResetTokenHash), []byte(want)) == 1 {
				user = u
			}
		}
	})
	if user == nil {
		http.Error(w, "this reset link is invalid or has expired — request a new one", http.StatusForbidden)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	consumed := false
	a.Store.Mutate(func() {
		// Recheck under the write lock: concurrent reset requests may both
		// pass the first lookup, but only one may consume this credential.
		current := a.Store.Users[user.ID]
		if current != user || user.ResetTokenHash == "" || time.Now().After(user.ResetExpires) || subtle.ConstantTimeCompare([]byte(user.ResetTokenHash), []byte(want)) != 1 {
			return
		}
		user.PasswordHash = string(hash)
		user.ResetTokenHash = ""
		user.ResetExpires = time.Time{}
		user.SessionVersion++
		delete(a.Store.ResetEmails, user.ID)
		for id, key := range a.Store.Tokens {
			if key.UserID == user.ID {
				delete(a.Store.Tokens, id)
			}
		}
		consumed = true
	})
	if !consumed {
		http.Error(w, "this reset link is invalid or has expired — request a new one", http.StatusForbidden)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "password change could not be saved; contact your installation administrator", http.StatusServiceUnavailable)
		return
	}

	// Sign the user straight in — they just proved control of the account.
	token := signJWT(a.Store.SessionSecret(), jwtClaims{
		Sub: user.ID, Role: string(user.Role), Version: user.SessionVersion, Exp: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	writeJSON(w, map[string]any{"token": token, "user": user.Sanitized()})
}

// dashboardURL is the base URL reset links are built on. Raw r.Host would let
// an unauthenticated caller poison emailed links via the Host header, so we
// prefer the address recorded at the last *successful* login (attacker needs
// valid credentials to influence it) and fall back to the request host only
// before any login ever happened (first-run, where the link goes to the log).
func (a *API) dashboardURL(r *http.Request) string {
	var recorded string
	a.Store.View(func() { recorded = a.Store.DashboardURL })
	if recorded != "" {
		return recorded
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// recordDashboardURL persists scheme+host from an authenticated request.
func (a *API) recordDashboardURL(r *http.Request) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	u := fmt.Sprintf("%s://%s", scheme, r.Host)
	a.Store.Mutate(func() { a.Store.DashboardURL = u })
}
