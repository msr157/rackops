package cloud

import "time"

type InspectionFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Resource metadata compatibility is only one part of a launch review. A
// positive result does not certify network reachability, capacity or app health.
type MachineInspection struct {
	Provider            string              `json:"provider"`
	Region              string              `json:"region"`
	SpecFingerprint     string              `json:"specFingerprint"`
	CheckedAt           time.Time           `json:"checkedAt"`
	Architecture        string              `json:"architecture"`
	VCPUs               int32               `json:"vcpus"`
	MemoryMB            int64               `json:"memoryMb"`
	Zone                string              `json:"zone"`
	MinimumDiskGB       int32               `json:"minimumDiskGb"`
	ResourceCompatible  bool                `json:"resourceCompatible"`
	CanLaunch           bool                `json:"canLaunch"`
	MonthlyCostEstimate *float64            `json:"monthlyCostEstimate"`
	Blockers            []InspectionFinding `json:"blockers"`
	RemainingChecks     []string            `json:"remainingChecks"`
}
