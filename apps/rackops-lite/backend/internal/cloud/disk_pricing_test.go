package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/pricing"
)

func TestAWSDiskQuoteGP3Selection(t *testing.T) {
	for _, mode := range []string{"base", "wrong-volume", "wrong-region", "missing-price", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			client := pricingFixture(func(in *pricing.GetProductsInput) (*pricing.GetProductsOutput, error) {
				attributes := map[string]string{"regionCode": "us-east-1", "volumeApiName": "gp3", "locationType": "AWS Region"}
				switch mode {
				case "wrong-volume":
					attributes["volumeApiName"] = "gp2"
				case "wrong-region":
					attributes["regionCode"] = "eu-west-1"
				}
				effective := time.Now().Add(-time.Hour)
				price := map[string]string{"USD": "0.08"}
				if mode == "missing-price" {
					price["USD"] = "0"
				}
				term := map[string]any{"sku": "sku", "effectiveDate": effective, "priceDimensions": map[string]any{"rate": map[string]any{"rateCode": "rate", "unit": "GB-Mo", "beginRange": "0", "endRange": "Inf", "pricePerUnit": price}}}
				raw, _ := json.Marshal(map[string]any{"serviceCode": "AmazonEC2", "product": map[string]any{"sku": "sku", "productFamily": "Storage", "attributes": attributes}, "terms": map[string]any{"OnDemand": map[string]any{"term": term}}})
				response := &pricing.GetProductsOutput{PriceList: []string{string(raw)}}
				if mode == "ambiguous" {
					response.PriceList = append(response.PriceList, string(raw))
				}
				return response, nil
			})
			quote, err := awsDiskQuote(context.Background(), client, "us-east-1", 30, time.Now())
			valid := mode == "base"
			if (err == nil) != valid {
				t.Fatalf("unexpected result: %v", err)
			}
			if valid {
				// 0.08 * 30 = 2.40
				if quote.Monthly != 2.4 || quote.DiskType != "gp3" || quote.DiskGB != 30 || quote.MeterID != "rate" || len(quote.Exclusions) == 0 {
					t.Fatalf("incorrect disk quote: %+v", quote)
				}
			}
		})
	}
}

func TestAzureDiskTierSelection(t *testing.T) {
	for _, tc := range []struct {
		diskGB   int64
		expected string
	}{
		{4, "E1"}, {8, "E2"}, {16, "E3"}, {30, "E4"}, {64, "E6"},
		{100, "E10"}, {200, "E15"}, {500, "E20"}, {1000, "E30"},
		{2000, "E40"}, {4000, "E50"}, {8000, "E60"},
	} {
		tier, err := azureDiskTierForSize(tc.diskGB)
		if err != nil || tier.name != tc.expected {
			t.Fatalf("diskGB=%d: expected %s, got %v (err=%v)", tc.diskGB, tc.expected, tier.name, err)
		}
	}
	if _, err := azureDiskTierForSize(40000); err == nil {
		t.Fatal("oversized disk accepted")
	}
}

func TestAzureDiskQuoteStandardSSD(t *testing.T) {
	for _, mode := range []string{"base", "wrong-region", "wrong-unit", "wrong-service", "wrong-product", "wrong-tier", "wrong-sku", "minimum", "missing", "pagination", "foreign-next", "loop", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			calls := 0
			client := &http.Client{Transport: cloudTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Hostname() != "prices.azure.com" {
					t.Fatal("wrong pricing host")
				}
				item := azureRetailItem{Currency: "USD", Price: 7.68, Region: "eastus", Effective: now.Add(-time.Hour), MeterID: "disk-meter", Meter: "E10 LRS Disk", Product: "Standard SSD Managed Disks", SKU: "E10 LRS", Service: "Storage", Unit: "1/Month", Type: "Consumption", Primary: true, Size: ""}
				switch mode {
				case "wrong-product":
					item.Product = "Premium SSD Managed Disks"
				case "wrong-tier":
					item.Meter = "E20 LRS Disk"
				case "wrong-sku":
					item.SKU = "E10 ZRS"
				case "minimum":
					item.Minimum = 10
				case "wrong-region":
					item.Region = "westus"
				case "wrong-unit":
					item.Unit = "1 Hour"
				case "wrong-service":
					item.Service = "Compute"
				case "missing":
					item.Price = 0
				}
				items := []azureRetailItem{item}
				next := ""
				if mode == "pagination" && calls == 1 {
					items = nil
					next = "https://prices.azure.com:443/api/retail/prices?$skip=1000"
				}
				if mode == "foreign-next" {
					next = "https://untrusted.invalid/api/retail/prices"
				}
				if mode == "loop" {
					next = r.URL.String()
				}
				if mode == "ambiguous" {
					other := item
					other.MeterID = "another"
					items = append(items, other)
				}
				raw, _ := json.Marshal(map[string]any{"Items": items, "NextPageLink": next})
				return jsonResponse(r, string(raw)), nil
			})}
			quote, err := azureDiskQuote(context.Background(), client, "eastus", 100, now)
			valid := mode == "base" || mode == "pagination"
			if (err == nil) != valid {
				t.Fatalf("unexpected result: %v", err)
			}
			if valid {
				// Azure charges per tier, not per GB: E10 = $7.68/month for up to 128 GB
				if quote.Monthly != 7.68 || quote.DiskType != "StandardSSD_LRS E10" || quote.DiskGB != 100 || quote.MeterID != "disk-meter" || len(quote.Exclusions) == 0 {
					t.Fatalf("incorrect disk quote: %+v", quote)
				}
			}
		})
	}
}

func TestDiskPriceRejectsTrailingJunk(t *testing.T) {
	if _, err := parseFloat("0.08garbage"); err == nil {
		t.Fatal("accepted malformed price")
	}
	if _, err := azureDiskTierForSize(0); err == nil {
		t.Fatal("accepted zero disk size")
	}
}
