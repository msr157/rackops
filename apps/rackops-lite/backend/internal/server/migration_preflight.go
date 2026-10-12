package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

type migrationFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type migrationReview struct {
	ServiceID       string             `json:"serviceId"`
	Provider        string             `json:"provider"`
	Region          string             `json:"region"`
	Architecture    string             `json:"architecture"`
	CanStart        bool               `json:"canStart"`
	Blockers        []migrationFinding `json:"blockers"`
	CheckedAt       time.Time          `json:"checkedAt"`
	CostEstimate    *float64           `json:"monthlyCostEstimate"`
	DataBytes       *int64             `json:"dataBytes"`
	DowntimeSeconds *int64             `json:"downtimeSeconds"`
}

var migrationRegion = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// This is a read-only assessment, not an approved provisioning plan. Unknown
// costs, data size and downtime remain null rather than misleading zeroes.
func (a *API) handleMigrationPreflight(w http.ResponseWriter, r *http.Request) {
	svc, org, member := a.serviceForOrg(w, r)
	if svc == nil || !requireServiceControl(w, member) {
		return
	}
	var req struct {
		Provider     string `json:"provider"`
		Region       string `json:"region"`
		Architecture string `json:"architecture"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	switch req.Provider {
	case "aws", "azure", "gcp":
	default:
		http.Error(w, "provider must be aws, azure or gcp", 400)
		return
	}
	if !migrationRegion.MatchString(req.Region) {
		http.Error(w, "a valid region name is required", 400)
		return
	}
	review := migrationReview{ServiceID: r.PathValue("id"), Provider: req.Provider, Region: req.Region, CheckedAt: time.Now().UTC(), Blockers: []migrationFinding{}}
	add := func(code, message string) { review.Blockers = append(review.Blockers, migrationFinding{code, message}) }
	found := false
	a.Store.View(func() {
		current := a.Store.Services[review.ServiceID]
		if current == nil || !a.serviceInOrg(current, org.ID) || !projectVisible(member, current.ProjectID) {
			return
		}
		found = true
		if node := a.Store.Nodes[current.NodeID]; node != nil && node.OrgID == org.ID {
			review.Architecture = node.Arch
			if node.OS != "linux" {
				add("source_os", "Migration requires a Linux source machine.")
			}
			if node.Status != types.NodeOnline || time.Since(node.LastSeen) > time.Minute {
				add("source_offline", "Reconnect the source agent and refresh its inventory.")
			}
		} else {
			add("source_missing", "The source machine is unavailable in this organization.")
		}
		if review.Architecture != "amd64" && review.Architecture != "arm64" {
			add("source_architecture", "Source architecture must be verified as amd64 or arm64.")
		}
		if req.Architecture != "" && req.Architecture != review.Architecture {
			add("architecture_change", "Changing CPU architecture is not supported.")
		}
		if current.Adopted {
			add("adopted_workload", "Recreate this app as a RackOps-managed deployment before migrating.")
		}
		if current.GPU {
			add("gpu", "GPU workloads are outside the initial migration scope.")
		}
		if current.ComposeYML != "" || current.Kind == types.KindCompose {
			add("compose_review", "Compose dependencies, privileges and storage require a validated migration adapter.")
			add("compose_dependencies", "Multi-container Compose services require per-service dependency ordering and network mapping.")
		}
		if current.Kind == types.KindDatabase {
			add("database_backup", "A tested database-specific backup and restore is required before this database can move.")
			imgLower := strings.ToLower(current.Image)
			if !strings.Contains(imgLower, "postgres") && !strings.Contains(imgLower, "mysql") && !strings.Contains(imgLower, "mariadb") {
				add("unsupported_database_engine", "Automated migration currently supports PostgreSQL and MySQL engines only.")
			}
		}
		if len(current.Mounts) > 0 || len(current.Volumes) > 0 || current.PinnedPath != "" {
			add("storage_backup", "Attached storage requires a consistent backup, integrity verification and a tested destination restore.")
			hasBind := false
			for _, m := range current.Mounts {
				if m.Type == "bind" || strings.HasPrefix(m.Source, "/") || strings.HasPrefix(m.Source, "\\") {
					hasBind = true
					break
				}
			}
			if hasBind || current.PinnedPath != "" {
				add("unsupported_volume_entry", "Host bind mounts cannot be migrated across machines; convert to named Docker volumes.")
			}
		}
		if current.Image == "" && current.GitRepo == "" && current.ComposeYML == "" {
			add("deployment_source", "A reproducible deployment source is missing.")
		}
		if current.Status == types.StatusBuilding || current.Status == types.StatusPending {
			add("active_deployment", "Finish the current deployment before planning a move.")
		}
	})
	if !found {
		http.Error(w, "service not found", 404)
		return
	}
	add("dependency_inventory", "External dependencies, image-declared volumes, writable container data and privileges still need agent inspection.")
	add("cloud_validation", "Cloud account permissions, region availability, quotas, sizing and cost have not been validated.")
	add("migration_engine", "Provisioning, data transfer, health validation and traffic cutover are not available yet. This review makes no changes.")
	writeJSON(w, review)
}
