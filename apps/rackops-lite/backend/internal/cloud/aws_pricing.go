package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

type awsPricingAPI interface {
	GetProducts(context.Context, *pricing.GetProductsInput, ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
}

func AWSComputeQuote(ctx context.Context, region, size string, cred AWSCredentials) (ComputeQuote, error) {
	if !awsRegion.MatchString(region) || !instanceType.MatchString(size) || cred.AccessKeyID == "" || cred.SecretAccessKey == "" {
		return ComputeQuote{}, errors.New("invalid AWS pricing selection")
	}
	client := pricing.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cred.AccessKeyID, cred.SecretAccessKey, cred.SessionToken)), RetryMaxAttempts: 2, HTTPClient: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("pricing redirect refused") }}})
	return awsComputeQuote(ctx, client, region, size, time.Now().UTC())
}

func awsComputeQuote(ctx context.Context, client awsPricingAPI, region, size string, now time.Time) (ComputeQuote, error) {
	expected := map[string]string{"regionCode": region, "instanceType": size, "operatingSystem": "Linux", "tenancy": "Shared", "preInstalledSw": "NA", "capacitystatus": "Used", "locationType": "AWS Region", "operation": "RunInstances"}
	input := &pricing.GetProductsInput{ServiceCode: aws.String("AmazonEC2"), FormatVersion: aws.String("aws_v1"), MaxResults: aws.Int32(100)}
	for field, value := range expected {
		input.Filters = append(input.Filters, pricingtypes.Filter{Field: aws.String(field), Value: aws.String(value), Type: pricingtypes.FilterTypeTermMatch})
	}
	seen := map[string]bool{}
	var selected ComputeQuote
	for page := 0; ; page++ {
		if page >= 10 {
			return ComputeQuote{}, errors.New("incomplete AWS pricing pagination")
		}
		response, err := client.GetProducts(ctx, input)
		if err != nil {
			return ComputeQuote{}, err
		}
		if response == nil {
			return ComputeQuote{}, errors.New("empty pricing response")
		}
		for _, raw := range response.PriceList {
			if len(raw) > 1<<20 {
				return ComputeQuote{}, errors.New("oversized pricing product")
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
							Unit    string            `json:"unit"`
							Begin   string            `json:"beginRange"`
							End     string            `json:"endRange"`
							Rate    string            `json:"rateCode"`
							Applies []string          `json:"appliesTo"`
							Price   map[string]string `json:"pricePerUnit"`
						} `json:"priceDimensions"`
					} `json:"OnDemand"`
				} `json:"terms"`
			}
			if json.Unmarshal([]byte(raw), &product) != nil {
				return ComputeQuote{}, errors.New("invalid pricing product")
			}
			matches := product.ServiceCode == "AmazonEC2" && product.Product.Family == "Compute Instance" && product.Product.SKU != ""
			for field, value := range expected {
				if product.Product.Attributes[field] != value {
					matches = false
				}
			}
			if !matches {
				continue
			}
			for _, term := range product.Terms.OnDemand {
				if term.SKU != product.Product.SKU || term.Effective.IsZero() || term.Effective.After(now) {
					continue
				}
				if len(term.Dimensions) != 1 {
					return ComputeQuote{}, errors.New("unsupported tiered compute pricing")
				}
				for _, dimension := range term.Dimensions {
					price, err := strconv.ParseFloat(dimension.Price["USD"], 64)
					if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price*73000, 0) || dimension.Unit != "Hrs" || dimension.Begin != "0" || dimension.End != "Inf" || dimension.Rate == "" || len(dimension.Applies) != 0 {
						return ComputeQuote{}, errors.New("unsupported compute price")
					}
					if selected.MeterID != "" {
						return ComputeQuote{}, errors.New("ambiguous compute price")
					}
					selected = ComputeQuote{Provider: "aws", Region: region, Size: size, Currency: "USD", Hourly: price, Monthly: math.Round(price*73000) / 100, HoursPerMonth: 730, MeterID: dimension.Rate, Source: "https://pricing.us-east-1.amazonaws.com", RetrievedAt: now, EffectiveAt: term.Effective, ExpiresAt: now.Add(30 * time.Minute), Exclusions: []string{"EBS volumes, snapshots and disk throughput/IOPS", "Network resources and traffic transfer", "CPU surplus credits and paid image software", "Taxes, support and account-specific discounts", "Temporary duplicate resources during migration"}}
				}
			}
		}
		token := aws.ToString(response.NextToken)
		if token == "" {
			break
		}
		if seen[token] {
			return ComputeQuote{}, errors.New("repeated pricing cursor")
		}
		seen[token] = true
		input.NextToken = aws.String(token)
	}
	if selected.MeterID == "" {
		return ComputeQuote{}, errors.New("base VM price unavailable")
	}
	return selected, nil
}
