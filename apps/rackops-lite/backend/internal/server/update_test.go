package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v0.3.0", "0.2.0", true},
		{"0.3.0", "v0.3.0", false},
		{"v0.2.0", "0.3.0", false},
		{"v0.10.1", "0.9.9", true},
		{"v1.0.0-rc1", "0.9.0", true},
		{"v0.3.0", "0.3.0-dev", false},
		{"garbage", "0.1.0", false},
		{"v0.0.1", "dev", true},
	}
	for _, tt := range tests {
		if got := versionNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestHandleApplyUpdateRejectsNonAdmins(t *testing.T) {
	tests := []struct {
		name   string
		claims *jwtClaims
	}{
		{name: "missing identity"},
		{name: "member", claims: &jwtClaims{Role: "member"}},
		{name: "viewer", claims: &jwtClaims{Role: "viewer"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
			if tt.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), claimsKey, tt.claims))
			}
			response := httptest.NewRecorder()

			(&API{}).handleApplyUpdate(response, req)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}
