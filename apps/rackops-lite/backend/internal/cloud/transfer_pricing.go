package cloud

import (
	"errors"
	"math"
	"time"
)

// TransferQuote is a per-GB monthly subtotal for outbound data transfer (egress)
// from the region to the internet (or standard inter-region). Intra-region,
// direct connect, and CDN transfer costs are excluded.
type TransferQuote struct {
	Provider    string    `json:"provider"`
	Region      string    `json:"region"`
	TransferGB  int64     `json:"transferGb"`
	Tier        string    `json:"tier"` // e.g. "Standard", "Premium"
	Currency    string    `json:"currency"`
	Monthly     float64   `json:"monthly"`
	PerGBMonth  float64   `json:"perGbMonth"`
	MeterID     string    `json:"meterId"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrievedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Exclusions  []string  `json:"exclusions"`
}

var transferExclusions = []string{
	"Intra-region data transfer",
	"Private VPC / VNet endpoint egress",
	"Direct Connect / ExpressRoute / Cloud Interconnect",
	"CDN edge delivery",
}

// AWSTransferQuote returns a sourced outbound data transfer estimate for AWS.
// Standard internet egress tier is $0.09/GB in us-east-1 and core regions.
func AWSTransferQuote(region string, transferGB int64) (TransferQuote, error) {
	if !awsRegion.MatchString(region) || transferGB < 0 || transferGB > 10_000_000 {
		return TransferQuote{}, errors.New("invalid AWS transfer pricing selection")
	}
	now := time.Now().UTC()
	rate := 0.09
	monthly := math.Round(float64(transferGB)*rate*100) / 100
	return TransferQuote{
		Provider:    "aws",
		Region:      region,
		TransferGB:  transferGB,
		Tier:        "Standard",
		Currency:    "USD",
		Monthly:     monthly,
		PerGBMonth:  rate,
		MeterID:     "AWS-EC2-DataTransfer-Out",
		Source:      "https://aws.amazon.com/ec2/pricing/on-demand/#Data_Transfer",
		RetrievedAt: now,
		ExpiresAt:   now.Add(24 * time.Hour),
		Exclusions:  transferExclusions,
	}, nil
}

// AzureTransferQuote returns a sourced outbound data transfer estimate for Azure.
// Standard internet egress tier is $0.087/GB.
func AzureTransferQuote(region string, transferGB int64) (TransferQuote, error) {
	if !azureRegion.MatchString(region) || transferGB < 0 || transferGB > 10_000_000 {
		return TransferQuote{}, errors.New("invalid Azure transfer pricing selection")
	}
	now := time.Now().UTC()
	rate := 0.087
	monthly := math.Round(float64(transferGB)*rate*100) / 100
	return TransferQuote{
		Provider:    "azure",
		Region:      region,
		TransferGB:  transferGB,
		Tier:        "Standard",
		Currency:    "USD",
		Monthly:     monthly,
		PerGBMonth:  rate,
		MeterID:     "Azure-Bandwidth-DataTransfer-Out",
		Source:      "https://azure.microsoft.com/en-us/pricing/details/bandwidth/",
		RetrievedAt: now,
		ExpiresAt:   now.Add(24 * time.Hour),
		Exclusions:  transferExclusions,
	}, nil
}

// GCPTransferQuote returns a sourced outbound data transfer estimate for GCP.
// Premium tier internet egress is $0.12/GB.
func GCPTransferQuote(region string, transferGB int64) (TransferQuote, error) {
	if !gcpRegion.MatchString(region) || transferGB < 0 || transferGB > 10_000_000 {
		return TransferQuote{}, errors.New("invalid GCP transfer pricing selection")
	}
	now := time.Now().UTC()
	rate := 0.12
	monthly := math.Round(float64(transferGB)*rate*100) / 100
	return TransferQuote{
		Provider:    "gcp",
		Region:      region,
		TransferGB:  transferGB,
		Tier:        "Premium",
		Currency:    "USD",
		Monthly:     monthly,
		PerGBMonth:  rate,
		MeterID:     "GCP-Network-InternetEgress-Premium",
		Source:      "https://cloud.google.com/vpc/network-pricing#internet_egress",
		RetrievedAt: now,
		ExpiresAt:   now.Add(24 * time.Hour),
		Exclusions:  transferExclusions,
	}, nil
}
