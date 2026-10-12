package cloud

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/option"
)

const computeBillingService = "services/6F81-5844-456A"

// The initial mapping is deliberately limited to predefined N2 standard VMs.
// Other families need their own verified mapping, not a guessed CPU/RAM price.
func GCPComputeQuote(ctx context.Context, region, size string, c GCPCredentials, inspection MachineInspection) (ComputeQuote, error) {
	if !gcpRegion.MatchString(region) {
		return ComputeQuote{}, errors.New("invalid pricing region")
	}
	config, err := gcpJWT(c)
	if err != nil {
		return ComputeQuote{}, err
	}
	service, err := cloudbilling.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx)))
	if err != nil {
		return ComputeQuote{}, err
	}
	return gcpComputeQuote(ctx, func(ctx context.Context, token string) (*cloudbilling.ListSkusResponse, error) {
		return service.Services.Skus.List(computeBillingService).CurrencyCode("USD").PageSize(5000).PageToken(token).Context(ctx).Do()
	}, region, size, inspection, time.Now().UTC())
}

func gcpComputeQuote(ctx context.Context, list func(context.Context, string) (*cloudbilling.ListSkusResponse, error), region, size string, inspection MachineInspection, now time.Time) (ComputeQuote, error) {
	family, count, err := gcpParsePricingSize(size)
	if err != nil || inspection.VCPUs != int32(count) || inspection.MemoryMB != int64(count)*int64(family.memGBPerVCPU)*1024 || inspection.Region != region || inspection.Architecture != "amd64" {
		return ComputeQuote{}, errors.New("pricing supports inspected E2, N2 and N2D standard amd64 VMs only")
	}
	rates := map[string]float64{}
	ids := map[string]string{}
	effective := now
	token := ""
	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page >= 100 {
			return ComputeQuote{}, errors.New("incomplete compute catalog")
		}
		result, err := list(ctx, token)
		if err != nil {
			return ComputeQuote{}, err
		}
		if result == nil {
			return ComputeQuote{}, errors.New("missing compute catalog")
		}
		for _, sku := range result.Skus {
			if sku == nil || sku.Category == nil || sku.Category.ResourceFamily != "Compute" || sku.Category.UsageType != "OnDemand" || sku.ServiceProviderName != "Google" {
				continue
			}
			found := false
			for _, r := range sku.ServiceRegions {
				if r == region {
					found = true
				}
			}
			if !found {
				continue
			}
			kind := ""
			unit := ""
			if strings.HasPrefix(sku.Description, family.cpuPrefix) {
				kind = "cpu"
				unit = "h"
			}
			if strings.HasPrefix(sku.Description, family.ramPrefix) {
				kind = "ram"
				unit = "GiBy.h"
			}
			if kind == "" {
				continue
			}
			if sku.SkuId == "" || sku.Name != computeBillingService+"/skus/"+sku.SkuId || ids[kind] != "" {
				return ComputeQuote{}, errors.New("ambiguous compute SKU")
			}
			var chosen *cloudbilling.PricingInfo
			var chosenAt time.Time
			for _, info := range sku.PricingInfo {
				if info == nil {
					continue
				}
				at, e := time.Parse(time.RFC3339, info.EffectiveTime)
				if e != nil {
					return ComputeQuote{}, errors.New("invalid price date")
				}
				if at.After(now) {
					continue
				}
				if at.Equal(chosenAt) {
					return ComputeQuote{}, errors.New("ambiguous effective price")
				}
				if chosen == nil || at.After(chosenAt) {
					chosen = info
					chosenAt = at
				}
			}
			if chosen == nil || chosen.PricingExpression == nil {
				return ComputeQuote{}, errors.New("current price unavailable")
			}
			expression := chosen.PricingExpression
			if expression.UsageUnit != unit || len(expression.TieredRates) != 1 || expression.TieredRates[0] == nil || expression.TieredRates[0].StartUsageAmount != 0 {
				return ComputeQuote{}, errors.New("unsupported compute price tiers or units")
			}
			money := expression.TieredRates[0].UnitPrice
			if money == nil || money.CurrencyCode != "USD" || money.Units < 0 || money.Nanos < 0 || money.Nanos >= 1000000000 {
				return ComputeQuote{}, errors.New("invalid compute money")
			}
			price := float64(money.Units) + float64(money.Nanos)/1e9
			if price <= 0 {
				return ComputeQuote{}, errors.New("missing positive compute price")
			}
			rates[kind] = price
			ids[kind] = sku.SkuId
			if chosenAt.Before(effective) {
				effective = chosenAt
			}
		}
		token = result.NextPageToken
		if token == "" {
			break
		}
		if seen[token] {
			return ComputeQuote{}, errors.New("repeated catalog cursor")
		}
		seen[token] = true
	}
	if len(rates) != 2 {
		return ComputeQuote{}, errors.New("CPU or memory price unavailable")
	}
	hourly := rates["cpu"]*float64(count) + rates["ram"]*float64(inspection.MemoryMB)/1024
	if math.IsInf(hourly*73000, 0) || math.IsNaN(hourly) {
		return ComputeQuote{}, errors.New("compute price out of range")
	}
	return ComputeQuote{Provider: "gcp", Region: region, Size: size, Currency: "USD", Hourly: hourly, Monthly: math.Round(hourly*73000) / 100, HoursPerMonth: 730, MeterID: ids["cpu"] + "," + ids["ram"], Source: "https://cloud.google.com/skus", RetrievedAt: now, EffectiveAt: effective, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"Persistent disks, snapshots and disk performance charges", "Network resources and traffic transfer", "Paid image licenses, support and taxes", "Sustained-use, committed-use and account-specific discounts", "Temporary duplicate resources during migration"}}, nil
}

// gcpPricingFamily maps a predefined standard VM family to its catalog SKU
// description prefixes and known memory ratio. Custom, highmem, highcpu and
// other shapes require their own verified mapping and are explicitly excluded.
type gcpPricingFamily struct {
	prefix       string // e.g. "n2-standard-"
	cpuPrefix    string // e.g. "N2 Instance Core running in "
	ramPrefix    string // e.g. "N2 Instance Ram running in "
	memGBPerVCPU int    // GiB per vCPU for the standard tier
	minVCPUs     int
	maxVCPUs     int
}

var gcpPricingFamilies = []gcpPricingFamily{
	{prefix: "e2-standard-", cpuPrefix: "E2 Instance Core running in ", ramPrefix: "E2 Instance Ram running in ", memGBPerVCPU: 4, minVCPUs: 2, maxVCPUs: 32},
	{prefix: "n2-standard-", cpuPrefix: "N2 Instance Core running in ", ramPrefix: "N2 Instance Ram running in ", memGBPerVCPU: 4, minVCPUs: 2, maxVCPUs: 128},
	{prefix: "n2d-standard-", cpuPrefix: "N2D AMD Instance Core running in ", ramPrefix: "N2D AMD Instance Ram running in ", memGBPerVCPU: 4, minVCPUs: 2, maxVCPUs: 224},
}

func gcpParsePricingSize(size string) (gcpPricingFamily, int, error) {
	for _, f := range gcpPricingFamilies {
		if strings.HasPrefix(size, f.prefix) {
			count, err := strconv.Atoi(strings.TrimPrefix(size, f.prefix))
			if err != nil || count < f.minVCPUs || count > f.maxVCPUs {
				return gcpPricingFamily{}, 0, errors.New("unsupported vCPU count")
			}
			return f, count, nil
		}
	}
	return gcpPricingFamily{}, 0, errors.New("unsupported GCP machine family")
}

