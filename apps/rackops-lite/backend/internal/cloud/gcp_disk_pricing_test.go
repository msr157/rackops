package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/api/cloudbilling/v1"
)

func TestGCPDiskCatalog(t *testing.T) {
	for _, mode := range []string{"base", "pagination", "regional", "region", "spot", "currency", "unit", "tier", "future", "duplicate", "loop", "denied", "missing", "negative", "invalid-size"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			calls := 0
			list := func(context.Context, string) (*cloudbilling.ListSkusResponse, error) {
				calls++
				if mode == "denied" {
					return nil, errors.New("denied")
				}
				if mode == "missing" {
					return nil, nil
				}
				sku := &cloudbilling.Sku{SkuId: "balanced", Name: computeBillingService + "/skus/balanced", Description: "Balanced PD Capacity", ServiceProviderName: "Google", ServiceRegions: []string{"us-central1"}, Category: &cloudbilling.Category{ResourceFamily: "Storage", UsageType: "OnDemand"}, PricingInfo: []*cloudbilling.PricingInfo{{EffectiveTime: now.Add(-time.Hour).Format(time.RFC3339), PricingExpression: &cloudbilling.PricingExpression{UsageUnit: "GiBy.mo", TieredRates: []*cloudbilling.TierRate{{UnitPrice: &cloudbilling.Money{CurrencyCode: "USD", Nanos: 100000000}}}}}}}
				p := sku.PricingInfo[0].PricingExpression
				switch mode {
				case "regional":
					sku.Description = "Regional Balanced PD Capacity"
				case "region":
					sku.ServiceRegions = []string{"us-east1"}
				case "spot":
					sku.Category.UsageType = "Preemptible"
				case "currency":
					p.TieredRates[0].UnitPrice.CurrencyCode = "EUR"
				case "unit":
					p.UsageUnit = "GiBy.h"
				case "tier":
					p.TieredRates[0].StartUsageAmount = 1
				case "future":
					sku.PricingInfo[0].EffectiveTime = now.Add(time.Hour).Format(time.RFC3339)
				case "negative":
					p.TieredRates[0].UnitPrice.Nanos = -1
				}
				out := &cloudbilling.ListSkusResponse{Skus: []*cloudbilling.Sku{sku}}
				if mode == "duplicate" {
					out.Skus = append(out.Skus, sku)
				}
				if mode == "loop" || (mode == "pagination" && calls == 1) {
					out.Skus = nil
					out.NextPageToken = "next"
				}
				return out, nil
			}
			size := int64(100)
			if mode == "invalid-size" {
				size = 0
			}
			quote, err := gcpDiskQuote(context.Background(), list, "us-central1", size, now)
			valid := mode == "base" || mode == "pagination"
			if (err == nil) != valid {
				t.Fatalf("quote %+v error %v", quote, err)
			}
			if valid && (quote.Monthly != 10 || quote.MeterID != "balanced" || quote.DiskGB != 100 || quote.PerGBMonth != 0.1) {
				t.Fatalf("wrong quote %+v", quote)
			}
			if mode == "invalid-size" && calls != 0 {
				t.Fatal("invalid selection queried catalog")
			}
		})
	}
}
