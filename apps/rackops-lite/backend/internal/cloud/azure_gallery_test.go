package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
)

func TestGalleryImageMetadataChecks(t *testing.T) {
	const subscription = "22222222-2222-4222-8222-222222222222"
	const definition = "/subscriptions/" + subscription + "/resourceGroups/fixture/providers/Microsoft.Compute/galleries/fixture/images/linux"
	const version = definition + "/versions/1.2.3"
	if !validAzureGalleryVersion(version, subscription) || validAzureGalleryVersion(definition+"/versions/latest", subscription) || validAzureGalleryVersion(version, "other") {
		t.Fatal("gallery reference validation failed")
	}
	for _, mode := range []string{"ready", "architecture", "windows", "replication", "disk", "identity", "permission"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			transport := cloudTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Fatal("image inspection mutated resources")
				}
				if mode == "permission" {
					resp := jsonResponse(r, `{"error":{"code":"AuthorizationFailed","message":"denied"}}`)
					resp.StatusCode = 403
					return resp, nil
				}
				var result map[string]any
				switch r.URL.Path {
				case definition:
					props := map[string]any{"architecture": "x64", "osType": "Linux", "osState": "Generalized", "provisioningState": "Succeeded"}
					if mode == "architecture" {
						props["architecture"] = "Arm64"
					}
					if mode == "windows" {
						props["osType"] = "Windows"
					}
					result = map[string]any{"id": definition, "properties": props}
				case version:
					if r.URL.Query().Get("$expand") != "ReplicationStatus" {
						t.Fatal("missing replication evidence request")
					}
					state := "Completed"
					if mode == "replication" {
						state = "InProgress"
					}
					disk := 20
					if mode == "disk" {
						disk = 100
					}
					result = map[string]any{"id": version, "properties": map[string]any{"provisioningState": "Succeeded", "storageProfile": map[string]any{"osDiskImage": map[string]any{"sizeInGB": disk}}, "replicationStatus": map[string]any{"summary": []any{map[string]any{"region": "eastus", "state": state}}}}}
					if mode == "identity" {
						result["id"] = definition + "/versions/2.0.0"
					}
				default:
					t.Fatal("unscoped image request", r.URL.Path)
				}
				raw, _ := json.Marshal(result)
				return jsonResponse(r, string(raw)), nil
			})
			options := &arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: transport}, DisableRPRegistration: true}
			images, err := armcompute.NewGalleryImagesClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := armcompute.NewGalleryImageVersionsClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			p := &Azure{region: "eastus", galleryImages: images, galleryVersions: versions}
			out := MachineInspection{}
			err = p.inspectGalleryImage(context.Background(), AzureMachineSpec{ImageID: version, DiskGB: 30}, "amd64", &out)
			if mode == "identity" || mode == "permission" {
				if err == nil {
					t.Fatal("unsafe metadata accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || (len(out.Blockers) == 0) != (mode == "ready") {
				t.Fatalf("unexpected findings: %+v", out.Blockers)
			}
			for _, b := range out.Blockers {
				if strings.Contains(b.Code, "architecture_unverified") {
					t.Fatal("gallery architecture treated as absent managed-image field")
				}
			}
		})
	}
}
