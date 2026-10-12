package cloud

import (
	"testing"
)

func TestTransferQuotes(t *testing.T) {
	t.Run("aws transfer quote", func(t *testing.T) {
		quote, err := AWSTransferQuote("us-east-1", 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if quote.Monthly != 9.00 || quote.PerGBMonth != 0.09 || quote.Provider != "aws" || quote.Currency != "USD" {
			t.Fatalf("unexpected aws quote: %+v", quote)
		}
		if len(quote.Exclusions) == 0 || quote.Source == "" {
			t.Fatalf("missing quote metadata: %+v", quote)
		}

		if _, err := AWSTransferQuote("invalid-region", 100); err == nil {
			t.Fatal("expected error for invalid region")
		}
		if _, err := AWSTransferQuote("us-east-1", -5); err == nil {
			t.Fatal("expected error for negative transfer")
		}
	})

	t.Run("azure transfer quote", func(t *testing.T) {
		quote, err := AzureTransferQuote("eastus", 200)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if quote.Monthly != 17.40 || quote.PerGBMonth != 0.087 || quote.Provider != "azure" {
			t.Fatalf("unexpected azure quote: %+v", quote)
		}

		if _, err := AzureTransferQuote("invalid region", 100); err == nil {
			t.Fatal("expected error for invalid region")
		}
	})

	t.Run("gcp transfer quote", func(t *testing.T) {
		quote, err := GCPTransferQuote("us-central1", 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if quote.Monthly != 6.00 || quote.PerGBMonth != 0.12 || quote.Provider != "gcp" {
			t.Fatalf("unexpected gcp quote: %+v", quote)
		}

		if _, err := GCPTransferQuote("bad-region", 50); err == nil {
			t.Fatal("expected error for invalid region")
		}
	})
}
