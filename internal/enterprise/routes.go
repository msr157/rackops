// Package enterprise is the divergence point for RackOps Enterprise features
// (cluster orchestration, RBAC/SSO, GPU scheduling, policies, audit logs —
// see docs/product/). Everything here is a declared stub until those land.
package enterprise

import (
	"encoding/json"
	"net/http"
)

func notImplemented(feature string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "not implemented",
			"feature": feature,
			"edition": "enterprise",
		})
	}
}

// Routes registers Enterprise-only endpoints.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/clusters", notImplemented("cluster orchestration"))
	mux.HandleFunc("GET /api/v1/rbac/roles", notImplemented("RBAC & SSO"))
}
