package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestServiceResponseRedactsWithoutChangingStoredValues(t *testing.T) {
	s := &types.Service{ID: "app", GitToken: "git-test-secret", Env: []types.EnvVar{
		{Key: "PASSWORD", Value: "test-password", Secret: true},
		{Key: "UNMARKED", Value: "test-unmarked-value"},
	}}
	for _, response := range []any{s, *s, []*types.Service{s}} {
		rec := httptest.NewRecorder()
		writeJSON(rec, response)
		for _, secret := range []string{"git-test-secret", "test-password", "test-unmarked-value"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatalf("response contains stored credential: %s", rec.Body)
			}
		}
		if !strings.Contains(rec.Body.String(), `"redacted":true`) {
			t.Fatal("missing preservation marker")
		}
	}
	if s.GitToken != "git-test-secret" || s.Env[0].Value != "test-password" || s.Env[0].Redacted {
		t.Fatal("redaction mutated runtime configuration")
	}
}

func TestEnvironmentRedactionRoundTrip(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	svc := &types.Service{ID: "app", NodeID: "node", ProjectID: "p", Image: "app:1", Env: []types.EnvVar{{Key: "PASSWORD", Value: "preserve-me", Secret: true}}}
	a.Store.Mutate(func() {
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Services["app"] = svc
	})
	h := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	rec := do(a, "GET", "/api/services/app", "", h)
	var visible types.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &visible); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(ServiceUpdate{Env: &visible.Env})
	rec = do(a, "PATCH", "/api/services/app", string(body), h)
	if rec.Code != 200 || svc.Env[0].Value != "preserve-me" || svc.Env[0].Redacted {
		t.Fatalf("save lost stored value: %d", rec.Code)
	}
	rec = do(a, "PATCH", "/api/services/app", `{"env":[{"key":"RENAMED","value":"","redacted":true}]}`, h)
	if rec.Code != 400 || svc.Env[0].Key != "PASSWORD" {
		t.Fatal("rename without a replacement should be atomic and rejected")
	}
	rec = do(a, "PATCH", "/api/services/app", `{"env":[{"key":"PASSWORD","value":""}]}`, h)
	if rec.Code != 200 || svc.Env[0].Value != "" {
		t.Fatal("explicit empty value must clear stored value")
	}
}

func TestServiceNotificationsAreScopedAndContainNoConfiguration(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	svc := &types.Service{ID: "private-id", ProjectID: "p", Name: "private-name", GitToken: "private-token"}
	a.Store.Mutate(func() { a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org} })
	own, foreign := make(chan []byte, 1), make(chan []byte, 1)
	a.Hub.clients[own] = org
	a.Hub.clients[foreign] = "other-org"
	a.serviceChanged(svc)
	select {
	case msg := <-own:
		if string(msg) != "event: services_changed\ndata: {}\n\n" {
			t.Fatalf("unexpected notification: %s", msg)
		}
	default:
		t.Fatal("missing own-org notification")
	}
	select {
	case <-foreign:
		t.Fatal("cross-org notification")
	default:
	}
}
