package cloud

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcloud "github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"golang.org/x/crypto/ssh"
)

type AzureCredentials struct {
	TenantID       string `json:"tenantId"`
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
	SubscriptionID string `json:"subscriptionId"`
}

type Azure struct {
	bootstrap     string
	region        string
	credentials   AzureCredentials
	subscriptions *armsubscriptions.Client
	machines      *armcompute.VirtualMachinesClient
	images        *armcompute.ImagesClient
	skus          *armcompute.ResourceSKUsClient
	nics          *armnetwork.InterfacesClient
	subnets       *armnetwork.SubnetsClient
}

var uuidID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var azureRegion = regexp.MustCompile(`^[a-z][a-z0-9]{1,49}$`)
var azureGroup = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func NewAzure(region string, c AzureCredentials) (*Azure, error) {
	if !azureRegion.MatchString(region) || !uuidID.MatchString(c.TenantID) || !uuidID.MatchString(c.ClientID) || !uuidID.MatchString(c.SubscriptionID) || c.ClientSecret == "" {
		return nil, errors.New("Azure region and explicit tenant, subscription and client credentials are required")
	}
	credential, err := azidentity.NewClientSecretCredential(c.TenantID, c.ClientID, c.ClientSecret, &azidentity.ClientSecretCredentialOptions{ClientOptions: azcore.ClientOptions{Cloud: azcloud.AzurePublic}})
	if err != nil {
		return nil, err
	}
	subscriptions, err := armsubscriptions.NewClient(credential, &arm.ClientOptions{DisableRPRegistration: true})
	if err != nil {
		return nil, err
	}
	machines, err := armcompute.NewVirtualMachinesClient(c.SubscriptionID, credential, &arm.ClientOptions{DisableRPRegistration: true})
	if err != nil {
		return nil, err
	}
	options := &arm.ClientOptions{DisableRPRegistration: true}
	images, err := armcompute.NewImagesClient(c.SubscriptionID, credential, options)
	if err != nil {
		return nil, err
	}
	skus, err := armcompute.NewResourceSKUsClient(c.SubscriptionID, credential, options)
	if err != nil {
		return nil, err
	}
	nics, err := armnetwork.NewInterfacesClient(c.SubscriptionID, credential, options)
	if err != nil {
		return nil, err
	}
	subnets, err := armnetwork.NewSubnetsClient(c.SubscriptionID, credential, options)
	if err != nil {
		return nil, err
	}
	return &Azure{region: region, credentials: c, subscriptions: subscriptions, machines: machines, images: images, skus: skus, nics: nics, subnets: subnets}, nil
}
func (p *Azure) Name() string   { return "azure" }
func (p *Azure) Region() string { return p.region }
func (p *Azure) Validate(ctx context.Context) (Identity, error) {
	result, err := p.subscriptions.Get(ctx, p.credentials.SubscriptionID, nil)
	if err != nil {
		return Identity{}, err
	}
	if result.SubscriptionID == nil || !strings.EqualFold(*result.SubscriptionID, p.credentials.SubscriptionID) || result.State == nil || *result.State != armsubscriptions.SubscriptionStateEnabled {
		return Identity{}, errors.New("Azure subscription is unavailable")
	}
	return Identity{AccountID: strings.ToLower(*result.SubscriptionID), Principal: strings.ToLower(p.credentials.TenantID + "/" + p.credentials.ClientID)}, nil
}

type AzureMachineSpec struct {
	OperationID        string `json:"operationId"`
	SubscriptionID     string `json:"subscriptionId"`
	ResourceGroup      string `json:"resourceGroup"`
	Size               string `json:"size"`
	ImageID            string `json:"imageId"`
	NetworkInterfaceID string `json:"networkInterfaceId"`
	SSHPublicKey       string `json:"sshPublicKey"`
	DiskGB             int32  `json:"diskGb"`
}

func (s AzureMachineSpec) Validate() error {
	if !uuidID.MatchString(s.OperationID) || !uuidID.MatchString(s.SubscriptionID) || !azureGroup.MatchString(s.ResourceGroup) || !azureGroup.MatchString(s.Size) || s.DiskGB < 8 || s.DiskGB > 32767 {
		return errors.New("invalid Azure resource specification")
	}
	prefix := "/subscriptions/" + strings.ToLower(s.SubscriptionID) + "/resourcegroups/"
	for _, item := range []struct{ value, kind string }{{s.ImageID, "microsoft.compute/images"}, {s.NetworkInterfaceID, "microsoft.network/networkinterfaces"}} {
		value := strings.ToLower(item.value)
		parts := strings.Split(value, "/")
		if !strings.HasPrefix(value, prefix) || len(parts) != 9 || parts[5] != "providers" || parts[6]+"/"+parts[7] != item.kind || !azureGroup.MatchString(parts[4]) || !azureGroup.MatchString(parts[8]) {
			return errors.New("Azure image and NIC must be explicit resources in the target subscription")
		}
	}
	_, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(s.SSHPublicKey))
	if err != nil || len(strings.TrimSpace(string(rest))) != 0 {
		return errors.New("one valid SSH public key is required")
	}
	return nil
}
func (s AzureMachineSpec) vm(region string) armcompute.VirtualMachine {
	return armcompute.VirtualMachine{Location: to.Ptr(region), Tags: map[string]*string{"rackops-operation": to.Ptr(s.OperationID), "rackops-spec": to.Ptr(fingerprint(s))}, Properties: &armcompute.VirtualMachineProperties{
		HardwareProfile: &armcompute.HardwareProfile{VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(s.Size))},
		StorageProfile:  &armcompute.StorageProfile{ImageReference: &armcompute.ImageReference{ID: to.Ptr(s.ImageID)}, OSDisk: &armcompute.OSDisk{CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage), DiskSizeGB: to.Ptr(s.DiskGB), DeleteOption: to.Ptr(armcompute.DiskDeleteOptionTypesDetach), ManagedDisk: &armcompute.ManagedDiskParameters{StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardSSDLRS)}}},
		OSProfile:       &armcompute.OSProfile{ComputerName: to.Ptr("rackops"), AdminUsername: to.Ptr("rackops"), LinuxConfiguration: &armcompute.LinuxConfiguration{DisablePasswordAuthentication: to.Ptr(true), SSH: &armcompute.SSHConfiguration{PublicKeys: []*armcompute.SSHPublicKey{{Path: to.Ptr("/home/rackops/.ssh/authorized_keys"), KeyData: to.Ptr(s.SSHPublicKey)}}}}},
		NetworkProfile:  &armcompute.NetworkProfile{NetworkInterfaces: []*armcompute.NetworkInterfaceReference{{ID: to.Ptr(s.NetworkInterfaceID), Properties: &armcompute.NetworkInterfaceReferenceProperties{Primary: to.Ptr(true), DeleteOption: to.Ptr(armcompute.DeleteOptionsDetach)}}}},
	}}
}
func (p *Azure) Start(ctx context.Context, s MachineSpec) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	spec := s.Azure
	if !strings.EqualFold(spec.SubscriptionID, p.credentials.SubscriptionID) {
		return Receipt{}, errors.New("Azure subscription mismatch")
	}
	name := "rackops-" + spec.OperationID
	vm := spec.vm(p.region)
	if p.bootstrap != "" {
		vm.Properties.OSProfile.CustomData = to.Ptr(base64.StdEncoding.EncodeToString([]byte(p.bootstrap)))
	}
	poller, err := p.machines.BeginCreateOrUpdate(ctx, spec.ResourceGroup, name, vm, &armcompute.VirtualMachinesClientBeginCreateOrUpdateOptions{IfNoneMatch: to.Ptr("*")})
	if err != nil {
		return Receipt{}, err
	}
	// Resource identity is known even if the long-running operation is pending.
	_ = poller
	id := "/subscriptions/" + spec.SubscriptionID + "/resourceGroups/" + spec.ResourceGroup + "/providers/Microsoft.Compute/virtualMachines/" + name
	return Receipt{ResourceID: id, OperationID: name, State: "submitted"}, nil
}
func (p *Azure) Reconcile(ctx context.Context, s MachineSpec, _ Receipt) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	spec := s.Azure
	if !strings.EqualFold(spec.SubscriptionID, p.credentials.SubscriptionID) {
		return Receipt{}, errors.New("Azure subscription mismatch")
	}
	result, err := p.machines.Get(ctx, spec.ResourceGroup, "rackops-"+spec.OperationID, nil)
	if err != nil {
		return Receipt{}, err
	}
	if result.ID == nil || result.Tags["rackops-spec"] == nil || *result.Tags["rackops-spec"] != fingerprint(*spec) || result.Tags["rackops-operation"] == nil || *result.Tags["rackops-operation"] != spec.OperationID || result.Location == nil || *result.Location != p.region {
		return Receipt{}, errors.New("Azure resource ownership mismatch")
	}
	state := "unknown"
	if result.Properties != nil && result.Properties.ProvisioningState != nil {
		state = *result.Properties.ProvisioningState
	}
	return Receipt{ResourceID: *result.ID, OperationID: "rackops-" + spec.OperationID, State: state}, nil
}
