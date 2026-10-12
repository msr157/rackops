package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"testing"
	"time"
)

type pricingFixture func(*pricing.GetProductsInput) (*pricing.GetProductsOutput, error)

func (f pricingFixture) GetProducts(_ context.Context, in *pricing.GetProductsInput, _ ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
	return f(in)
}

func TestAWSComputeQuoteSelection(t *testing.T) {
	for _, mode := range []string{"base", "windows", "region", "dedicated", "software", "reserved", "future", "tier", "currency", "negative", "nan", "ambiguous", "pagination", "loop", "denied"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := pricingFixture(func(in *pricing.GetProductsInput) (*pricing.GetProductsOutput, error) {
				calls++
				if aws.ToString(in.ServiceCode) != "AmazonEC2" || len(in.Filters) != 8 {
					t.Fatal("missing selection filters")
				}
				if mode == "denied" {
					return nil, errors.New("denied")
				}
				attributes := map[string]string{"regionCode": "us-east-1", "instanceType": "m5.large", "operatingSystem": "Linux", "tenancy": "Shared", "preInstalledSw": "NA", "capacitystatus": "Used", "locationType": "AWS Region", "operation": "RunInstances"}
				switch mode {
				case "windows":
					attributes["operatingSystem"] = "Windows"
				case "region":
					attributes["regionCode"] = "us-west-2"
				case "dedicated":
					attributes["tenancy"] = "Dedicated"
				case "software":
					attributes["preInstalledSw"] = "SQL Std"
				}
				price := map[string]string{"USD": "0.096"}
				if mode == "currency" {
					price = map[string]string{"EUR": "0.1"}
				}
				if mode == "negative" {
					price["USD"] = "-1"
				}
				if mode == "nan" {
					price["USD"] = "NaN"
				}
				begin := "0"
				if mode == "tier" {
					begin = "100"
				}
				effective := time.Now().Add(-time.Hour)
				if mode == "future" {
					effective = time.Now().Add(time.Hour)
				}
				term := map[string]any{"sku": "sku", "effectiveDate": effective, "priceDimensions": map[string]any{"rate": map[string]any{"rateCode": "rate", "unit": "Hrs", "beginRange": begin, "endRange": "Inf", "pricePerUnit": price, "appliesTo": []string{}}}}
				termType := "OnDemand"
				if mode == "reserved" {
					termType = "Reserved"
				}
				raw, _ := json.Marshal(map[string]any{"serviceCode": "AmazonEC2", "product": map[string]any{"sku": "sku", "productFamily": "Compute Instance", "attributes": attributes}, "terms": map[string]any{termType: map[string]any{"term": term}}})
				response := &pricing.GetProductsOutput{PriceList: []string{string(raw)}}
				if mode == "ambiguous" {
					response.PriceList = append(response.PriceList, string(raw))
				}
				if mode == "pagination" && calls == 1 {
					response.PriceList = nil
					response.NextToken = aws.String("next")
				}
				if mode == "loop" {
					response.PriceList = nil
					response.NextToken = aws.String("next")
				}
				return response, nil
			})
			quote, err := awsComputeQuote(context.Background(), client, "us-east-1", "m5.large", time.Now())
			valid := mode == "base" || mode == "pagination"
			if (err == nil) != valid {
				t.Fatalf("unexpected result %v", err)
			}
			if valid && (quote.Monthly != 70.08 || quote.MeterID != "rate" || len(quote.Exclusions) == 0 || quote.Provider != "aws") {
				t.Fatal("incorrect quote")
			}
		})
	}
}
