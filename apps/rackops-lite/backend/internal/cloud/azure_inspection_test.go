package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v7"
	"golang.org/x/crypto/ssh"
)

func TestAzureInspectionRejectsUnsafeImageAndNICMetadata(t *testing.T) {
	const subscription = "22222222-2222-4222-8222-222222222222"
	const prefix = "/subscriptions/" + subscription + "/resourceGroups/fixture/providers/"
	key, _ := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, 32)).Public())
	spec := AzureMachineSpec{OperationID: "11111111-1111-4111-8111-111111111111", SubscriptionID: subscription, ResourceGroup: "fixture", Size: "Standard_B2s", ImageID: prefix + "Microsoft.Compute/images/fixture", NetworkInterfaceID: prefix + "Microsoft.Network/networkInterfaces/fixture", SSHPublicKey: string(ssh.MarshalAuthorizedKey(key)), DiskGB: 30}
	for _, mode := range []string{"baseline", "windows", "disk", "size-restricted", "attached-nic", "public-ip", "foreign-subnet", "wrong-image", "permission"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			transport := cloudTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Fatal("inspection attempted a mutation")
				}
				var result any
				switch {
				case strings.HasSuffix(r.URL.Path, "/images/fixture"):
					img := armcompute.Image{ID: to.Ptr(spec.ImageID), Location: to.Ptr("eastus"), Properties: &armcompute.ImageProperties{ProvisioningState: to.Ptr("Succeeded"), StorageProfile: &armcompute.ImageStorageProfile{OSDisk: &armcompute.ImageOSDisk{OSType: to.Ptr(armcompute.OperatingSystemTypesLinux), OSState: to.Ptr(armcompute.OperatingSystemStateTypesGeneralized), DiskSizeGB: to.Ptr(int32(20))}}}}
					if mode == "windows" {
						img.Properties.StorageProfile.OSDisk.OSType = to.Ptr(armcompute.OperatingSystemTypesWindows)
					}
					if mode == "disk" {
						img.Properties.StorageProfile.OSDisk.DiskSizeGB = to.Ptr(int32(50))
					}
					if mode == "wrong-image" {
						img.ID = to.Ptr(prefix + "Microsoft.Compute/images/other")
					}
					if mode == "permission" {
						response := jsonResponse(r, `{"error":{"code":"AuthorizationFailed","message":"denied"}}`)
						response.StatusCode = 403
						return response, nil
					}
					result = img
				case strings.HasSuffix(r.URL.Path, "/skus"):
					if r.URL.Query().Get("$filter") != "location eq 'eastus'" {
						t.Fatal("SKU query not scoped to region")
					}
					sku := &armcompute.ResourceSKU{Name: to.Ptr(spec.Size), ResourceType: to.Ptr("virtualMachines"), Locations: []*string{to.Ptr("eastus")}, Capabilities: []*armcompute.ResourceSKUCapabilities{{Name: to.Ptr("CpuArchitectureType"), Value: to.Ptr("x64")}, {Name: to.Ptr("vCPUs"), Value: to.Ptr("2")}, {Name: to.Ptr("MemoryGB"), Value: to.Ptr("4")}}}
					if mode == "size-restricted" {
						sku.Restrictions = []*armcompute.ResourceSKURestrictions{{}}
					}
					result = map[string]any{"value": []*armcompute.ResourceSKU{sku}}
				case strings.HasSuffix(r.URL.Path, "/networkInterfaces/fixture"):
					nic := armnetwork.Interface{ID: to.Ptr(spec.NetworkInterfaceID), Location: to.Ptr("eastus"), Properties: &armnetwork.InterfacePropertiesFormat{ProvisioningState: to.Ptr(armnetwork.ProvisioningStateSucceeded), IPConfigurations: []*armnetwork.InterfaceIPConfiguration{{Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{PrivateIPAddressVersion: to.Ptr(armnetwork.IPVersionIPv4), Subnet: &armnetwork.Subnet{ID: to.Ptr(prefix + "Microsoft.Network/virtualNetworks/fixture/subnets/fixture")}}}}}}
					if mode == "attached-nic" {
						nic.Properties.VirtualMachine = &armnetwork.SubResource{ID: to.Ptr("existing-vm")}
					}
					if mode == "public-ip" {
						nic.Properties.IPConfigurations[0].Properties.PublicIPAddress = &armnetwork.PublicIPAddress{}
					}
					if mode == "foreign-subnet" {
						nic.Properties.IPConfigurations[0].Properties.Subnet.ID = to.Ptr("/subscriptions/33333333-3333-4333-8333-333333333333/resourceGroups/fixture/providers/Microsoft.Network/virtualNetworks/fixture/subnets/fixture")
					}
					result = nic
				case strings.HasSuffix(r.URL.Path, "/virtualNetworks/fixture/subnets/fixture"):
					result = armnetwork.Subnet{ID: to.Ptr(prefix + "Microsoft.Network/virtualNetworks/fixture/subnets/fixture"), Properties: &armnetwork.SubnetPropertiesFormat{AddressPrefix: to.Ptr("10.0.0.0/24"), ProvisioningState: to.Ptr(armnetwork.ProvisioningStateSucceeded)}}
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				raw, _ := json.Marshal(result)
				return jsonResponse(r, string(raw)), nil
			})
			options := &arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: transport}, DisableRPRegistration: true}
			images, err := armcompute.NewImagesClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			skus, err := armcompute.NewResourceSKUsClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			nics, err := armnetwork.NewInterfacesClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			subnets, err := armnetwork.NewSubnetsClient(subscription, azureTestCredential{}, options)
			if err != nil {
				t.Fatal(err)
			}
			p := &Azure{region: "eastus", credentials: AzureCredentials{SubscriptionID: subscription}, images: images, skus: skus, nics: nics, subnets: subnets}
			review, err := p.InspectMachine(context.Background(), spec, "amd64")
			if mode == "foreign-subnet" || mode == "wrong-image" || mode == "permission" {
				if err == nil {
					t.Fatal("invalid resource identity/read accepted")
				}
				return
			}
			if err != nil || calls != 4 {
				t.Fatalf("inspection: %v calls=%d", err, calls)
			}
			if review.CanLaunch || review.ResourceCompatible || review.MonthlyCostEstimate != nil {
				t.Fatal("unknown image architecture was certified")
			}
			if mode == "baseline" && (len(review.Blockers) != 1 || review.Blockers[0].Code != "image_architecture_unverified" || review.VCPUs != 2 || review.MemoryMB != 4096) {
				t.Fatalf("baseline: %+v", review)
			}
			if mode != "baseline" && len(review.Blockers) < 2 {
				t.Fatal("missing compatibility blocker")
			}
		})
	}
}
