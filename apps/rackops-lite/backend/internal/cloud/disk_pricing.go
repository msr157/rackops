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
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// DiskQuote is a per-GB monthly subtotal for the selected root volume only.
// It does not include disk transactions, IOPS provisioning, snapshots or
// additional data disks. Compute, network and transfer are separate.
type DiskQuote struct {
	Provider    string    `json:"provider"`
	Region      string    `json:"region"`
	DiskType    string    `json:"diskType"`
	DiskGB      int64     `json:"diskGb"`
	Currency    string    `json:"currency"`
	Monthly     float64   `json:"monthly"`
	PerGBMonth  float64   `json:"perGbMonth"`
	MeterID     string    `json:"meterId"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrievedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Exclusions  []string  `json:"exclusions"`
}

// AWSDiskQuote returns a gp3 EBS root volume estimate. gp3 baseline is 3000
// IOPS and 125 MiB/s; provisioned IOPS and throughput are separate charges
// and excluded.
func AWSDiskQuote(ctx context.Context, region string, diskGB int64, cred AWSCredentials) (DiskQuote, error) {
	if !awsRegion.MatchString(region) || diskGB < 1 || diskGB > 65536 || cred.AccessKeyID == "" || cred.SecretAccessKey == "" {
		return DiskQuote{}, errors.New("invalid AWS disk pricing selection")
	}
	client := pricing.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cred.AccessKeyID, cred.SecretAccessKey, cred.SessionToken)), RetryMaxAttempts: 2, HTTPClient: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("pricing redirect refused") }}})
	return awsDiskQuote(ctx, client, region, diskGB, time.Now().UTC())
}

func awsDiskQuote(ctx context.Context, client awsPricingAPI, region string, diskGB int64, now time.Time) (DiskQuote, error) {
	input := &pricing.GetProductsInput{ServiceCode: aws.String("AmazonEC2"), FormatVersion: aws.String("aws_v1"), MaxResults: aws.Int32(100), Filters: []pricingtypes.Filter{
		{Field: aws.String("regionCode"), Value: aws.String(region), Type: pricingtypes.FilterTypeTermMatch},
		{Field: aws.String("productFamily"), Value: aws.String("Storage"), Type: pricingtypes.FilterTypeTermMatch},
		{Field: aws.String("volumeApiName"), Value: aws.String("gp3"), Type: pricingtypes.FilterTypeTermMatch},
		{Field: aws.String("locationType"), Value: aws.String("AWS Region"), Type: pricingtypes.FilterTypeTermMatch},
	}}
	var perGB float64
	var meterID string
	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page >= 10 {
			return DiskQuote{}, errors.New("incomplete AWS disk pricing pagination")
		}
		response, err := client.GetProducts(ctx, input)
		if err != nil {
			return DiskQuote{}, err
		}
		if response == nil {
			return DiskQuote{}, errors.New("empty disk pricing response")
		}
		for _, raw := range response.PriceList {
			if len(raw) > 1<<20 {
				return DiskQuote{}, errors.New("oversized disk pricing product")
			}
			var product struct {
				ServiceCode string `json:"serviceCode"`
				Product     struct {
					SKU        string            `json:"sku"`
					Family     string            `json:"productFamily"`
					Attributes map[string]string `json:"attributes"`
				} `json:"product"`
				Terms struct {
					OnDemand map[string]struct {
						SKU        string    `json:"sku"`
						Effective  time.Time `json:"effectiveDate"`
						Dimensions map[string]struct {
							Unit  string            `json:"unit"`
							Begin string            `json:"beginRange"`
							End   string            `json:"endRange"`
							Rate  string            `json:"rateCode"`
							Price map[string]string `json:"pricePerUnit"`
						} `json:"priceDimensions"`
					} `json:"OnDemand"`
				} `json:"terms"`
			}
			if json.Unmarshal([]byte(raw), &product) != nil {
				return DiskQuote{}, errors.New("invalid disk pricing product")
			}
			if product.ServiceCode != "AmazonEC2" || product.Product.SKU == "" || product.Product.Family != "Storage" || product.Product.Attributes["locationType"] != "AWS Region" || product.Product.Attributes["volumeApiName"] != "gp3" || product.Product.Attributes["regionCode"] != region {
				continue
			}
			for _, term := range product.Terms.OnDemand {
				if term.SKU != product.Product.SKU || term.Effective.IsZero() || term.Effective.After(now) || len(term.Dimensions) != 1 {
					continue
				}
				for _, dim := range term.Dimensions {
					usd := dim.Price["USD"]
					price, err := parseFloat(usd)
					if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price*float64(diskGB)*100, 0) || dim.Unit != "GB-Mo" || dim.Begin != "0" || dim.End != "Inf" || dim.Rate == "" {
						continue
					}
					if meterID != "" {
						return DiskQuote{}, errors.New("ambiguous gp3 disk price")
					}
					perGB = price
					meterID = dim.Rate
				}
			}
		}
		token := aws.ToString(response.NextToken)
		if token == "" {
			break
		}
		if seen[token] {
			return DiskQuote{}, errors.New("repeated disk pricing cursor")
		}
		seen[token] = true
		input.NextToken = aws.String(token)
	}
	if meterID == "" {
		return DiskQuote{}, errors.New("gp3 volume price unavailable")
	}
	monthly := math.Round(perGB*float64(diskGB)*100) / 100
	return DiskQuote{Provider: "aws", Region: region, DiskType: "gp3", DiskGB: diskGB, Currency: "USD", Monthly: monthly, PerGBMonth: perGB, MeterID: meterID, Source: "https://pricing.us-east-1.amazonaws.com", RetrievedAt: now, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"Provisioned IOPS above gp3 baseline (3000)", "Provisioned throughput above gp3 baseline (125 MiB/s)", "Snapshots and snapshot copy charges", "Additional data disks"}}, nil
}

// AzureDiskQuote returns a Standard SSD (E-series) managed disk estimate for
// the given root disk size. Premium SSD and Ultra require separate estimates.
func AzureDiskQuote(ctx context.Context, region string, diskGB int64) (DiskQuote, error) {
	if !azureRegion.MatchString(region) || diskGB < 1 || diskGB > 65536 {
		return DiskQuote{}, errors.New("invalid Azure disk pricing selection")
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("pricing redirect refused") }}
	return azureDiskQuote(ctx, client, region, diskGB, time.Now().UTC())
}

// azureDiskTier maps the requested disk size to the smallest Standard SSD
// managed disk tier that fits. Azure charges a fixed monthly price per tier,
// not per-GB. The meter name in the retail API is the tier name (e.g. "E10").
type azureDiskTier struct {
	name  string
	maxGB int64
}

var azureDiskTiers = []azureDiskTier{
	{"E1", 4}, {"E2", 8}, {"E3", 16}, {"E4", 32}, {"E6", 64},
	{"E10", 128}, {"E15", 256}, {"E20", 512}, {"E30", 1024},
	{"E40", 2048}, {"E50", 4096}, {"E60", 8192}, {"E70", 16384},
	{"E80", 32767},
}

func azureDiskTierForSize(diskGB int64) (azureDiskTier, error) {
	if diskGB < 1 {
		return azureDiskTier{}, errors.New("disk size must be positive")
	}
	for _, tier := range azureDiskTiers {
		if diskGB <= tier.maxGB {
			return tier, nil
		}
	}
	return azureDiskTier{}, errors.New("disk size exceeds Standard SSD tier limits")
}

func azureDiskQuote(ctx context.Context, client *http.Client, region string, diskGB int64, now time.Time) (DiskQuote, error) {
	tier, err := azureDiskTierForSize(diskGB)
	if err != nil {
		return DiskQuote{}, err
	}
	values := url.Values{"currencyCode": {"'USD'"}, "api-version": {"2023-01-01-preview"}, "$filter": {fmt.Sprintf("serviceName eq 'Storage' and armRegionName eq '%s' and productName eq 'Standard SSD Managed Disks' and meterName eq '%s LRS Disk' and priceType eq 'Consumption'", region, tier.name)}}
	source := "https://prices.azure.com/api/retail/prices?" + values.Encode()
	next := source
	seen := map[string]bool{}
	var selected *azureRetailItem
	for page := 0; next != ""; page++ {
		parsed, parseErr := url.Parse(next)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() != "prices.azure.com" || (parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil || parsed.Path != "/api/retail/prices" || parsed.Fragment != "" || seen[next] || page >= 20 {
			return DiskQuote{}, errors.New("invalid or incomplete disk pricing pagination")
		}
		seen[next] = true
		req, err := http.NewRequestWithContext(ctx, "GET", next, nil)
		if err != nil {
			return DiskQuote{}, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return DiskQuote{}, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != 200 || len(raw) > 2<<20 {
			return DiskQuote{}, errors.New("disk pricing response unavailable or oversized")
		}
		var result struct {
			Items []azureRetailItem `json:"Items"`
			Next  string            `json:"NextPageLink"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return DiskQuote{}, errors.New("invalid disk pricing response")
		}
		for _, item := range result.Items {
			if item.Product != "Standard SSD Managed Disks" || item.Meter != tier.name+" LRS Disk" || item.SKU != tier.name+" LRS" || item.Minimum != 0 || item.Region != region || item.Service != "Storage" || item.Type != "Consumption" || item.Currency != "USD" || item.Unit != "1/Month" || !item.Primary || item.MeterID == "" || item.Price <= 0 || math.IsNaN(item.Price) || math.IsInf(item.Price*100, 0) || item.Effective.IsZero() || item.Effective.After(now) {
				continue
			}
			if selected != nil {
				if selected.MeterID != item.MeterID {
					return DiskQuote{}, errors.New("ambiguous disk meter")
				}
				if selected.Effective.Equal(item.Effective) && selected.Price != item.Price {
					return DiskQuote{}, errors.New("conflicting disk prices")
				}
				if !item.Effective.After(selected.Effective) {
					continue
				}
			}
			copy := item
			selected = &copy
		}
		next = result.Next
	}
	if selected == nil {
		return DiskQuote{}, errors.New("Standard SSD disk price unavailable for this region and tier")
	}
	return DiskQuote{Provider: "azure", Region: region, DiskType: "StandardSSD_LRS " + tier.name, DiskGB: diskGB, Currency: "USD", Monthly: math.Round(selected.Price*100) / 100, PerGBMonth: math.Round(selected.Price/float64(tier.maxGB)*10000) / 10000, MeterID: selected.MeterID, Source: source, RetrievedAt: now, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"Disk transactions and bursting", "Premium SSD and Ultra Disk alternatives", "Snapshots and snapshot redundancy", "Additional data disks"}}, nil
}

// parseFloat is a minimal safe wrapper used for disk pricing.
func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, errors.New("empty price")
	}
	return strconv.ParseFloat(s, 64)
}
