package cloud

import (
	"context"
	"errors"
	"google.golang.org/api/cloudbilling/v1"
	"testing"
	"time"
)

func TestGCPComputePricingMapping(t *testing.T) {
	for _, mode := range []string{"base", "pagination", "missing-ram", "wrong-region", "spot", "custom", "currency", "unit", "tier", "future", "negative", "duplicate", "denied", "loop", "wrong-size"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			list := func(context.Context, string) (*cloudbilling.ListSkusResponse, error) {
				calls++
				if mode == "denied" {
					return nil, errors.New("permission denied")
				}
				makeSKU := func(kind, unit string, nanos int64) *cloudbilling.Sku {
					return &cloudbilling.Sku{Name: computeBillingService + "/skus/" + kind, SkuId: kind, Description: "N2 Instance " + kind + " running in Americas", ServiceProviderName: "Google", ServiceRegions: []string{"us-central1"}, Category: &cloudbilling.Category{ResourceFamily: "Compute", UsageType: "OnDemand"}, PricingInfo: []*cloudbilling.PricingInfo{{EffectiveTime: time.Now().Add(-time.Hour).Format(time.RFC3339), PricingExpression: &cloudbilling.PricingExpression{UsageUnit: unit, TieredRates: []*cloudbilling.TierRate{{UnitPrice: &cloudbilling.Money{CurrencyCode: "USD", Nanos: nanos}}}}}}}
				}
				cpu := makeSKU("Core", "h", 30000000)
				ram := makeSKU("Ram", "GiBy.h", 4000000)
				switch mode {
				case "wrong-region":
					cpu.ServiceRegions = []string{"us-east1"}
				case "spot":
					cpu.Category.UsageType = "Preemptible"
				case "custom":
					cpu.Description = "N2 Custom Instance Core running in Americas"
				case "currency":
					cpu.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.CurrencyCode = "EUR"
				case "unit":
					ram.PricingInfo[0].PricingExpression.UsageUnit = "GiBy"
				case "tier":
					cpu.PricingInfo[0].PricingExpression.TieredRates[0].StartUsageAmount = 1
				case "future":
					cpu.PricingInfo[0].EffectiveTime = time.Now().Add(time.Hour).Format(time.RFC3339)
				case "negative":
					cpu.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos = -1
				}
				out := &cloudbilling.ListSkusResponse{Skus: []*cloudbilling.Sku{cpu, ram}}
				if mode == "missing-ram" {
					out.Skus = out.Skus[:1]
				}
				if mode == "duplicate" {
					out.Skus = append(out.Skus, cpu)
				}
				if mode == "pagination" && calls == 1 {
					out.Skus = nil
					out.NextPageToken = "next"
				}
				if mode == "loop" {
					out.Skus = nil
					out.NextPageToken = "next"
				}
				return out, nil
			}
			inspection := MachineInspection{Region: "us-central1", Architecture: "amd64", VCPUs: 2, MemoryMB: 8192}
			if mode == "wrong-size" {
				inspection.VCPUs = 4
			}
			quote, err := gcpComputeQuote(context.Background(), list, "us-central1", "n2-standard-2", inspection, time.Now())
			valid := mode == "base" || mode == "pagination"
			if (err == nil) != valid {
				t.Fatalf("unexpected result %v", err)
			}
			if valid && (quote.Monthly != 67.16 || quote.MeterID != "Core,Ram" || quote.Provider != "gcp") {
				t.Fatalf("wrong quote %+v", quote)
			}
			if mode == "wrong-size" && calls != 0 {
				t.Fatal("unsupported shape queried catalog")
			}
		})
	}
}

func TestGCPComputePricingE2Family(t *testing.T) {
	list := func(context.Context, string) (*cloudbilling.ListSkusResponse, error) {
		makeSKU := func(kind, unit string, nanos int64) *cloudbilling.Sku {
			return &cloudbilling.Sku{Name: computeBillingService + "/skus/e2-" + kind, SkuId: "e2-" + kind, Description: "E2 Instance " + kind + " running in Americas", ServiceProviderName: "Google", ServiceRegions: []string{"us-central1"}, Category: &cloudbilling.Category{ResourceFamily: "Compute", UsageType: "OnDemand"}, PricingInfo: []*cloudbilling.PricingInfo{{EffectiveTime: time.Now().Add(-time.Hour).Format(time.RFC3339), PricingExpression: &cloudbilling.PricingExpression{UsageUnit: unit, TieredRates: []*cloudbilling.TierRate{{UnitPrice: &cloudbilling.Money{CurrencyCode: "USD", Nanos: nanos}}}}}}}
		}
		return &cloudbilling.ListSkusResponse{Skus: []*cloudbilling.Sku{
			makeSKU("Core", "h", 22000000),
			makeSKU("Ram", "GiBy.h", 3000000),
		}}, nil
	}
	// e2-standard-4: 4 vCPUs, 16 GiB (4 GiB/vCPU)
	inspection := MachineInspection{Region: "us-central1", Architecture: "amd64", VCPUs: 4, MemoryMB: 16384}
	quote, err := gcpComputeQuote(context.Background(), list, "us-central1", "e2-standard-4", inspection, time.Now())
	if err != nil {
		t.Fatalf("E2 pricing failed: %v", err)
	}
	// hourly = 0.022*4 + 0.003*16 = 0.088+0.048 = 0.136; monthly = round(0.136*73000)/100 = 99.28
	if quote.Monthly != 99.28 || quote.Provider != "gcp" || quote.Size != "e2-standard-4" || quote.MeterID != "e2-Core,e2-Ram" {
		t.Fatalf("wrong E2 quote: %+v", quote)
	}
}

func TestGCPComputePricingN2DFamily(t *testing.T) {
	list := func(context.Context, string) (*cloudbilling.ListSkusResponse, error) {
		makeSKU := func(kind, unit string, nanos int64) *cloudbilling.Sku {
			return &cloudbilling.Sku{Name: computeBillingService + "/skus/n2d-" + kind, SkuId: "n2d-" + kind, Description: "N2D AMD Instance " + kind + " running in Americas", ServiceProviderName: "Google", ServiceRegions: []string{"us-central1"}, Category: &cloudbilling.Category{ResourceFamily: "Compute", UsageType: "OnDemand"}, PricingInfo: []*cloudbilling.PricingInfo{{EffectiveTime: time.Now().Add(-time.Hour).Format(time.RFC3339), PricingExpression: &cloudbilling.PricingExpression{UsageUnit: unit, TieredRates: []*cloudbilling.TierRate{{UnitPrice: &cloudbilling.Money{CurrencyCode: "USD", Nanos: nanos}}}}}}}
		}
		return &cloudbilling.ListSkusResponse{Skus: []*cloudbilling.Sku{
			makeSKU("Core", "h", 25000000),
			makeSKU("Ram", "GiBy.h", 3500000),
		}}, nil
	}
	// n2d-standard-8: 8 vCPUs, 32 GiB (4 GiB/vCPU)
	inspection := MachineInspection{Region: "us-central1", Architecture: "amd64", VCPUs: 8, MemoryMB: 32768}
	quote, err := gcpComputeQuote(context.Background(), list, "us-central1", "n2d-standard-8", inspection, time.Now())
	if err != nil {
		t.Fatalf("N2D pricing failed: %v", err)
	}
	// hourly = 0.025*8 + 0.0035*32 = 0.200+0.112 = 0.312; monthly = round(0.312*73000)/100 = 227.76
	if quote.Monthly != 227.76 || quote.Provider != "gcp" || quote.Size != "n2d-standard-8" || quote.MeterID != "n2d-Core,n2d-Ram" {
		t.Fatalf("wrong N2D quote: %+v", quote)
	}
}

func TestGCPComputePricingRejectsUnsupportedFamilies(t *testing.T) {
	neverCalled := func(context.Context, string) (*cloudbilling.ListSkusResponse, error) {
		t.Fatal("catalog queried for unsupported family")
		return nil, nil
	}
	for _, size := range []string{"c3-standard-4", "n2-highmem-4", "n2-highcpu-4", "n2-custom-4-8192", "t2a-standard-4", "e2-micro"} {
		t.Run(size, func(t *testing.T) {
			inspection := MachineInspection{Region: "us-central1", Architecture: "amd64", VCPUs: 4, MemoryMB: 16384}
			_, err := gcpComputeQuote(context.Background(), neverCalled, "us-central1", size, inspection, time.Now())
			if err == nil {
				t.Fatal("unsupported family accepted")
			}
		})
	}
}

