package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
)

func TestMachineInspectionScopeIdentityAndSecretProtection(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"accessKeyId":"fixture-access","secretAccessKey":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "aws", Region: "us-east-1", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
	})
	identityCalls, inventoryCalls := 0, 0
	identity := cloud.Identity{AccountID: "account", Principal: "principal"}
	a.awsIdentityCheck = func(_ context.Context, region string, credentials cloud.AWSCredentials) (cloud.Identity, error) {
		identityCalls++
		if region != "us-east-1" || credentials.SecretAccessKey != "fixture-secret" {
			t.Fatal("incorrect connection used")
		}
		return identity, nil
	}
	var inspectErr error
	a.awsMachineCheck = func(ctx context.Context, region string, credentials cloud.AWSCredentials, spec cloud.AWSMachineSpec, arch string) (cloud.MachineInspection, error) {
		inventoryCalls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing provider deadline")
		}
		if arch != "amd64" || spec.OperationID != cloudFixture(t, "aws").ID() || region != "us-east-1" || credentials.SecretAccessKey != "fixture-secret" {
			t.Fatal("inspection changed requested inputs")
		}
		return cloud.MachineInspection{ResourceCompatible: true, CanLaunch: true}, inspectErr
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	body, _ := json.Marshal(map[string]any{"spec": cloudFixture(t, "aws"), "architecture": "amd64"})
	path := "/api/cloud-connections/connection/machine-preflight"
	r := do(a, "POST", path, string(body), headers)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("inspection: %d %s", r.Code, r.Body)
	}
	if strings.Contains(r.Body.String(), "fixture-secret") || strings.Contains(r.Body.String(), `"canLaunch":true`) {
		t.Fatal("leaked secret or granted launch approval")
	}
	if len(a.Store.CloudProvisioning) != 0 || len(a.Store.NodeEnrollments) != 0 {
		t.Fatal("inspection created operation state")
	}
	inspectErr = errors.New("fixture-secret in SDK failure")
	r = do(a, "POST", path, string(body), headers)
	if r.Code != 422 || strings.Contains(r.Body.String(), "fixture-secret") {
		t.Fatal("provider error leaked")
	}
	identity.AccountID = "changed-account"
	if r := do(a, "POST", path, string(body), headers); r.Code != 409 || inventoryCalls != 2 {
		t.Fatal("changed identity reached resource inspection")
	}
	a.Store.Mutate(func() { a.Store.CloudConnections["connection"].OrgID = "another-org" })
	if r := do(a, "POST", path, string(body), headers); r.Code != 404 || identityCalls != 3 {
		t.Fatal("cross-org inspection reached provider")
	}
}

func TestMachineInspectionRejectsUnsupportedAndInvalidRequests(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "unsupported"}}
	})
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	path := "/api/cloud-connections/connection/machine-preflight"
	if r := do(a, "POST", path, `{}`, headers); r.Code != 501 {
		t.Fatal("unsupported inspection claimed success")
	}
	a.Store.Mutate(func() { a.Store.CloudConnections["connection"].Provider = "aws" })
	if r := do(a, "POST", path, `{}`, headers); r.Code != 400 {
		t.Fatal("empty spec accepted")
	}
	if r := do(a, "POST", path, `{}`, nil); r.Code == 200 {
		t.Fatal("unauthenticated inspection accepted")
	}
}

func TestAzureAndGoogleInspectionUseScopedStoredConnection(t *testing.T) {
	for _, provider := range []string{"azure", "gcp"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			org := a.Store.DefaultOrgID()
			sealed, err := encryptState(a.Store.stateKey, []byte(`{"fixture":"private-credential"}`))
			if err != nil {
				t.Fatal(err)
			}
			a.Store.Mutate(func() {
				a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: provider, Region: "region", AccountID: "account", Principal: "principal", SealedCredentials: sealed}}
			})
			a.cloudIdentityCheck = func(_ context.Context, name, region string, raw json.RawMessage) (cloud.Identity, error) {
				if name != provider || region != "region" || !strings.Contains(string(raw), "private-credential") {
					t.Fatal("wrong cloud credentials used")
				}
				return cloud.Identity{AccountID: "account", Principal: "principal"}, nil
			}
			calls := 0
			remove := false
			a.cloudMachineCheck = func(_ context.Context, name, region string, raw json.RawMessage, spec cloud.MachineSpec, architecture string) (cloud.MachineInspection, error) {
				calls++
				if name != provider || region != "region" || spec.Validate(provider) != nil || architecture != "amd64" || !strings.Contains(string(raw), "private-credential") {
					t.Fatal("inspection input mismatch")
				}
				if remove {
					a.Store.Mutate(func() { delete(a.Store.CloudConnections, "connection") })
				}
				return cloud.MachineInspection{Provider: provider, CanLaunch: true}, nil
			}
			body, _ := json.Marshal(map[string]any{"spec": cloudFixture(t, provider), "architecture": "amd64"})
			headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
			path := "/api/cloud-connections/connection/machine-preflight"
			r := do(a, "POST", path, string(body), headers)
			if r.Code != 200 || strings.Contains(r.Body.String(), "private-credential") || strings.Contains(r.Body.String(), `"canLaunch":true`) {
				t.Fatalf("unsafe inspection response %d %s", r.Code, r.Body)
			}
			remove = true
			if r := do(a, "POST", path, string(body), headers); r.Code != 404 {
				t.Fatal("removed connection still returned metadata")
			}
			if r := do(a, "POST", path, string(body), headers); r.Code != 404 || calls != 2 {
				t.Fatal("missing connection reached provider")
			}
		})
	}
}
