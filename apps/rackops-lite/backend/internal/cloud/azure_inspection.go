package cloud

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v7"
)

func azureValue[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func (p *Azure) InspectMachine(ctx context.Context, s AzureMachineSpec, architecture string) (MachineInspection, error) {
	if err := s.Validate(); err != nil {
		return MachineInspection{}, err
	}
	if !strings.EqualFold(s.SubscriptionID, p.credentials.SubscriptionID) {
		return MachineInspection{}, errors.New("Azure subscription mismatch")
	}
	if architecture != "amd64" && architecture != "arm64" {
		return MachineInspection{}, errors.New("unsupported target architecture")
	}
	out := MachineInspection{Provider: p.Name(), Region: p.region, SpecFingerprint: (MachineSpec{Azure: &s}).Fingerprint(), CheckedAt: time.Now().UTC(), Architecture: architecture, Blockers: []InspectionFinding{}, RemainingChecks: []string{
		"Verify image CPU architecture and prepare Linux with cloud-init, systemd, Docker, Compose and Git.",
		"Verify effective network security rules, route tables, DNS, address capacity and outbound HTTPS from the selected NIC.",
		"Check launch permissions, quotas, current VM capacity, accelerated-networking support and image/VM firmware compatibility.",
		"Obtain recurring and transfer cost estimates and approve the final resource plan before launch.",
	}}
	add := func(code, message string) { out.Blockers = append(out.Blockers, InspectionFinding{code, message}) }
	imageParts := strings.Split(s.ImageID, "/")
	img, err := p.images.Get(ctx, imageParts[4], imageParts[8], nil)
	if err != nil {
		return MachineInspection{}, err
	}
	if !strings.EqualFold(azureValue(img.ID), s.ImageID) || !strings.EqualFold(azureValue(img.Location), p.region) || img.Properties == nil {
		return MachineInspection{}, errors.New("Azure image identity or location does not match the selection")
	}
	if azureValue(img.Properties.ProvisioningState) != "Succeeded" {
		add("image_unavailable", "The selected image has not finished provisioning.")
	}
	// The managed-image model does not carry a CPU architecture assertion. Do
	// not infer architecture from image names, tags or a mutable source VM.
	add("image_architecture_unverified", "The managed image's CPU architecture must be verified before this plan can launch.")
	storage := img.Properties.StorageProfile
	if storage == nil || storage.OSDisk == nil {
		add("image_storage", "Image OS disk metadata is missing.")
	} else {
		if azureValue(storage.OSDisk.OSType) != "Linux" || azureValue(storage.OSDisk.OSState) != "Generalized" {
			add("image_os", "Select a generalized Linux image.")
		}
		if len(storage.DataDisks) > 0 {
			add("image_storage", "Additional image disks require an explicit storage plan.")
		}
		if storage.OSDisk.DiskSizeGB != nil {
			out.MinimumDiskGB = *storage.OSDisk.DiskSizeGB
		}
		if out.MinimumDiskGB <= 0 || s.DiskGB < out.MinimumDiskGB {
			add("disk_size", "The requested root disk is smaller than the image or its minimum size is unknown.")
		}
	}
	var selected *armcompute.ResourceSKU
	pager := p.skus.NewListPager(&armcompute.ResourceSKUsClientListOptions{Filter: to.Ptr("location eq '" + p.region + "'")})
	for page := 0; pager.More(); page++ {
		if page >= 50 {
			return MachineInspection{}, errors.New("Azure SKU inventory exceeded inspection limit")
		}
		response, err := pager.NextPage(ctx)
		if err != nil {
			return MachineInspection{}, err
		}
		for _, sku := range response.Value {
			if sku != nil && azureValue(sku.ResourceType) == "virtualMachines" && strings.EqualFold(azureValue(sku.Name), s.Size) {
				if selected != nil {
					return MachineInspection{}, errors.New("Azure VM size inventory is ambiguous")
				}
				selected = sku
			}
		}
	}
	if selected == nil {
		add("size_unavailable", "The selected VM size is not offered in the target region.")
	} else {
		location := false
		for _, loc := range selected.Locations {
			location = location || strings.EqualFold(azureValue(loc), p.region)
		}
		if !location || len(selected.Restrictions) > 0 {
			add("size_restricted", "This VM size has location or subscription restrictions that must be resolved.")
		}
		capabilities := map[string]string{}
		for _, c := range selected.Capabilities {
			if c != nil {
				capabilities[strings.ToLower(azureValue(c.Name))] = azureValue(c.Value)
			}
		}
		arch := "x64"
		if architecture == "arm64" {
			arch = "Arm64"
		}
		if !strings.EqualFold(capabilities["cpuarchitecturetype"], arch) {
			add("size_architecture", "VM size architecture is unknown or does not match the selection.")
		}
		cpus, cpuErr := strconv.ParseInt(capabilities["vcpus"], 10, 32)
		memory, memErr := strconv.ParseFloat(capabilities["memorygb"], 64)
		if cpuErr != nil || cpus <= 0 || memErr != nil || memory <= 0 || math.IsNaN(memory) || math.IsInf(memory, 0) || memory > 1<<30 {
			add("size_unknown", "CPU or memory capacity is unavailable for this VM size.")
		} else {
			out.VCPUs = int32(cpus)
			out.MemoryMB = int64(memory * 1024)
		}
		for _, accelerator := range []string{"gpus", "fpgas"} {
			if v := capabilities[accelerator]; v != "" && v != "0" {
				add("unsupported_"+accelerator, "Accelerator VM sizes are outside the initial setup scope.")
			}
		}
	}
	nicParts := strings.Split(s.NetworkInterfaceID, "/")
	nic, err := p.nics.Get(ctx, nicParts[4], nicParts[8], nil)
	if err != nil {
		return MachineInspection{}, err
	}
	if !strings.EqualFold(azureValue(nic.ID), s.NetworkInterfaceID) || !strings.EqualFold(azureValue(nic.Location), p.region) || nic.Properties == nil {
		return MachineInspection{}, errors.New("Azure NIC identity or location does not match the selection")
	}
	n := nic.Properties
	if azureValue(n.ProvisioningState) != "Succeeded" || n.VirtualMachine != nil || n.PrivateEndpoint != nil || n.PrivateLinkService != nil || len(n.HostedWorkloads) > 0 {
		add("nic_unavailable", "Select a fully provisioned NIC that is not attached to another resource.")
	}
	if n.EnableIPForwarding != nil && *n.EnableIPForwarding {
		add("nic_forwarding", "IP-forwarding interfaces require a separate network plan.")
	}
	if len(n.IPConfigurations) != 1 || n.IPConfigurations[0] == nil || n.IPConfigurations[0].Properties == nil {
		add("nic_configuration", "Select a NIC with exactly one private IPv4 configuration.")
	} else {
		ip := n.IPConfigurations[0].Properties
		if ip.PublicIPAddress != nil || azureValue(ip.PrivateIPAddressVersion) != "IPv4" || len(ip.LoadBalancerBackendAddressPools) > 0 || len(ip.ApplicationGatewayBackendAddressPools) > 0 || len(ip.LoadBalancerInboundNatRules) > 0 || ip.GatewayLoadBalancer != nil {
			add("nic_configuration", "Public IPs, non-IPv4 and load-balancer attachments require a separate network plan.")
		}
		if ip.Subnet == nil {
			return MachineInspection{}, errors.New("Azure NIC subnet reference missing")
		}
		parts := strings.Split(azureValue(ip.Subnet.ID), "/")
		if len(parts) != 11 || !strings.EqualFold(parts[1], "subscriptions") || !strings.EqualFold(parts[2], s.SubscriptionID) || !strings.EqualFold(parts[3], "resourceGroups") || !strings.EqualFold(parts[5], "providers") || !strings.EqualFold(parts[6], "Microsoft.Network") || !strings.EqualFold(parts[7], "virtualNetworks") || !strings.EqualFold(parts[9], "subnets") || !azureGroup.MatchString(parts[4]) || !azureGroup.MatchString(parts[8]) || !azureGroup.MatchString(parts[10]) {
			return MachineInspection{}, errors.New("Azure subnet is not an explicit resource in the target subscription")
		}
		subnet, err := p.subnets.Get(ctx, parts[4], parts[8], parts[10], nil)
		if err != nil {
			return MachineInspection{}, err
		}
		if !strings.EqualFold(azureValue(subnet.ID), azureValue(ip.Subnet.ID)) || subnet.Properties == nil {
			return MachineInspection{}, errors.New("Azure subnet metadata is incomplete")
		}
		props := subnet.Properties
		ipv4 := false
		prefixes := append([]*string{props.AddressPrefix}, props.AddressPrefixes...)
		for _, value := range prefixes {
			if cidr, err := netip.ParsePrefix(azureValue(value)); err == nil && cidr.Addr().Is4() {
				ipv4 = true
			}
		}
		if !ipv4 || props.ProvisioningState == nil || *props.ProvisioningState != armnetwork.ProvisioningStateSucceeded || len(props.Delegations) > 0 {
			add("subnet_unavailable", "Select a provisioned IPv4 subnet without service delegation.")
		}
	}
	out.ResourceCompatible = len(out.Blockers) == 0
	return out, nil
}
