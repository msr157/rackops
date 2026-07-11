// Package server implements the RackOps control plane HTTP server shared by
// the Lite and Enterprise binaries.
package server

import (
	"database/sql"
	"log"
	"net/http"

	_ "github.com/lib/pq"
)

// Version is the control-plane version reported by /api/v1/meta.
const Version = "0.1.0"

type Options struct {
	// Edition is "lite" or "enterprise".
	Edition string
	// DashboardDir, when non-empty, serves a built dashboard SPA at /.
	DashboardDir string
	// Extra lets an edition register additional routes on the mux.
	Extra func(mux *http.ServeMux)
	// DatabaseURL is the optional Postgres connection string (DATABASE_URL).
	// When empty the server runs with an in-memory store.
	DatabaseURL string
}

type Server struct {
	opts  Options
	store *Store
}

// New assembles the control-plane handler.
func New(opts Options) http.Handler {
	var store *Store
	if opts.DatabaseURL != "" {
		db, err := sql.Open("postgres", opts.DatabaseURL)
		if err != nil {
			log.Printf("server: could not open postgres (%v) — falling back to in-memory store", err)
			store = NewStore()
		} else if err = db.Ping(); err != nil {
			log.Printf("server: postgres ping failed (%v) — falling back to in-memory store", err)
			store = NewStore()
		} else {
			log.Println("server: connected to postgres")
			store = NewStoreWithDB(db)
		}
	} else {
		log.Println("server: DATABASE_URL not set — using in-memory store")
		store = NewStore()
	}

	s := &Server{opts: opts, store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/report", s.handleReport)
	mux.HandleFunc("GET /api/v1/nodes", s.handleListNodes)
	mux.HandleFunc("GET /api/v1/nodes/{id}", s.handleGetNode)
	mux.HandleFunc("GET /api/v1/meta", s.handleMeta)
	mux.HandleFunc("POST /api/v1/early-access", s.handleEarlyAccess)
	mux.HandleFunc("GET /healthz", handleHealthz)

	if opts.Extra != nil {
		opts.Extra(mux)
	}
	if opts.DashboardDir != "" {
		mux.Handle("/", spaHandler(opts.DashboardDir))
	}

	return logRequests(cors(mux))
}
