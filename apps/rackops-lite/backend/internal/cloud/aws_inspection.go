package cloud

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type awsInventoryAPI interface {
	DescribeImages(context.Context, *ec2.DescribeImagesInput, ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
	DescribeInstanceTypes(context.Context, *ec2.DescribeInstanceTypesInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeInstanceTypeOfferings(context.Context, *ec2.DescribeInstanceTypeOfferingsInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
}

// InspectMachine reads only the explicitly selected resources. No launch,
// permission dry-run, resource discovery across accounts or mutation occurs.
func (p *AWS) InspectMachine(ctx context.Context, s AWSMachineSpec, architecture string) (MachineInspection, error) {
	if err := s.Validate(); err != nil {
		return MachineInspection{}, err
	}
	if architecture != "amd64" && architecture != "arm64" {
		return MachineInspection{}, errors.New("unsupported target architecture")
	}
	if p.inventory == nil {
		return MachineInspection{}, errors.New("AWS inventory client unavailable")
	}
	out := MachineInspection{Provider: p.Name(), Region: p.region, SpecFingerprint: (MachineSpec{AWS: &s}).Fingerprint(), CheckedAt: time.Now().UTC(),
		Architecture: architecture, Blockers: []InspectionFinding{}, RemainingChecks: []string{
			"Verify the image contains systemd, Docker, Compose, Git and the pinned RackOps agent prerequisites.",
			"Verify security rules, route tables, DNS and outbound HTTPS from the private subnet to the control plane and binary download hosts.",
			"Check launch permissions, quotas, current capacity and image/VM firmware compatibility.",
			"Obtain recurring and transfer cost estimates and approve the final resource plan before launch.",
		}}
	add := func(code, message string) { out.Blockers = append(out.Blockers, InspectionFinding{code, message}) }
	images, err := p.inventory.DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{s.ImageID}})
	if err != nil {
		return MachineInspection{}, err
	}
	if images == nil || len(images.Images) != 1 || aws.ToString(images.NextToken) != "" || aws.ToString(images.Images[0].ImageId) != s.ImageID {
		return MachineInspection{}, errors.New("selected AWS image is unavailable or its metadata is incomplete")
	}
	img := images.Images[0]
	arch := ec2types.ArchitectureValuesX8664
	if architecture == "arm64" {
		arch = ec2types.ArchitectureValuesArm64
	}
	if img.Architecture != arch {
		add("image_architecture", "The image architecture does not match the requested architecture.")
	}
	if img.State != ec2types.ImageStateAvailable || aws.ToBool(img.ImageAllowed) == false && img.ImageAllowed != nil {
		add("image_unavailable", "The image is unavailable or denied by this account's image policy.")
	}
	if aws.ToString(img.PlatformDetails) != "Linux/UNIX" || img.Platform != "" {
		add("image_os", "Use a verified Linux image without a Windows or licensed platform requirement.")
	}
	if img.RootDeviceType != ec2types.DeviceTypeEbs || img.VirtualizationType != ec2types.VirtualizationTypeHvm || aws.ToString(img.RootDeviceName) != s.RootDevice {
		add("image_boot", "Use an EBS-backed HVM image and its exact root device name.")
	}
	if len(img.BlockDeviceMappings) != 1 || aws.ToString(img.BlockDeviceMappings[0].DeviceName) != s.RootDevice || img.BlockDeviceMappings[0].Ebs == nil {
		add("image_storage", "The image must declare exactly one EBS root disk; additional image disks require an explicit storage plan.")
	} else {
		out.MinimumDiskGB = aws.ToInt32(img.BlockDeviceMappings[0].Ebs.VolumeSize)
		if out.MinimumDiskGB <= 0 || s.DiskGB < out.MinimumDiskGB {
			add("disk_size", "The requested root disk is smaller than the image or its minimum size is unknown.")
		}
	}
	types, err := p.inventory.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(s.InstanceType)}})
	if err != nil {
		return MachineInspection{}, err
	}
	if types == nil || len(types.InstanceTypes) != 1 || aws.ToString(types.NextToken) != "" || string(types.InstanceTypes[0].InstanceType) != s.InstanceType {
		return MachineInspection{}, errors.New("selected AWS VM size metadata is incomplete")
	}
	size := types.InstanceTypes[0]
	supported := false
	if size.ProcessorInfo != nil {
		for _, a := range size.ProcessorInfo.SupportedArchitectures {
			if string(a) == string(arch) {
				supported = true
			}
		}
	}
	if !supported {
		add("size_architecture", "The selected VM size does not support the requested architecture.")
	}
	if size.VCpuInfo != nil {
		out.VCPUs = aws.ToInt32(size.VCpuInfo.DefaultVCpus)
	}
	if size.MemoryInfo != nil {
		out.MemoryMB = aws.ToInt64(size.MemoryInfo.SizeInMiB)
	}
	if out.VCPUs <= 0 || out.MemoryMB <= 0 {
		add("size_unknown", "CPU or memory capacity is unavailable for this VM size.")
	}
	if size.GpuInfo != nil || size.FpgaInfo != nil || size.InferenceAcceleratorInfo != nil || size.NeuronInfo != nil || aws.ToBool(size.BareMetal) {
		add("unsupported_hardware", "Accelerator and bare-metal VM sizes are outside the initial cloud setup scope.")
	}
	subnets, err := p.inventory.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{SubnetIds: []string{s.SubnetID}})
	if err != nil {
		return MachineInspection{}, err
	}
	if subnets == nil || len(subnets.Subnets) != 1 || aws.ToString(subnets.NextToken) != "" || aws.ToString(subnets.Subnets[0].SubnetId) != s.SubnetID {
		return MachineInspection{}, errors.New("selected AWS subnet metadata is incomplete")
	}
	subnet := subnets.Subnets[0]
	out.Zone = aws.ToString(subnet.AvailabilityZone)
	if subnet.State != ec2types.SubnetStateAvailable || aws.ToInt32(subnet.AvailableIpAddressCount) < 1 || aws.ToBool(subnet.Ipv6Native) || out.Zone == "" || aws.ToString(subnet.VpcId) == "" {
		add("subnet_unavailable", "Select an available IPv4 subnet with free addresses and a known availability zone.")
	}
	groups, err := p.inventory.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{GroupIds: []string{s.SecurityGroupID}})
	if err != nil {
		return MachineInspection{}, err
	}
	if groups == nil || len(groups.SecurityGroups) != 1 || aws.ToString(groups.NextToken) != "" || aws.ToString(groups.SecurityGroups[0].GroupId) != s.SecurityGroupID {
		return MachineInspection{}, errors.New("selected AWS security group metadata is incomplete")
	}
	if aws.ToString(groups.SecurityGroups[0].VpcId) == "" || aws.ToString(groups.SecurityGroups[0].VpcId) != aws.ToString(subnet.VpcId) {
		add("network_mismatch", "The security group must belong to the selected subnet's VPC.")
	}
	if out.Zone != "" {
		offerings, err := p.inventory.DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{LocationType: ec2types.LocationTypeAvailabilityZone, Filters: []ec2types.Filter{
			{Name: aws.String("instance-type"), Values: []string{s.InstanceType}}, {Name: aws.String("location"), Values: []string{out.Zone}},
		}})
		if err != nil {
			return MachineInspection{}, err
		}
		if offerings == nil || aws.ToString(offerings.NextToken) != "" {
			return MachineInspection{}, errors.New("AWS zone offering metadata is incomplete")
		}
		available := false
		for _, offer := range offerings.InstanceTypeOfferings {
			if string(offer.InstanceType) == s.InstanceType && aws.ToString(offer.Location) == out.Zone && offer.LocationType == ec2types.LocationTypeAvailabilityZone {
				available = true
			}
		}
		if !available {
			add("size_not_offered", "The selected VM size is not offered in the subnet's availability zone.")
		}
	}
	out.ResourceCompatible = len(out.Blockers) == 0
	return out, nil
}
