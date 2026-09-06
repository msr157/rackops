package cloud

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"strings"
	"time"
)

// Match only canonical Compute API resource references. Never follow links
// returned by metadata as HTTP endpoints or accept a suffix-only match.
func gcpReference(value, expected string) bool {
	return value == expected || value == "https://www.googleapis.com/compute/v1/"+expected || value == "https://compute.googleapis.com/compute/v1/"+expected
}

func (p *GCP) InspectMachine(ctx context.Context, s GCPMachineSpec, architecture string) (MachineInspection, error) {
	if err := s.Validate(); err != nil {
		return MachineInspection{}, err
	}
	if s.ProjectID != p.project || s.Zone[:len(s.Zone)-2] != p.region {
		return MachineInspection{}, errors.New("Google target mismatch")
	}
	if architecture != "amd64" && architecture != "arm64" {
		return MachineInspection{}, errors.New("unsupported target architecture")
	}
	out := MachineInspection{Provider: p.Name(), Region: p.region, SpecFingerprint: (MachineSpec{GCP: &s}).Fingerprint(), CheckedAt: time.Now().UTC(), Architecture: architecture, Zone: s.Zone, Blockers: []InspectionFinding{}, RemainingChecks: []string{
		"Verify the image is Linux and includes systemd, Docker, Compose, Git and the Google guest environment that runs startup scripts.",
		"Verify private subnet capacity, firewall policy, routes, DNS and outbound HTTPS to the control plane and binary download hosts.",
		"Check launch permissions, quotas, current VM capacity, disk support and image/VM firmware compatibility.",
		"Obtain recurring and transfer cost estimates and approve the final resource plan before launch.",
	}}
	add := func(code, message string) { out.Blockers = append(out.Blockers, InspectionFinding{code, message}) }
	imageParts := strings.Split(s.Image, "/")
	img, err := p.compute.Images.Get(imageParts[1], imageParts[4]).Context(ctx).Do()
	if err != nil {
		return MachineInspection{}, err
	}
	if img == nil || img.Name != imageParts[4] || !gcpReference(img.SelfLink, s.Image) {
		return MachineInspection{}, errors.New("Google image identity is incomplete or different from the selection")
	}
	arch := "X86_64"
	if architecture == "arm64" {
		arch = "ARM64"
	}
	if img.Architecture != arch {
		add("image_architecture", "The image architecture is unknown or does not match the selected architecture.")
	}
	if img.Status != "READY" || img.Deprecated != nil && img.Deprecated.State != "" && img.Deprecated.State != "ACTIVE" {
		add("image_unavailable", "Select a ready image that has not been deprecated.")
	}
	for _, feature := range img.GuestOsFeatures {
		if feature != nil && feature.Type == "WINDOWS" {
			add("image_os", "Windows images are outside the Linux migration scope.")
			break
		}
	}
	if img.EnableConfidentialCompute {
		add("confidential_image", "Confidential-compute images require a separate machine compatibility plan.")
	}
	if img.DiskSizeGb <= 0 || img.DiskSizeGb > math.MaxInt32 {
		add("disk_unknown", "The image's minimum root disk size is unavailable.")
	} else {
		out.MinimumDiskGB = int32(img.DiskSizeGb)
		if s.DiskGB < img.DiskSizeGb {
			add("disk_size", "The requested disk is smaller than the selected image.")
		}
	}
	zone, err := p.compute.Zones.Get(p.project, s.Zone).Context(ctx).Do()
	if err != nil {
		return MachineInspection{}, err
	}
	regionRef := "projects/" + p.project + "/regions/" + p.region
	if zone == nil || zone.Name != s.Zone || !gcpReference(zone.Region, regionRef) {
		return MachineInspection{}, errors.New("Google zone metadata does not match the target region")
	}
	if zone.Status != "UP" {
		add("zone_unavailable", "The selected availability zone is not up.")
	}
	size, err := p.compute.MachineTypes.Get(p.project, s.Zone, s.MachineType).Context(ctx).Do()
	if err != nil {
		return MachineInspection{}, err
	}
	if size == nil || size.Name != s.MachineType || !gcpReference(size.SelfLink, "projects/"+p.project+"/zones/"+s.Zone+"/machineTypes/"+s.MachineType) {
		return MachineInspection{}, errors.New("Google VM size metadata is incomplete or different from the selection")
	}
	if size.Architecture != arch {
		add("size_architecture", "The VM size architecture is unknown or does not match the selected architecture.")
	}
	if size.GuestCpus <= 0 || size.GuestCpus > math.MaxInt32 || size.MemoryMb <= 0 {
		add("size_unknown", "CPU or memory capacity is unavailable for this VM size.")
	} else {
		out.VCPUs = int32(size.GuestCpus)
		out.MemoryMB = size.MemoryMb
	}
	if size.Deprecated != nil && size.Deprecated.State != "" && size.Deprecated.State != "ACTIVE" {
		add("size_unavailable", "The selected VM size has been deprecated.")
	}
	if len(size.Accelerators) > 0 || size.BundledLocalSsds != nil || strings.HasSuffix(s.MachineType, "-metal") {
		add("unsupported_hardware", "Accelerators, bundled local storage and bare-metal sizes are outside the initial setup scope.")
	}
	subnetParts := strings.Split(s.Subnetwork, "/")
	subnet, err := p.compute.Subnetworks.Get(p.project, p.region, subnetParts[5]).Context(ctx).Do()
	if err != nil {
		return MachineInspection{}, err
	}
	if subnet == nil || subnet.Name != subnetParts[5] || !gcpReference(subnet.SelfLink, s.Subnetwork) || !gcpReference(subnet.Region, regionRef) {
		return MachineInspection{}, errors.New("Google subnet metadata does not match the target project and region")
	}
	prefix, cidrErr := netip.ParsePrefix(subnet.IpCidrRange)
	if cidrErr != nil || !prefix.Addr().Is4() || (subnet.StackType != "" && subnet.StackType != "IPV4_ONLY" && subnet.StackType != "IPV4_IPV6") || (subnet.Purpose != "" && subnet.Purpose != "PRIVATE") {
		add("subnet_unavailable", "Select a regular VM subnet with an IPv4 range.")
	}
	// A network reference is evidence only; firewall and reachability checks
	// remain mandatory even when this metadata is structurally compatible.
	networkParts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(subnet.Network, "https://www.googleapis.com/compute/v1/"), "https://compute.googleapis.com/compute/v1/"), "/")
	if len(networkParts) != 5 || networkParts[0] != "projects" || networkParts[1] != p.project || networkParts[2] != "global" || networkParts[3] != "networks" || !gcpResource.MatchString(networkParts[4]) {
		add("network_mismatch", "The subnet must reference a network in the target project.")
	}
	out.ResourceCompatible = len(out.Blockers) == 0
	return out, nil
}
