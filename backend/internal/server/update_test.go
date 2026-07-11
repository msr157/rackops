package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRestartRejectsNonAdmins(t *testing.T) {
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
			req := httptest.NewRequest(http.MethodPost, "/api/restart", nil)
			if tt.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), claimsKey, tt.claims))
			}
			response := httptest.NewRecorder()

			(&API{}).handleRestart(response, req)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}
