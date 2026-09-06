package edge

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentCannotAdministerOrReplaceAnotherIdentity(t *testing.T) {
	r := newTestRegistry(t)
	key := testKey(t)
	_, err := r.Upsert(Tenant{ID: "one", PublicKey: key, Workspace: "acme", QuotaBytes: 1234})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAdminAPI(newTestEdge(t, r), "vendor-only-secret-never-distributed")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := a.IssueEnrollmentToken("one")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"publicKey":"` + key + `","workspace":"acme","hostnames":["app-acme.link.rackops.in"]}`
	request := func(method, path, credential, payload string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+credential)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if got := request("PUT", "/enroll/v1/tenants/one", token, body); got != 200 {
		t.Fatalf("own enrollment: %d", got)
	}
	for _, tc := range []struct{ method, path, credential, body string }{
		{"PUT", "/enroll/v1/tenants/two", token, body},
		{"PUT", "/enroll/v1/tenants/one", a.token, body},
		{"PUT", "/enroll/v1/tenants/one", token, strings.Replace(body, key, testKey(t), 1)},
		{"PUT", "/enroll/v1/tenants/one", token, strings.Replace(body, "app-acme", "app-other", 1)},
		{"PUT", "/enroll/v1/tenants/one", token, strings.TrimSuffix(body, "}") + `,"quotaBytes":999999}`},
		{"GET", "/admin/tenants", token, ""},
		{"DELETE", "/admin/tenants/one", token, ""},
		{"POST", "/admin/tenants/one/suspend", token, "{}"},
		{"POST", "/admin/tenants/one/enrollment", token, ""},
	} {
		got := request(tc.method, tc.path, tc.credential, tc.body)
		if got != http.StatusForbidden && got != http.StatusUnauthorized {
			t.Fatalf("%s %s: %d", tc.method, tc.path, got)
		}
	}
	// Expiration is checked even for a correctly signed grant.
	b, _ := json.Marshal(enrollmentClaims{"one", key, "acme", time.Now().Add(-time.Minute).Unix()})
	p := base64.RawURLEncoding.EncodeToString(b)
	expired := "re1." + p + "." + base64.RawURLEncoding.EncodeToString(a.enrollmentMAC(p))
	if got := request("PUT", "/enroll/v1/tenants/one", expired, body); got != 403 {
		t.Fatalf("expired: %d", got)
	}
	tenant, _ := r.Get("one")
	if tenant.PublicKey != key || tenant.QuotaBytes != 1234 || len(tenant.Hostnames) != 1 || tenant.Suspended {
		t.Fatalf("tenant corrupted: %+v", tenant)
	}
	// Changing identity revokes existing grants without waiting for expiration.
	tenant.PublicKey = testKey(t)
	if _, err := r.Upsert(tenant); err != nil {
		t.Fatal(err)
	}
	if got := request("PUT", "/enroll/v1/tenants/one", token, body); got != 403 {
		t.Fatalf("rotated identity: %d", got)
	}
}
