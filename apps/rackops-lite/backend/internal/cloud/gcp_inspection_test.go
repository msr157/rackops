package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

func TestGoogleInspectionReadsOnlySelectedResources(t *testing.T) {
	const root = "https://www.googleapis.com/compute/v1/projects/fixture-project/"
	spec := GCPMachineSpec{OperationID: "11111111-1111-4111-8111-111111111111", ProjectID: "fixture-project", Zone: "us-central1-a", MachineType: "e2-small", Image: "projects/fixture-project/global/images/fixture-image", Subnetwork: "projects/fixture-project/regions/us-central1/subnetworks/fixture", DiskGB: 30}
	for _, mode := range []string{"compatible", "architecture", "windows", "disk", "zone", "accelerator", "subnet", "identity", "permission"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			transport := cloudTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Fatal("inventory attempted a mutation")
				}
				var result any
				switch {
				case strings.HasSuffix(r.URL.Path, "/global/images/fixture-image"):
					img := &compute.Image{Name: "fixture-image", SelfLink: root + "global/images/fixture-image", Architecture: "X86_64", Status: "READY", DiskSizeGb: 20}
					switch mode {
					case "architecture":
						img.Architecture = "ARM64"
					case "windows":
						img.GuestOsFeatures = []*compute.GuestOsFeature{{Type: "WINDOWS"}}
					case "disk":
						img.DiskSizeGb = 50
					case "identity":
						img.SelfLink = "https://untrusted.test/compute/v1/projects/fixture-project/global/images/fixture-image"
					case "permission":
						response := jsonResponse(r, `{"error":{"code":403,"message":"permission denied"}}`)
						response.StatusCode = 403
						return response, nil
					}
					result = img
				case strings.HasSuffix(r.URL.Path, "/zones/us-central1-a"):
					zone := &compute.Zone{Name: "us-central1-a", Region: root + "regions/us-central1", Status: "UP"}
					if mode == "zone" {
						zone.Status = "DOWN"
					}
					result = zone
				case strings.HasSuffix(r.URL.Path, "/zones/us-central1-a/machineTypes/e2-small"):
					size := &compute.MachineType{Name: "e2-small", SelfLink: root + "zones/us-central1-a/machineTypes/e2-small", Architecture: "X86_64", GuestCpus: 2, MemoryMb: 2048}
					if mode == "accelerator" {
						size.Accelerators = []*compute.MachineTypeAccelerators{{GuestAcceleratorCount: 1}}
					}
					result = size
				case strings.HasSuffix(r.URL.Path, "/regions/us-central1/subnetworks/fixture"):
					subnet := &compute.Subnetwork{Name: "fixture", SelfLink: root + "regions/us-central1/subnetworks/fixture", Region: root + "regions/us-central1", Network: root + "global/networks/fixture", IpCidrRange: "10.0.0.0/24", Purpose: "PRIVATE"}
					if mode == "subnet" {
						subnet.Purpose = "REGIONAL_MANAGED_PROXY"
					}
					result = subnet
				default:
					t.Fatalf("unexpected inventory request: %s", r.URL.Path)
				}
				raw, _ := json.Marshal(result)
				return jsonResponse(r, string(raw)), nil
			})
			client, err := compute.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			p := &GCP{region: "us-central1", project: "fixture-project", compute: client}
			review, err := p.InspectMachine(context.Background(), spec, "amd64")
			if mode == "identity" || mode == "permission" {
				if err == nil || calls != 1 {
					t.Fatal("failed metadata read was accepted")
				}
				return
			}
			if err != nil || calls != 4 {
				t.Fatalf("inspection: %v calls=%d", err, calls)
			}
			if review.ResourceCompatible != (mode == "compatible") || review.CanLaunch || review.MonthlyCostEstimate != nil {
				t.Fatalf("incorrect compatibility/launch status: %+v", review)
			}
			before := calls
			other := spec
			other.ProjectID = "another-project"
			if _, err := p.InspectMachine(context.Background(), other, "amd64"); err == nil || calls != before {
				t.Fatal("cross-project inspection reached Google")
			}
		})
	}
}
