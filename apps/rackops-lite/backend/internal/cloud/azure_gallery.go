package cloud

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
)

var galleryVersionNumber = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func validAzureGalleryVersion(id, subscription string) bool {
	p := strings.Split(strings.ToLower(id), "/")
	return len(p) == 13 && p[0] == "" && p[1] == "subscriptions" && p[2] == strings.ToLower(subscription) && p[3] == "resourcegroups" && azureGroup.MatchString(p[4]) && p[5] == "providers" && p[6] == "microsoft.compute" && p[7] == "galleries" && azureGroup.MatchString(p[8]) && p[9] == "images" && azureGroup.MatchString(p[10]) && p[11] == "versions" && galleryVersionNumber.MatchString(p[12])
}

func (p *Azure) inspectGalleryImage(ctx context.Context, s AzureMachineSpec, architecture string, out *MachineInspection) error {
	if p.galleryImages == nil || p.galleryVersions == nil {
		return errors.New("gallery clients unavailable")
	}
	parts := strings.Split(s.ImageID, "/")
	definitionID := strings.Join(parts[:11], "/")
	definition, err := p.galleryImages.Get(ctx, parts[4], parts[8], parts[10], nil)
	if err != nil {
		return err
	}
	version, err := p.galleryVersions.Get(ctx, parts[4], parts[8], parts[10], parts[12], &armcompute.GalleryImageVersionsClientGetOptions{Expand: to.Ptr(armcompute.ReplicationStatusTypesReplicationStatus)})
	if err != nil {
		return err
	}
	if !strings.EqualFold(azureValue(definition.ID), definitionID) || !strings.EqualFold(azureValue(version.ID), s.ImageID) || definition.Properties == nil || version.Properties == nil {
		return errors.New("gallery identity or metadata mismatch")
	}
	add := func(code, message string) { out.Blockers = append(out.Blockers, InspectionFinding{code, message}) }
	d, v := definition.Properties, version.Properties
	expected := "x64"
	if architecture == "arm64" {
		expected = "Arm64"
	}
	if !strings.EqualFold(azureValue(d.Architecture), expected) {
		add("image_architecture", "Gallery image architecture is missing or does not match the requested architecture.")
	}
	if azureValue(d.OSType) != "Linux" || azureValue(d.OSState) != "Generalized" {
		add("image_os", "Select a generalized Linux gallery image.")
	}
	if azureValue(d.ProvisioningState) != "Succeeded" || azureValue(v.ProvisioningState) != "Succeeded" {
		add("image_unavailable", "Gallery definition or version is not ready.")
	}
	if d.EndOfLifeDate != nil && !time.Now().Before(*d.EndOfLifeDate) {
		add("image_expired", "Gallery image definition has reached end of life.")
	}
	if v.PublishingProfile != nil && v.PublishingProfile.EndOfLifeDate != nil && !time.Now().Before(*v.PublishingProfile.EndOfLifeDate) {
		add("image_expired", "Gallery image version has reached end of life.")
	}
	if d.PurchasePlan != nil || len(d.Features) > 0 || v.SecurityProfile != nil {
		add("image_features", "Gallery licensing or security features require compatibility validation before launch.")
	}
	replicated := false
	if v.ReplicationStatus != nil {
		for _, r := range v.ReplicationStatus.Summary {
			if r != nil && strings.EqualFold(azureValue(r.Region), p.region) && azureValue(r.State) == "Completed" {
				replicated = true
			}
		}
	}
	if !replicated {
		add("image_replication", "The pinned gallery version is not confirmed replicated in the target region.")
	}
	if v.StorageProfile == nil || v.StorageProfile.OSDiskImage == nil {
		add("image_storage", "Gallery image OS disk metadata is missing.")
	} else {
		if len(v.StorageProfile.DataDiskImages) > 0 {
			add("image_storage", "Additional image disks require an explicit storage plan.")
		}
		if v.StorageProfile.OSDiskImage.SizeInGB != nil {
			out.MinimumDiskGB = *v.StorageProfile.OSDiskImage.SizeInGB
		}
		if out.MinimumDiskGB <= 0 || s.DiskGB < out.MinimumDiskGB {
			add("disk_size", "The requested root disk is smaller than the gallery image or its size is unknown.")
		}
	}
	return nil
}
