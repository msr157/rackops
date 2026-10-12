package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAzureComputePricingRejectsWrongProductsAndIncompleteCatalog(t *testing.T) {
	for _, mode := range []string{"base", "arm-sku-name", "wrong-meter", "windows", "spot", "reserved", "region", "currency", "unit", "future", "ambiguous", "conflict", "pagination", "foreign-next", "loop", "zero", "missing-date"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			now := time.Now().UTC()
			client := &http.Client{Transport: cloudTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Hostname() != "prices.azure.com" || r.Header.Get("Authorization") != "" || r.Method != "GET" {
					t.Fatal("unsafe public pricing request")
				}
				if calls == 1 && !strings.Contains(r.URL.Query().Get("$filter"), "armSkuName eq 'Standard_B2s'") {
					t.Fatal("missing exact SKU filter")
				}
				item := azureRetailItem{Currency: "USD", Price: 0.05, Region: "eastus", Effective: now.Add(-time.Hour), MeterID: "meter", Meter: "B2s", Product: "Virtual Machines BS Series", SKU: "B2s", Service: "Virtual Machines", Unit: "1 Hour", Type: "Consumption", Primary: true, Size: "Standard_B2s"}
				switch mode {
				case "arm-sku-name":
					item.SKU = "Standard_B2s"
				case "wrong-meter":
					item.Meter = "B2s Low Priority"
				case "windows":
					item.Product += " Windows"
				case "spot":
					item.SKU += " Spot"
					item.Meter = item.SKU
				case "reserved":
					item.Type = "Reservation"
				case "region":
					item.Region = "westus"
				case "currency":
					item.Currency = "EUR"
				case "unit":
					item.Unit = "1 Month"
				case "future":
					item.Effective = now.Add(time.Hour)
				case "zero":
					item.Price = 0
				case "missing-date":
					item.Effective = time.Time{}
				}
				items := []azureRetailItem{item}
				next := ""
				if mode == "ambiguous" {
					other := item
					other.MeterID = "another"
					items = append(items, other)
				}
				if mode == "conflict" {
					other := item
					other.Price = 0.1
					items = append(items, other)
				}
				if mode == "pagination" && calls == 1 {
					items = nil
					next = "https://prices.azure.com:443/api/retail/prices?$skip=1000"
				}
				if mode == "foreign-next" {
					next = "https://attacker.invalid/api/retail/prices"
				}
				if mode == "loop" {
					next = r.URL.String()
				}
				raw, _ := json.Marshal(map[string]any{"Items": items, "NextPageLink": next})
				return jsonResponse(r, string(raw)), nil
			})}
			quote, err := azureComputeQuote(context.Background(), client, "eastus", "Standard_B2s", now)
			valid := mode == "base" || mode == "pagination" || mode == "arm-sku-name"
			if (err == nil) != valid {
				t.Fatalf("unexpected pricing result: %v", err)
			}
			if valid && (quote.Monthly != 36.5 || quote.Currency != "USD" || quote.HoursPerMonth != 730 || len(quote.Exclusions) == 0 || quote.Source == "" || quote.MeterID != "meter") {
				t.Fatal("incorrect or unattributed estimate")
			}
			if mode == "foreign-next" && calls != 1 {
				t.Fatal("followed foreign pagination")
			}
		})
	}
}

// Matches the public eastus catalog's Standard_D2s_v5 naming observed on
// 2026-09-07: the SKU uses the ARM name, while the meter uses spaces.
func TestAzureComputePricingARMSKUWithSpacedMeter(t *testing.T) {
	client := &http.Client{Transport: cloudTransport(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, `{"Items":[{"currencyCode":"USD","retailPrice":0.096,"armRegionName":"eastus","effectiveStartDate":"2021-11-01T00:00:00Z","meterId":"fixture-meter","meterName":"D2s v5","productName":"Virtual Machines Dsv5 Series","skuName":"Standard_D2s_v5","serviceName":"Virtual Machines","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true,"armSkuName":"Standard_D2s_v5"}]}`), nil
	})}
	quote, err := azureComputeQuote(context.Background(), client, "eastus", "Standard_D2s_v5", time.Now().UTC())
	if err != nil || quote.Hourly != 0.096 || quote.Monthly != 70.08 {
		t.Fatalf("catalog naming not matched: %+v, %v", quote, err)
	}
}
