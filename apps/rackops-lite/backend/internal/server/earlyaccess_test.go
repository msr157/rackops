package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

const websiteOrigin = "https://rackops.in"

// The form is public, so every case here runs against a store that already
// has an owner — that is when authMiddleware is enforcing, and the endpoint
// must still be reachable.

func TestEarlyAccessRecordsRequest(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)

	rec := do(a, "POST", "/api/v1/early-access",
		`{"email":"ops@example.com","name":"Ada","industry":"logistics","notes":"12 racks"}`,
		map[string]string{"Content-Type": "application/json", "Origin": websiteOrigin})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["status"] != "recorded" {
		t.Errorf("status field = %q, want %q", got["status"], "recorded")
	}

	var lead *types.EarlyAccessLead
	a.Store.View(func() { lead = a.Store.EarlyAccess["ops@example.com"] })
	if lead == nil {
		t.Fatal("lead was not stored under its lowercased email")
	}
	if lead.Name != "Ada" || lead.Industry != "logistics" || lead.Notes != "12 racks" {
		t.Errorf("stored lead = %+v, fields did not round-trip", lead)
	}
	if lead.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

// The website shows a distinct message for 409, so the duplicate path must not
// collapse into a generic error.
func TestEarlyAccessDuplicateEmailConflicts(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	body := `{"email":"Ops@Example.com","name":"Ada","industry":"logistics"}`
	hdr := map[string]string{"Content-Type": "application/json", "Origin": websiteOrigin}

	if rec := do(a, "POST", "/api/v1/early-access", body, hdr); rec.Code != http.StatusOK {
		t.Fatalf("first submit = %d, want 200", rec.Code)
	}
	// Same address, different casing — must still collide.
	rec := do(a, "POST", "/api/v1/early-access",
		`{"email":"ops@example.com","name":"Ada","industry":"logistics"}`, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second submit = %d, want 409", rec.Code)
	}
}

func TestEarlyAccessRejectsIncompleteSubmissions(t *testing.T) {
	cases := map[string]string{
		"missing email":    `{"email":"","name":"Ada","industry":"logistics"}`,
		"missing name":     `{"email":"ops@example.com","name":" ","industry":"logistics"}`,
		"missing industry": `{"email":"ops@example.com","name":"Ada","industry":""}`,
		"malformed email":  `{"email":"not-an-email","name":"Ada","industry":"logistics"}`,
		"invalid json":     `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			rec := do(a, "POST", "/api/v1/early-access", body,
				map[string]string{"Content-Type": "application/json", "Origin": websiteOrigin})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			count := 0
			a.Store.View(func() { count = len(a.Store.EarlyAccess) })
			if count != 0 {
				t.Errorf("stored %d leads, want 0", count)
			}
		})
	}
}

// Without these headers the browser discards the response and the visitor sees
// a generic failure even though the lead was recorded.
func TestEarlyAccessCORS(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	hdr := map[string]string{"Content-Type": "application/json", "Origin": websiteOrigin}

	rec := do(a, "POST", "/api/v1/early-access",
		`{"email":"ops@example.com","name":"Ada","industry":"logistics"}`, hdr)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != websiteOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, websiteOrigin)
	}

	// Preflight must succeed on its own — it carries no body and no auth.
	pre := do(a, "OPTIONS", "/api/v1/early-access", "", hdr)
	if pre.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", pre.Code)
	}
	if got := pre.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("preflight did not advertise allowed methods")
	}

	// An origin outside the allow-list gets no CORS grant, so a hostile page
	// cannot read the response.
	other := do(a, "POST", "/api/v1/early-access",
		`{"email":"evil@example.com","name":"Eve","industry":"spam"}`,
		map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"})
	if got := other.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q for a disallowed origin, want empty", got)
	}
}
