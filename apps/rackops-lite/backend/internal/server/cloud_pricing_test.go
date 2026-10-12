package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
)

func TestComputeQuoteRemainsPartialAndScoped(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	spec := cloudFixture(t, "azure")
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "azure", Region: "eastus", AccountID: "account"}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "azure", Region: "eastus", AccountID: "account", Spec: spec, ExpiresAt: time.Now().Add(time.Hour), Status: "blocked"}}
	})
	calls := 0
	mode := "ok"
	a.azureComputeQuote = func(_ context.Context, region, size string) (cloud.ComputeQuote, error) {
		calls++
		if region != "eastus" || size != spec.Azure.Size {
			t.Error("quote selection changed")
		}
		if mode == "failure" {
			return cloud.ComputeQuote{}, errors.New("secret-in-provider-error")
		}
		if mode == "deleted" {
			a.Store.Mutate(func() { delete(a.Store.CloudConnections, "connection") })
		}
		return cloud.ComputeQuote{Provider: "azure", Region: region, Size: size, Currency: "USD", Monthly: 36.5, Hourly: 0.05, HoursPerMonth: 730}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	path := "/api/cloud-plans/plan/compute-estimate"
	r := do(a, "POST", path, "", headers)
	if r.Code != 200 {
		t.Fatal("quote failed", r.Code)
	}
	plan := a.Store.CloudReviewPlans["plan"]
	if plan.ComputeQuote == nil || plan.CanApprove || plan.MonthlyCost != nil || plan.TransferCost != nil || len(a.Store.CloudProvisioning) != 0 {
		t.Fatal("subtotal granted approval or became a total")
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.CloudReviewPlans["plan"].ComputeQuote == nil {
		t.Fatal("quote not persisted")
	}
	mode = "failure"
	r = do(a, "POST", path, "", headers)
	if r.Code != 422 || strings.Contains(r.Body.String(), "secret-in-provider-error") {
		t.Fatal("raw pricing failure leaked")
	}
	a.Store.Mutate(func() { plan.OrgID = "other" })
	if r := do(a, "POST", path, "", headers); r.Code != 404 || calls != 2 {
		t.Fatal("cross-org pricing allowed")
	}
	a.Store.Mutate(func() { plan.OrgID = org; plan.ExpiresAt = time.Now().Add(-time.Minute) })
	if r := do(a, "POST", path, "", headers); r.Code != 409 || calls != 2 {
		t.Fatal("expired review priced")
	}
	a.Store.Mutate(func() { plan.ExpiresAt = time.Now().Add(time.Hour) })
	mode = "deleted"
	r = do(a, "POST", path, "", headers)
	if r.Code != 409 {
		t.Fatal("changed connection accepted")
	}
}

func TestGCPComputeQuoteScopeAndIdentity(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"projectId":"fixture-project","serviceAccountJson":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	spec := cloudFixture(t, "gcp")
	spec.GCP.MachineType = "n2-standard-2"
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "gcp", Region: "us-central1", AccountID: "fixture-project", Principal: "principal", SealedCredentials: sealed}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "gcp", Region: "us-central1", AccountID: "fixture-project", Spec: spec, Inspection: cloud.MachineInspection{VCPUs: 2, MemoryMB: 8192}, ExpiresAt: time.Now().Add(time.Hour)}}
	})
	identityOK := true
	a.cloudIdentityCheck = func(_ context.Context, provider, region string, _ json.RawMessage) (cloud.Identity, error) {
		if provider != "gcp" || region != "us-central1" {
			t.Fatal("wrong identity selection")
		}
		if !identityOK {
			return cloud.Identity{}, errors.New("secret")
		}
		return cloud.Identity{AccountID: "fixture-project", Principal: "principal"}, nil
	}
	calls := 0
	a.gcpComputeQuote = func(_ context.Context, region, size string, c cloud.GCPCredentials, i cloud.MachineInspection) (cloud.ComputeQuote, error) {
		calls++
		if region != "us-central1" || size != "n2-standard-2" || c.ProjectID != "fixture-project" || i.MemoryMB != 8192 {
			t.Fatal("wrong saved selection")
		}
		return cloud.ComputeQuote{Provider: "gcp", Monthly: 67.16}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	path := "/api/cloud-plans/plan/compute-estimate"
	r := do(a, "POST", path, "", headers)
	if r.Code != 200 || strings.Contains(r.Body.String(), "fixture-secret") {
		t.Fatal("quote failed or leaked credentials", r.Code)
	}
	plan := a.Store.CloudReviewPlans["plan"]
	if plan.ComputeQuote == nil || plan.CanApprove || plan.MonthlyCost != nil || len(a.Store.CloudProvisioning) != 0 {
		t.Fatal("subtotal changed approval")
	}
	identityOK = false
	if r := do(a, "POST", path, "", headers); r.Code != 422 || calls != 1 {
		t.Fatal("invalid identity reached pricing")
	}
	a.Store.Mutate(func() { plan.OrgID = "other" })
	if r := do(a, "POST", path, "", headers); r.Code != 404 || calls != 1 {
		t.Fatal("cross-org pricing allowed")
	}
}

func TestAWSComputeQuoteUsesOriginalConnection(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"accessKeyId":"fixture-key","secretAccessKey":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	spec := cloudFixture(t, "aws")
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "aws", Region: "us-east-1", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "aws", Region: "us-east-1", AccountID: "account", Spec: spec, ExpiresAt: time.Now().Add(time.Hour)}}
	})
	identityOK := true
	a.awsIdentityCheck = func(context.Context, string, cloud.AWSCredentials) (cloud.Identity, error) {
		if !identityOK {
			return cloud.Identity{}, errors.New("secret")
		}
		return cloud.Identity{AccountID: "account", Principal: "principal"}, nil
	}
	calls := 0
	a.awsComputeQuote = func(_ context.Context, region, size string, c cloud.AWSCredentials) (cloud.ComputeQuote, error) {
		calls++
		if region != "us-east-1" || size != spec.AWS.InstanceType || c.AccessKeyID != "fixture-key" {
			t.Fatal("wrong connection")
		}
		return cloud.ComputeQuote{Provider: "aws", Monthly: 70.08}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	path := "/api/cloud-plans/plan/compute-estimate"
	response := do(a, "POST", path, "", headers)
	if response.Code != 200 || strings.Contains(response.Body.String(), "fixture-secret") {
		t.Fatal("quote failed or leaked credentials", response.Code)
	}
	plan := a.Store.CloudReviewPlans["plan"]
	if plan.ComputeQuote == nil || plan.CanApprove || plan.MonthlyCost != nil || len(a.Store.CloudProvisioning) != 0 {
		t.Fatal("subtotal changed launch state")
	}
	identityOK = false
	if r := do(a, "POST", path, "", headers); r.Code != 422 || calls != 1 {
		t.Fatal("invalid identity reached pricing")
	}
	a.Store.Mutate(func() { plan.OrgID = "other" })
	if r := do(a, "POST", path, "", headers); r.Code != 404 || calls != 1 {
		t.Fatal("cross-org pricing allowed")
	}
}
