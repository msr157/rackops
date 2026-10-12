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

func TestGCPDiskEstimateIdentityAndScope(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"projectId":"fixture-project","serviceAccountJson":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "gcp", Region: "us-central1", AccountID: "fixture-project", Principal: "principal", SealedCredentials: sealed}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "gcp", Region: "us-central1", AccountID: "fixture-project", Spec: cloudFixture(t, "gcp"), ExpiresAt: time.Now().Add(time.Hour)}}
	})
	deny := false
	a.cloudIdentityCheck = func(context.Context, string, string, json.RawMessage) (cloud.Identity, error) {
		if deny {
			return cloud.Identity{}, errors.New("fixture-secret")
		}
		return cloud.Identity{AccountID: "fixture-project", Principal: "principal"}, nil
	}
	calls := 0
	a.gcpDiskQuote = func(_ context.Context, region string, size int64, cred cloud.GCPCredentials) (cloud.DiskQuote, error) {
		calls++
		if cred.ProjectID != "fixture-project" || region != "us-central1" || size != a.Store.CloudReviewPlans["plan"].Spec.GCP.DiskGB {
			t.Fatal("wrong saved selection")
		}
		return cloud.DiskQuote{Provider: "gcp", Monthly: 10, DiskType: "pd-balanced (zonal)"}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	path := "/api/cloud-plans/plan/disk-estimate"
	if r := do(a, "POST", path, "", headers); r.Code != 200 || strings.Contains(r.Body.String(), "fixture-secret") {
		t.Fatalf("quote %d %s", r.Code, r.Body)
	}
	if a.Store.CloudReviewPlans["plan"].DiskQuote == nil {
		t.Fatal("quote not saved")
	}
	deny = true
	if r := do(a, "POST", path, "", headers); r.Code != 422 || strings.Contains(r.Body.String(), "fixture-secret") {
		t.Fatalf("identity denial %d %s", r.Code, r.Body)
	}
	if calls != 1 {
		t.Fatal("unvalidated identity queried catalog")
	}
	a.Store.Mutate(func() { a.Store.CloudReviewPlans["plan"].OrgID = "foreign" })
	if r := do(a, "POST", path, "", headers); r.Code != 404 {
		t.Fatal("foreign plan disclosed")
	}
}

func TestAzureDiskEstimateScopeAndPersistence(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	spec := cloudFixture(t, "azure")
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"fixtureSecret":"never-return-this"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "azure", Region: "eastus", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "azure", Region: "eastus", AccountID: "account", Spec: spec, ExpiresAt: time.Now().Add(time.Hour), Status: "blocked"}}
	})
	a.azureDiskQuote = func(_ context.Context, region string, diskGB int64) (cloud.DiskQuote, error) {
		if region != "eastus" || diskGB != 30 {
			t.Fatal("wrong disk parameters")
		}
		return cloud.DiskQuote{Provider: "azure", Region: region, DiskType: "StandardSSD_LRS E4", DiskGB: diskGB, Currency: "USD", Monthly: 3.84, PerGBMonth: 0.12, MeterID: "disk-meter", Source: "https://prices.azure.com", RetrievedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute), Exclusions: []string{"test"}}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	r := do(a, "POST", "/api/cloud-plans/plan/disk-estimate", "", headers)
	if r.Code != 200 || strings.Contains(r.Body.String(), "never-return-this") {
		t.Fatalf("disk estimate: %d %s", r.Code, r.Body)
	}
	var quote cloud.DiskQuote
	json.Unmarshal(r.Body.Bytes(), &quote)
	if quote.Monthly != 3.84 || quote.DiskType != "StandardSSD_LRS E4" || quote.MeterID != "disk-meter" {
		t.Fatalf("unexpected disk quote: %+v", quote)
	}
	plan := a.Store.CloudReviewPlans["plan"]
	if plan.DiskQuote == nil || plan.DiskQuote.Monthly != 3.84 {
		t.Fatal("disk quote not persisted")
	}
	a.Store.Close()
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	if restored.CloudReviewPlans["plan"].DiskQuote == nil {
		t.Fatal("disk quote not restored")
	}
	// Expired review rejects estimate
	a.Store.Mutate(func() { a.Store.CloudReviewPlans["plan"].ExpiresAt = time.Now().Add(-time.Minute) })
	if r := do(a, "POST", "/api/cloud-plans/plan/disk-estimate", "", headers); r.Code != 409 {
		t.Fatal("expired review accepted disk estimate")
	}
	// Missing plan returns 404
	if r := do(a, "POST", "/api/cloud-plans/missing/disk-estimate", "", headers); r.Code != 404 {
		t.Fatal("missing plan accepted")
	}
	// Unauthenticated request rejected
	if r := do(a, "POST", "/api/cloud-plans/plan/disk-estimate", "", nil); r.Code == 200 {
		t.Fatal("unauthenticated disk estimate accepted")
	}
}

func TestAWSDiskEstimateScopeAndPersistence(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	spec := cloudFixture(t, "aws")
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"accessKeyId":"fixture-access","secretAccessKey":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "aws", Region: "us-east-1", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
		a.Store.CloudReviewPlans = map[string]*CloudReviewPlan{"plan": {ID: "plan", OrgID: org, ConnectionID: "connection", Provider: "aws", Region: "us-east-1", AccountID: "account", Spec: spec, ExpiresAt: time.Now().Add(time.Hour), Status: "blocked"}}
	})
	a.awsIdentityCheck = func(_ context.Context, _ string, _ cloud.AWSCredentials) (cloud.Identity, error) {
		return cloud.Identity{AccountID: "account", Principal: "principal"}, nil
	}
	a.awsDiskQuote = func(_ context.Context, region string, diskGB int64, cred cloud.AWSCredentials) (cloud.DiskQuote, error) {
		if region != "us-east-1" || diskGB != 20 || cred.SecretAccessKey != "fixture-secret" {
			t.Fatal("wrong disk parameters")
		}
		return cloud.DiskQuote{Provider: "aws", Region: region, DiskType: "gp3", DiskGB: diskGB, Currency: "USD", Monthly: 1.6, PerGBMonth: 0.08, MeterID: "gp3-rate", Source: "https://pricing.us-east-1.amazonaws.com", RetrievedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute), Exclusions: []string{"test"}}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	r := do(a, "POST", "/api/cloud-plans/plan/disk-estimate", "", headers)
	if r.Code != 200 || strings.Contains(r.Body.String(), "fixture-secret") {
		t.Fatalf("disk estimate: %d %s", r.Code, r.Body)
	}
	plan := a.Store.CloudReviewPlans["plan"]
	if plan.DiskQuote == nil || plan.DiskQuote.Monthly != 1.6 || plan.DiskQuote.DiskType != "gp3" {
		t.Fatal("AWS disk quote not persisted")
	}
	// Cross-org access denied
	a.Store.Mutate(func() { a.Store.CloudReviewPlans["plan"].OrgID = "foreign" })
	if r := do(a, "POST", "/api/cloud-plans/plan/disk-estimate", "", headers); r.Code != 404 {
		t.Fatal("cross-org disk estimate disclosed")
	}
}
