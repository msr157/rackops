package cloud

import (
	"time"
)

// TransferQuote is a per-GB monthly subtotal for outbound data transfer (egress)
// from the region to the internet (or standard inter-region). Intra-region,
// direct connect, and CDN transfer costs are excluded.
type TransferQuote struct {
	Provider    string    `json:"provider"`
	Region      string    `json:"region"`
	TransferGB  int64     `json:"transferGb"`
	Tier        string    `json:"tier"` // e.g. "Standard", "Premium"
	Currency    string    `json:"currency"`
	Monthly     float64   `json:"monthly"`
	PerGBMonth  float64   `json:"perGbMonth"`
	MeterID     string    `json:"meterId"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrievedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Exclusions  []string  `json:"exclusions"`
}
