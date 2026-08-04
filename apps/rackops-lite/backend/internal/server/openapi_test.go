package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

// TestOpenAPIMatchesRouter proves the docs can't drift: every route in the
// table must register without panicking, and the generated document must
// contain exactly the table's operations.
func TestOpenAPIMatchesRouter(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)

	// Router() panics on duplicate/invalid patterns — building it IS the
	// registration test.
	router := a.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi.json: status %d", rec.Code)
	}

	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Info    map[string]any            `json:"info"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("openapi version = %q", doc.OpenAPI)
	}

	// Every table route appears in the doc with its method.
	for _, rt := range a.apiRoutes() {
		p := strings.ReplaceAll(rt.path, "{rest...}", "{rest}")
		ops, ok := doc.Paths[p]
		if !ok {
			t.Errorf("route %s %s missing from openapi paths", rt.method, rt.path)
			continue
		}
		if _, ok := ops[strings.ToLower(rt.method)]; !ok {
			t.Errorf("route %s %s: method missing from openapi", rt.method, rt.path)
		}
	}

	// And the doc contains nothing the table doesn't (drift in the other direction).
	tableOps := map[string]bool{}
	for _, rt := range a.apiRoutes() {
		p := strings.ReplaceAll(rt.path, "{rest...}", "{rest}")
		tableOps[strings.ToLower(rt.method)+" "+p] = true
	}
	for p, ops := range doc.Paths {
		for m := range ops {
			if !tableOps[m+" "+p] {
				t.Errorf("openapi documents %s %s which is not in the route table", m, p)
			}
		}
	}
}

func TestSwaggerServesEmbeddedAssets(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	router := a.Router()

	for _, tt := range []struct {
		path     string
		wantType string
	}{
		{"/swagger", "text/html"},
		{"/swagger/swagger-ui-bundle.js", "application/javascript"},
		{"/swagger/swagger-ui.css", "text/css"},
	} {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", tt.path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.wantType) {
			t.Errorf("%s: content-type %q, want prefix %q", tt.path, ct, tt.wantType)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", tt.path)
		}
	}
}
