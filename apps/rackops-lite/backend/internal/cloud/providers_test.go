package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"golang.org/x/crypto/ssh"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type cloudTransport func(*http.Request) (*http.Response, error)

func (f cloudTransport) Do(r *http.Request) (*http.Response, error)        { return f(r) }
func (f cloudTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type azureTestCredential struct{}

func (azureTestCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "fixture-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}
func jsonResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestAzureSDKPreventsOverwriteAndPreservesDisk(t *testing.T) {
	key, _ := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, 32)).Public())
	const subscription = "22222222-2222-4222-8222-222222222222"
	prefix := "/subscriptions/" + subscription + "/resourceGroups/fixture/providers/"
	spec := AzureMachineSpec{OperationID: "11111111-1111-4111-8111-111111111111", SubscriptionID: subscription, ResourceGroup: "fixture", Size: "Standard_B2s", ImageID: prefix + "Microsoft.Compute/images/fixture", NetworkInterfaceID: prefix + "Microsoft.Network/networkInterfaces/fixture", SSHPublicKey: string(ssh.MarshalAuthorizedKey(key)), DiskGB: 30}
	calls := 0
	transport := cloudTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "PUT" || r.Header.Get("If-None-Match") != "*" {
			t.Error("VM creation may overwrite existing machine")
		}
		var vm armcompute.VirtualMachine
		if err := json.NewDecoder(r.Body).Decode(&vm); err != nil {
			t.Fatal(err)
		}
		if vm.Properties.OSProfile.CustomData == nil {
			t.Fatal("Azure bootstrap missing")
		}
		decoded, err := base64.StdEncoding.DecodeString(*vm.Properties.OSProfile.CustomData)
		if err != nil || string(decoded) != "#!/bin/sh\nexit 0\n" {
			t.Fatal("Azure bootstrap incorrectly encoded")
		}
		if vm.Properties.StorageProfile.OSDisk.DeleteOption == nil || *vm.Properties.StorageProfile.OSDisk.DeleteOption != armcompute.DiskDeleteOptionTypesDetach || !*vm.Properties.OSProfile.LinuxConfiguration.DisablePasswordAuthentication {
			t.Error("unsafe disk/password defaults")
		}
		return jsonResponse(r, `{"id":"`+prefix+`Microsoft.Compute/virtualMachines/rackops-`+spec.OperationID+`","location":"eastus","properties":{"provisioningState":"Succeeded"}}`), nil
	})
	client, err := armcompute.NewVirtualMachinesClient(subscription, azureTestCredential{}, &arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: transport}, DisableRPRegistration: true})
	if err != nil {
		t.Fatal(err)
	}
	p := &Azure{region: "eastus", credentials: AzureCredentials{SubscriptionID: subscription}, machines: client, bootstrap: "#!/bin/sh\nexit 0\n"}
	receipt, err := p.Start(context.Background(), MachineSpec{Azure: &spec})
	if err != nil || receipt.ResourceID == "" || calls != 1 {
		t.Fatalf("SDK create: %v calls=%d", err, calls)
	}
}

func TestGCPSDKUsesRequestIDAndPrivateRetainedStorage(t *testing.T) {
	spec := GCPMachineSpec{OperationID: "11111111-1111-4111-8111-111111111111", ProjectID: "fixture-project", Zone: "us-central1-a", MachineType: "e2-small", Image: "projects/ubuntu-os-cloud/global/images/fixture-image", Subnetwork: "projects/fixture-project/regions/us-central1/subnetworks/fixture", DiskGB: 20}
	transport := cloudTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Query().Get("requestId") != spec.OperationID {
			t.Error("missing stable request ID")
		}
		var instance compute.Instance
		if err := json.NewDecoder(r.Body).Decode(&instance); err != nil {
			t.Fatal(err)
		}
		if instance.Metadata == nil || len(instance.Metadata.Items) != 1 || instance.Metadata.Items[0].Key != "startup-script" || instance.Metadata.Items[0].Value == nil || *instance.Metadata.Items[0].Value != "#!/bin/sh\nexit 0\n" {
			t.Fatal("Google bootstrap missing")
		}
		if len(instance.Disks) != 1 || instance.Disks[0].AutoDelete || len(instance.NetworkInterfaces) != 1 || len(instance.NetworkInterfaces[0].AccessConfigs) != 0 || len(instance.ServiceAccounts) != 0 {
			t.Error("unsafe instance defaults")
		}
		return jsonResponse(r, `{"name":"operation-fixture","status":"PENDING","targetLink":"projects/fixture-project/zones/us-central1-a/instances/fixture"}`), nil
	})
	client, err := compute.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	p := &GCP{region: "us-central1", project: "fixture-project", compute: client, bootstrap: "#!/bin/sh\nexit 0\n"}
	receipt, err := p.Start(context.Background(), MachineSpec{GCP: &spec})
	if err != nil || receipt.OperationID != "operation-fixture" {
		t.Fatalf("SDK insert: %v", err)
	}
}

func TestGoogleCredentialsRejectExternalTokenSources(t *testing.T) {
	for _, raw := range []string{
		`{"type":"external_account","credential_source":{"url":"http://localhost/private"}}`,
		`{"type":"service_account","client_email":"fixture@fixture-project.iam.gserviceaccount.com","private_key":"fixture","token_uri":"https://attacker.invalid/token"}`,
	} {
		if _, err := gcpJWT(GCPCredentials{ProjectID: "fixture-project", ServiceAccountJSON: raw}); err == nil {
			t.Fatal("untrusted credential source accepted")
		}
	}
}
