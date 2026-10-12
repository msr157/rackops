package cloud

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/option"
)

// GCPDiskQuote prices only the zonal pd-balanced root disk created by GCPMachineSpec.
func GCPDiskQuote(ctx context.Context, region string, diskGB int64, credentials GCPCredentials) (DiskQuote, error) {
	if !gcpRegion.MatchString(region) || diskGB < 1 || diskGB > 65536 {
		return DiskQuote{}, errors.New("invalid GCP disk selection")
	}
	config, err := gcpJWT(credentials)
	if err != nil {
		return DiskQuote{}, err
	}
	service, err := cloudbilling.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx)))
	if err != nil {
		return DiskQuote{}, err
	}
	return gcpDiskQuote(ctx, func(ctx context.Context, token string) (*cloudbilling.ListSkusResponse, error) {
		return service.Services.Skus.List(computeBillingService).CurrencyCode("USD").PageSize(5000).PageToken(token).Context(ctx).Do()
	}, region, diskGB, time.Now().UTC())
}

func gcpDiskQuote(ctx context.Context, list func(context.Context, string) (*cloudbilling.ListSkusResponse, error), region string, diskGB int64, now time.Time) (DiskQuote, error) {
	if !gcpRegion.MatchString(region) || diskGB < 1 || diskGB > 65536 {
		return DiskQuote{}, errors.New("invalid GCP disk selection")
	}
	token, meter := "", ""
	seen := map[string]bool{}
	var rate float64
	for page := 0; ; page++ {
		if page >= 100 {
			return DiskQuote{}, errors.New("incomplete disk catalog")
		}
		result, err := list(ctx, token)
		if err != nil {
			return DiskQuote{}, err
		}
		if result == nil {
			return DiskQuote{}, errors.New("missing disk catalog")
		}
		for _, sku := range result.Skus {
			if sku == nil || sku.Category == nil || sku.Category.ResourceFamily != "Storage" || sku.Category.UsageType != "OnDemand" || sku.ServiceProviderName != "Google" || !slices.Contains(sku.ServiceRegions, region) {
				continue
			}
			// Regional replicated disks have a different description and price.
			if sku.Description != "Balanced PD Capacity" && !strings.HasPrefix(sku.Description, "Balanced PD Capacity in ") {
				continue
			}
			if sku.SkuId == "" || sku.Name != computeBillingService+"/skus/"+sku.SkuId || meter != "" {
				return DiskQuote{}, errors.New("ambiguous disk SKU")
			}
			var chosen *cloudbilling.PricingInfo
			var latest time.Time
			for _, info := range sku.PricingInfo {
				if info == nil {
					continue
				}
				at, err := time.Parse(time.RFC3339, info.EffectiveTime)
				if err != nil {
					return DiskQuote{}, errors.New("invalid disk price date")
				}
				if at.After(now) {
					continue
				}
				if at.Equal(latest) {
					return DiskQuote{}, errors.New("ambiguous disk price date")
				}
				if chosen == nil || at.After(latest) {
					chosen, latest = info, at
				}
			}
			if chosen == nil || chosen.PricingExpression == nil {
				return DiskQuote{}, errors.New("current disk price unavailable")
			}
			expression := chosen.PricingExpression
			if expression.UsageUnit != "GiBy.mo" || len(expression.TieredRates) != 1 || expression.TieredRates[0] == nil || expression.TieredRates[0].StartUsageAmount != 0 {
				return DiskQuote{}, errors.New("unsupported disk price units or tiers")
			}
			money := expression.TieredRates[0].UnitPrice
			if money == nil || money.CurrencyCode != "USD" || money.Units < 0 || money.Nanos < 0 || money.Nanos >= 1e9 {
				return DiskQuote{}, errors.New("invalid disk money")
			}
			rate = float64(money.Units) + float64(money.Nanos)/1e9
			if rate <= 0 || math.IsInf(rate*float64(diskGB)*100, 0) {
				return DiskQuote{}, errors.New("invalid disk price")
			}
			meter = sku.SkuId
		}
		token = result.NextPageToken
		if token == "" {
			break
		}
		if seen[token] {
			return DiskQuote{}, errors.New("repeated disk catalog cursor")
		}
		seen[token] = true
	}
	if meter == "" {
		return DiskQuote{}, errors.New("zonal balanced disk price unavailable")
	}
	return DiskQuote{Provider: "gcp", Region: region, DiskType: "pd-balanced (zonal)", DiskGB: diskGB, Currency: "USD", Monthly: math.Round(rate*float64(diskGB)*100) / 100, PerGBMonth: rate, MeterID: meter, Source: "https://cloud.google.com/skus", RetrievedAt: now, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"Additional data disks and snapshots", "Compute, network resources and traffic transfer", "Account discounts, support and taxes", "Temporary duplicate storage during migration"}}, nil
}
