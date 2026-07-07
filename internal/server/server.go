// Package server implements the RackOps control plane HTTP server shared by
// the Lite and Enterprise binaries.
package server

import (
	"net/http"
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
}

type Server struct {
	opts  Options
	store *Store
}

// New assembles the control-plane handler.
func New(opts Options) http.Handler {
	s := &Server{opts: opts, store: NewStore()}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/report", s.handleReport)
	mux.HandleFunc("GET /api/v1/nodes", s.handleListNodes)
	mux.HandleFunc("GET /api/v1/nodes/{id}", s.handleGetNode)
	mux.HandleFunc("GET /api/v1/meta", s.handleMeta)
	mux.HandleFunc("GET /healthz", handleHealthz)

	if opts.Extra != nil {
		opts.Extra(mux)
	}
	if opts.DashboardDir != "" {
		mux.Handle("/", spaHandler(opts.DashboardDir))
	}

	return logRequests(cors(mux))
}
