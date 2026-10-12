package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
)

func TestSavedCloudReviewScopePersistenceAndImmutability(t *testing.T) {
	for _, provider := range []string{"aws", "azure", "gcp"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			org := a.Store.DefaultOrgID()
			sealed, err := encryptState(a.Store.stateKey, []byte(`{"fixtureSecret":"never-return-this"}`))
			if err != nil {
				t.Fatal(err)
			}
			a.Store.Mutate(func() {
				a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: provider, Region: "region", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
			})
			a.cloudIdentityCheck = func(context.Context, string, string, json.RawMessage) (cloud.Identity, error) {
				return cloud.Identity{AccountID: "account", Principal: "principal"}, nil
			}
			a.cloudMachineCheck = func(_ context.Context, name, region string, _ json.RawMessage, spec cloud.MachineSpec, arch string) (cloud.MachineInspection, error) {
				return cloud.MachineInspection{Provider: name, Region: region, SpecFingerprint: spec.Fingerprint(), Architecture: arch, CanLaunch: true, CheckedAt: time.Now()}, nil
			}
			headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
			spec := cloudFixture(t, provider)
			body, _ := json.Marshal(map[string]any{"spec": spec, "architecture": "amd64"})
			path := "/api/cloud-connections/connection/plans"
			response := do(a, "POST", path, string(body), headers)
			if response.Code != 200 {
				t.Fatalf("save: %d %s", response.Code, response.Body)
			}
			var plan CloudReviewPlan
			if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.CanApprove || plan.Inspection.CanLaunch || plan.MonthlyCost != nil || plan.TransferCost != nil || plan.Status != "blocked" || strings.Contains(response.Body.String(), "never-return-this") {
				t.Fatal("review claimed launch/cost or exposed secret")
			}
			response = do(a, "POST", path, string(body), headers)
			var replay CloudReviewPlan
			json.Unmarshal(response.Body.Bytes(), &replay)
			if response.Code != 200 || !replay.CreatedAt.Equal(plan.CreatedAt) || len(a.Store.CloudReviewPlans) != 1 {
				t.Fatal("retry replaced immutable review")
			}
			altered, _ := json.Marshal(map[string]any{"spec": spec, "architecture": "arm64"})
			if r := do(a, "POST", path, string(altered), headers); r.Code != 409 {
				t.Fatal("ID reused with changed plan")
			}
			if len(a.Store.CloudProvisioning) != 0 || len(a.Store.NodeEnrollments) != 0 {
				t.Fatal("review created execution state")
			}
			a.Store.Close()
			restored, err := NewStore(a.Store.dataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			a.Store = restored
			if restored.CloudReviewPlans[plan.ID] == nil || restored.CloudReviewPlans[plan.ID].Spec.Fingerprint() != spec.Fingerprint() {
				t.Fatal("review not restored")
			}
			if r := do(a, "GET", "/api/cloud-plans/"+plan.ID, "", headers); r.Code != 200 {
				t.Fatal("saved review inaccessible")
			}
			a.Store.Mutate(func() { a.Store.CloudReviewPlans[plan.ID].ExpiresAt = time.Now().Add(-time.Minute) })
			r := do(a, "GET", "/api/cloud-plans/"+plan.ID, "", headers)
			if !strings.Contains(r.Body.String(), `"status":"expired"`) {
				t.Fatal("expired review not identified")
			}
			a.Store.Mutate(func() { a.Store.CloudConnections["connection"].AccountID = "changed" })
			r = do(a, "GET", "/api/cloud-plans/"+plan.ID, "", headers)
			if !strings.Contains(r.Body.String(), `"status":"connection_unavailable"`) {
				t.Fatal("changed account not identified")
			}
			a.Store.Mutate(func() { a.Store.CloudReviewPlans[plan.ID].OrgID = "foreign" })
			if r := do(a, "GET", "/api/cloud-plans/"+plan.ID, "", headers); r.Code != 404 {
				t.Fatal("cross-org review disclosed")
			}
			if r := do(a, "GET", "/api/cloud-plans", "", headers); r.Code != 200 || strings.TrimSpace(r.Body.String()) != "[]" {
				t.Fatal("cross-org list disclosed")
			}
			if r := do(a, "GET", "/api/cloud-plans", "", nil); r.Code == 200 {
				t.Fatal("anonymous review access")
			}
		})
	}
}
