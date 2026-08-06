package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// maxBodyBytes caps every JSON request body (256 KB).
const maxBodyBytes = 256 << 10

// API wires the store, mailer and admin credentials into an http.Handler.
type API struct {
	Store  *Store
	Mailer Mailer
	Admin  *AdminAuth
	DashFS fs.FS // embedded dashboard; nil when not built
	// FrontDoor is the tunnel edge running in this process; nil when the
	// operator did not enable it.
	FrontDoor *FrontDoor

	// Ingest hardening: per-IP token buckets. Reset-email additionally
	// limits per target address so one IP can't hammer many mailboxes and
	// one mailbox can't be hammered from many IPs.
	ingestLimiter     *rateLimiter
	resetIPLimiter    *rateLimiter
	resetEmailLimiter *rateLimiter
	loginLimiter      *rateLimiter
}

// NewAPI constructs the handler set with production rate limits.
func NewAPI(store *Store, mailer Mailer, admin *AdminAuth, dashFS fs.FS) *API {
	return &API{
		Store:  store,
		Mailer: mailer,
		Admin:  admin,
		DashFS: dashFS,
		// 60/min sustained, burst 20 per IP on all ingest routes.
		ingestLimiter: newRateLimiter(60, 20),
		// Reset email: 5/hour per IP AND per email address.
		resetIPLimiter:    newRateLimiter(5.0/60.0, 5),
		resetEmailLimiter: newRateLimiter(5.0/60.0, 5),
		loginLimiter:      newRateLimiter(10, 10),
	}
}

// Handler builds the full route table.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public ingest API — called by customer RackOps Lite instances.
	mux.HandleFunc("POST /ingest/v1/tickets", a.handleIngestTicket)
	mux.HandleFunc("GET /ingest/v1/tickets/{ticketId}/replies", a.handleIngestReplies)
	mux.HandleFunc("POST /ingest/v1/reset-email", a.handleIngestResetEmail)
	mux.HandleFunc("POST /ingest/v1/snapshots", a.handleIngestSnapshot)

	// Admin API — JWT bearer.
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/overview", a.handleOverview)
	mux.HandleFunc("GET /api/installs", a.handleListInstalls)
	mux.HandleFunc("GET /api/installs/{id}", a.handleGetInstall)
	mux.HandleFunc("GET /api/tickets", a.handleListTickets)
	mux.HandleFunc("GET /api/tickets/{id}", a.handleGetTicket)
	mux.HandleFunc("POST /api/tickets/{id}/reply", a.handleTicketReply)
	mux.HandleFunc("POST /api/tickets/{id}/note", a.handleTicketNote)
	mux.HandleFunc("POST /api/tickets/{id}/status", a.handleTicketStatus)

	// Front Door — the tunnel edge, administered from the same console and
	// behind the same login.
	a.frontDoorRoutes(mux)

	// Liveness for container orchestration.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Embedded dashboard with SPA fallback.
	if a.DashFS != nil {
		mux.Handle("/", spaHandler(a.DashFS))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("RackOps CRM API — dashboard not built. See internal-apps/crm/README.md.\n"))
		})
	}

	return a.authMiddleware(a.ingestGuard(mux))
}

// ingestGuard applies the shared per-IP rate limit and the body-size cap to
// every /ingest/* route before dispatch.
func (a *API) ingestGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ingest/") {
			if !a.ingestLimiter.allow(clientIP(r)) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits a JSON error envelope. msg must never contain internal
// details — callers pass a fixed, user-safe string and log the real error.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON parses a JSON body, distinguishing an oversized body from a
// malformed one without leaking internals.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

// serverError logs the internal error and returns an opaque 500.
func serverError(w http.ResponseWriter, op string, err error) {
	log.Printf("crm: %s: %v", op, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// spaHandler serves the embedded dashboard with SPA fallback to index.html.
// index.html is no-cache so browsers pick up new asset hashes after an
// upgrade; hashed assets cache forever. (Mirrors RackOps Lite.)
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/" // SPA fallback
			path = "index.html"
		}
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		} else if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
