package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
	"testing"
)

func TestCloudConnectionsEncryptAndIsolateCredentials(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	calls := 0
	a.awsIdentityCheck = func(_ context.Context, _ string, c cloud.AWSCredentials) (cloud.Identity, error) {
		calls++
		if c.SecretAccessKey != "fixture-secret" {
			t.Error("credentials lost")
		}
		return cloud.Identity{AccountID: "123456789012", Principal: "fixture-principal"}, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	r := do(a, "POST", "/api/cloud-connections", `{"name":"Fixture","provider":"aws","region":"us-east-1","credentials":{"accessKeyId":"fixture-access","secretAccessKey":"fixture-secret"}}`, headers)
	if r.Code != 200 {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var connection CloudConnection
	if json.Unmarshal(r.Body.Bytes(), &connection) != nil || connection.ID == "" {
		t.Fatal("missing connection")
	}
	if len(connection.SealedCredentials) > 0 || bytes.Contains(r.Body.Bytes(), []byte("fixture-secret")) {
		t.Fatal("credential returned")
	}
	// Even a plaintext JSON export contains only nested ciphertext.
	exported, err := json.Marshal(a.Store)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(exported, []byte("fixture-secret")) || bytes.Contains(exported, []byte("fixture-access")) {
		t.Fatal("plaintext credentials in state export")
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
	if r := do(a, "POST", "/api/cloud-connections/"+connection.ID+"/validate", "", headers); r.Code != 200 {
		t.Fatalf("restart validation: %d %s", r.Code, r.Body)
	}
	a.Store.Mutate(func() { a.Store.CloudConnections[connection.ID].OrgID = "other-org" })
	if r := do(a, "GET", "/api/cloud-connections", "", headers); r.Body.String() != "[]\n" {
		t.Fatal("cross-org listing")
	}
	for _, method := range []string{"POST", "DELETE"} {
		path := "/api/cloud-connections/" + connection.ID
		if method == "POST" {
			path += "/validate"
		}
		if r := do(a, method, path, "", headers); r.Code != 404 {
			t.Fatal("cross-org access")
		}
	}
	if calls != 2 {
		t.Fatal("cross-org request reached cloud")
	}
}

func TestCloudValidationDoesNotExposeProviderErrors(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.awsIdentityCheck = func(context.Context, string, cloud.AWSCredentials) (cloud.Identity, error) {
		return cloud.Identity{}, errors.New("fixture-secret in provider error")
	}
	r := do(a, "POST", "/api/cloud-connections", `{"name":"Fixture","provider":"aws","region":"us-east-1","credentials":{"accessKeyId":"fixture-access","secretAccessKey":"fixture-secret"}}`, map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")})
	if r.Code != 422 || bytes.Contains(r.Body.Bytes(), []byte("fixture-secret")) || len(a.Store.CloudConnections) > 0 {
		t.Fatal("failed validation leaked or saved credentials")
	}
}

func TestCloudConnectionsRequireMachineManagement(t *testing.T) {
	a, ids := testAPI(t, types.RoleOwner, types.RoleMember)
	orgID := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		org := a.Store.Orgs[orgID]
		for i := range org.Members {
			if org.Members[i].UserID == ids[types.RoleMember] {
				org.Members[i].Permissions = types.OrgPermissions{}
			}
		}
	})
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "member@test.local")}
	for _, req := range []struct{ method, path string }{{"GET", "/api/cloud-connections"}, {"POST", "/api/cloud-connections"}, {"POST", "/api/cloud-connections/missing/validate"}, {"DELETE", "/api/cloud-connections/missing"}, {"GET", "/api/cloud-operations/missing"}, {"POST", "/api/cloud-connections/missing/machine-preflight"}} {
		if r := do(a, req.method, req.path, `{}`, headers); r.Code != 403 {
			t.Fatalf("missing capability: %d", r.Code)
		}
	}
}

func TestAzureAndGoogleConnectionsRemainEncryptedAcrossRestart(t *testing.T) {
	for _, provider := range []string{"azure", "gcp"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			calls := 0
			a.cloudIdentityCheck = func(_ context.Context, name, region string, raw json.RawMessage) (cloud.Identity, error) {
				calls++
				if name != provider || !bytes.Contains(raw, []byte("fixture-secret")) {
					t.Error("credential dispatch mismatch")
				}
				return cloud.Identity{AccountID: "fixture-account", Principal: "fixture-principal"}, nil
			}
			region := "eastus"
			credentials := map[string]string{"tenantId": "11111111-1111-4111-8111-111111111111", "subscriptionId": "22222222-2222-4222-8222-222222222222", "clientId": "33333333-3333-4333-8333-333333333333", "clientSecret": "fixture-secret"}
			if provider == "gcp" {
				region = "us-central1"
				credentials = map[string]string{"projectId": "fixture-project", "serviceAccountJson": `{"type":"service_account","client_email":"fixture@fixture-project.iam.gserviceaccount.com","private_key":"fixture-secret","token_uri":"https://oauth2.googleapis.com/token"}`}
			}
			body, _ := json.Marshal(map[string]any{"name": "Fixture", "provider": provider, "region": region, "credentials": credentials})
			headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
			r := do(a, "POST", "/api/cloud-connections", string(body), headers)
			var connection CloudConnection
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &connection) != nil {
				t.Fatalf("connection: %d %s", r.Code, r.Body)
			}
			exported, _ := json.Marshal(a.Store)
			if bytes.Contains(exported, []byte("fixture-secret")) || bytes.Contains(r.Body.Bytes(), []byte("fixture-secret")) {
				t.Fatal("credential exposed")
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
			if r := do(a, "POST", "/api/cloud-connections/"+connection.ID+"/validate", "", headers); r.Code != 200 || calls != 2 {
				t.Fatal("saved provider identity could not be revalidated")
			}
		})
	}
}
