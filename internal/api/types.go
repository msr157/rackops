// Package api defines the wire types shared by the node agent, the control
// planes, and (mirrored by hand) the TypeScript dashboards.
//
// KEEP IN SYNC with packages/api/src/types.ts — this file is the source of truth.
package api

import "time"

type CPUInfo struct {
	Model    string  `json:"model"`
	Cores    int     `json:"cores"`
	UsagePct float64 `json:"usagePct"`
}

type MemInfo struct {
	TotalBytes uint64  `json:"totalBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	UsedPct    float64 `json:"usedPct"`
}

type DiskInfo struct {
	Mountpoint string  `json:"mountpoint"`
	FS         string  `json:"fs"`
	TotalBytes uint64  `json:"totalBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	UsedPct    float64 `json:"usedPct"`
}

type NetIface struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
}

// Snapshot is one point-in-time reading of a machine's hardware state.
type Snapshot struct {
	Hostname    string     `json:"hostname"`
	OS          string     `json:"os"`
	Platform    string     `json:"platform"`
	Arch        string     `json:"arch"`
	UptimeSec   uint64     `json:"uptimeSec"`
	CPU         CPUInfo    `json:"cpu"`
	Memory      MemInfo    `json:"memory"`
	Disks       []DiskInfo `json:"disks"`
	Network     []NetIface `json:"network"`
	CollectedAt time.Time  `json:"collectedAt"`
}

// ReportRequest is what the agent POSTs to /api/v1/report.
type ReportRequest struct {
	NodeID   string   `json:"nodeId"`
	Snapshot Snapshot `json:"snapshot"`
}

// EarlyAccessRequest is what the website POSTs to /api/v1/early-access
type EarlyAccessRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Industry string `json:"industry"`
	Notes    string `json:"notes,omitempty"`
}

const (
	StatusOnline = "online"
	StatusStale  = "stale"
)

// Node is the control plane's view of a registered machine.
type Node struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"lastSeen"`
	Latest   Snapshot  `json:"latest"`
}

// Meta describes the control plane itself.
type Meta struct {
	Edition string `json:"edition"`
	Version string `json:"version"`
}
