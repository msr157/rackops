package server

import (
	"encoding/json"
	"github.com/rackops/rackops/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEarlyAccessDeliverySurvivesFailureAndRestart(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.InstallID = "fixture-install"
	attempts := make(chan string, 4)
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		}
		if r.URL.Path != "/ingest/v1/leads" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Email != "lead@example.com" {
			w.WriteHeader(400)
			return
		}
		attempts <- body.ID
		if len(attempts) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	if r := do(a, "POST", "/api/v1/early-access", `{"name":"Example","email":"lead@example.com","industry":"Software"}`, nil); r.Code != 200 {
		t.Fatal("capture failed")
	}
	a.retryEarlyAccess()
	if a.Store.EarlyAccess["lead@example.com"].Forwarded {
		t.Fatal("failed delivery marked forwarded")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	a.retryEarlyAccess()
	a.retryEarlyAccess()
	if len(attempts) != 2 {
		t.Fatal("lost or repeated accepted delivery")
	}
	if first, second := <-attempts, <-attempts; first == "" || first != second {
		t.Fatal("retry changed identity")
	}
	if !a.Store.EarlyAccess["lead@example.com"].Forwarded {
		t.Fatal("accepted delivery not recorded")
	}
}
