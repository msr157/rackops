package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ComputeQuote struct {
	Provider      string    `json:"provider"`
	Region        string    `json:"region"`
	Size          string    `json:"size"`
	Currency      string    `json:"currency"`
	Hourly        float64   `json:"hourly"`
	Monthly       float64   `json:"monthly"`
	HoursPerMonth int       `json:"hoursPerMonth"`
	MeterID       string    `json:"meterId"`
	Source        string    `json:"source"`
	RetrievedAt   time.Time `json:"retrievedAt"`
	EffectiveAt   time.Time `json:"effectiveAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Exclusions    []string  `json:"exclusions"`
}

type azureRetailItem struct {
	Currency  string    `json:"currencyCode"`
	Minimum   float64   `json:"tierMinimumUnits"`
	Price     float64   `json:"retailPrice"`
	Region    string    `json:"armRegionName"`
	Effective time.Time `json:"effectiveStartDate"`
	MeterID   string    `json:"meterId"`
	Meter     string    `json:"meterName"`
	Product   string    `json:"productName"`
	SKU       string    `json:"skuName"`
	Service   string    `json:"serviceName"`
	Unit      string    `json:"unitOfMeasure"`
	Type      string    `json:"type"`
	Primary   bool      `json:"isPrimaryMeterRegion"`
	Size      string    `json:"armSkuName"`
}

func AzureComputeQuote(ctx context.Context, region, size string) (ComputeQuote, error) {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("pricing redirect refused") }}
	return azureComputeQuote(ctx, client, region, size, time.Now().UTC())
}

func azureComputeQuote(ctx context.Context, client *http.Client, region, size string, now time.Time) (ComputeQuote, error) {
	if !azureRegion.MatchString(region) || !azureGroup.MatchString(size) {
		return ComputeQuote{}, errors.New("invalid pricing selection")
	}
	values := url.Values{"currencyCode": {"'USD'"}, "api-version": {"2023-01-01-preview"}, "$filter": {fmt.Sprintf("serviceName eq 'Virtual Machines' and armRegionName eq '%s' and armSkuName eq '%s' and priceType eq 'Consumption'", region, size)}}
	source := "https://prices.azure.com/api/retail/prices?" + values.Encode()
	next := source
	seen := map[string]bool{}
	candidates := map[string]azureRetailItem{}
	for page := 0; next != ""; page++ {
		parsed, err := url.Parse(next)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "prices.azure.com" || (parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil || parsed.Path != "/api/retail/prices" || parsed.Fragment != "" || seen[next] || page >= 20 {
			return ComputeQuote{}, errors.New("invalid or incomplete pricing pagination")
		}
		seen[next] = true
		req, err := http.NewRequestWithContext(ctx, "GET", next, nil)
		if err != nil {
			return ComputeQuote{}, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return ComputeQuote{}, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != 200 || len(raw) > 2<<20 {
			return ComputeQuote{}, errors.New("pricing response unavailable or oversized")
		}
		var result struct {
			Items []azureRetailItem `json:"Items"`
			Next  string            `json:"NextPageLink"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return ComputeQuote{}, errors.New("invalid pricing response")
		}
		for _, item := range result.Items {
			// Exact region/size and base meter names exclude spot/low-priority and
			// Windows/software bundles. Unrecognized product naming fails closed.
			base := strings.TrimPrefix(size, "Standard_")
			if item.Size != size || item.Region != region || item.Service != "Virtual Machines" || item.Type != "Consumption" || item.Currency != "USD" || item.Unit != "1 Hour" || !item.Primary || item.Minimum != 0 || item.MeterID == "" || item.Effective.IsZero() || item.Effective.After(now) || item.Price <= 0 || math.IsNaN(item.Price) || math.IsInf(item.Price, 0) || !strings.HasPrefix(item.Product, "Virtual Machines ") || !strings.HasSuffix(item.Product, " Series") || strings.Contains(strings.ToLower(item.Product), "windows") || strings.TrimPrefix(strings.ReplaceAll(item.SKU, " ", "_"), "Standard_") != base || strings.ReplaceAll(item.Meter, " ", "_") != base {
				continue
			}
			if old, ok := candidates[item.MeterID]; ok {
				if old.Effective.Equal(item.Effective) && old.Price != item.Price {
					return ComputeQuote{}, errors.New("conflicting retail prices")
				}
				if !item.Effective.After(old.Effective) {
					continue
				}
			}
			candidates[item.MeterID] = item
		}
		next = result.Next
	}
	if len(candidates) != 1 {
		return ComputeQuote{}, errors.New("base VM price missing or ambiguous")
	}
	var price azureRetailItem
	for _, p := range candidates {
		price = p
	}
	if math.IsInf(price.Price*730*100, 0) {
		return ComputeQuote{}, errors.New("retail price outside supported range")
	}
	return ComputeQuote{Provider: "azure", Region: region, Size: size, Currency: "USD", Hourly: price.Price, Monthly: math.Round(price.Price*730*100) / 100, HoursPerMonth: 730, MeterID: price.MeterID, Source: source, RetrievedAt: now, EffectiveAt: price.Effective, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"Managed disks and disk transactions", "Network resources and traffic transfer", "Software licensing and support", "Taxes and account-specific discounts", "Migration downtime and temporary duplicate resources"}}, nil
}
