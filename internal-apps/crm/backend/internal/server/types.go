package server

import "time"

// Ticket categories, severities and statuses accepted from instances.
// Anything outside these sets is rejected at the ingest boundary.
var (
	validCategories = map[string]bool{
		"general": true, "deploys": true, "networking": true,
		"updates": true, "billing": true, "bug": true,
	}
	validSeverities = map[string]bool{
		"low": true, "normal": true, "high": true, "urgent": true,
	}
	validStatuses = map[string]bool{
		"open": true, "pending": true, "resolved": true,
	}
)

// UsageSnapshot is the opt-in telemetry payload a RackOps Lite instance
// reports alongside tickets and periodic snapshot posts.
type UsageSnapshot struct {
	Version        string         `json:"version"`
	OS             string         `json:"os"`
	Arch           string         `json:"arch"`
	Nodes          int            `json:"nodes"`
	NodesOnline    int            `json:"nodesOnline"`
	Services       int            `json:"services"`
	ServicesByKind map[string]int `json:"servicesByKind"`
	Adopted        int            `json:"adopted"`
	EdgeEnabled    bool           `json:"edgeEnabled"`
	MeshEnabled    bool           `json:"meshEnabled"`
	HA             bool           `json:"ha"`
	Orgs           int            `json:"orgs"`
	Users          int            `json:"users"`
	Domains        int            `json:"domains"`
	FeaturesUsed   []string       `json:"featuresUsed"`
}

// Install is a customer's self-hosted RackOps Lite instance, identified by
// the UUID it generated at first boot.
type Install struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	OS        string         `json:"os"`
	Arch      string         `json:"arch"`
	FirstSeen time.Time      `json:"firstSeen"`
	LastSeen  time.Time      `json:"lastSeen"`
	Usage     *UsageSnapshot `json:"usage,omitempty"`
	// OpenTickets is filled by list/detail queries for the dashboard.
	OpenTickets int `json:"openTickets"`
}

// Snapshot is one historical usage report from an install.
type Snapshot struct {
	At   time.Time      `json:"at"`
	Data *UsageSnapshot `json:"data"`
}

// Ticket is a support ticket forwarded by a customer instance. Ticket IDs
// are minted by the instance, so the true primary key is (ID, InstallID).
type Ticket struct {
	ID          string          `json:"id"`
	InstallID   string          `json:"installId"`
	InstallName string          `json:"installName,omitempty"`
	Subject     string          `json:"subject"`
	Category    string          `json:"category"`
	Severity    string          `json:"severity"`
	Status      string          `json:"status"`
	Email       string          `json:"email"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Messages    []TicketMessage `json:"messages,omitempty"`
}

// TicketMessage is one entry in a ticket thread. Author is one of:
// "user" (customer, forwarded by the instance), "agent" (RackOps support,
// polled back by the instance), "note" (internal only, never leaves the CRM).
type TicketMessage struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Name   string    `json:"name"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

// ResetEmail is an audit-log row for every password-reset relay attempt.
type ResetEmail struct {
	ID           int64     `json:"id"`
	InstallID    string    `json:"installId"`
	Email        string    `json:"email"`
	InstanceName string    `json:"instanceName"`
	At           time.Time `json:"at"`
	Status       string    `json:"status"`
}

// Overview is the dashboard's landing-page aggregate.
type Overview struct {
	Installs      int            `json:"installs"`
	ActiveWeek    int            `json:"activeWeek"`
	OpenTickets   int            `json:"openTickets"`
	VersionDist   []VersionCount `json:"versionDist"`
	FeatureUsage  []FeatureCount `json:"featureUsage"`
	ByKindTotals  map[string]int `json:"byKindTotals"`
	RecentTickets []*Ticket      `json:"recentTickets"`
}

type VersionCount struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

type FeatureCount struct {
	Feature string `json:"feature"`
	Count   int    `json:"count"`
}
