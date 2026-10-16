package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// ServiceBackupItem describes an individual database snapshot.
type ServiceBackupItem struct {
	Filename      string    `json:"filename"`
	SizeBytes     int64     `json:"sizeBytes"`
	SizeFormatted string    `json:"sizeFormatted"`
	CreatedAt     time.Time `json:"createdAt"`
	Replicated    bool      `json:"replicated"`
	Format        string    `json:"format"`
}

// pendingBackups holds waiting channels for backup/restore tasks keyed by task ID.
var pendingBackups sync.Map // taskID -> chan types.TaskResult

// deliverBackup routes a backup or restore task result to its waiting HTTP request.
func deliverBackup(taskID string, res types.TaskResult) bool {
	if v, ok := pendingBackups.Load(taskID); ok {
		select {
		case v.(chan types.TaskResult) <- res:
		default:
		}
		return true
	}
	return false
}

// formatBytes returns a human-readable byte size.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// parseTimestampFromFilename extracts a timestamp from filenames like dump_20261016_000001.sql.gz.
func parseTimestampFromFilename(name string, fallback time.Time) time.Time {
	clean := strings.TrimSuffix(name, ".sql.gz")
	clean = strings.TrimSuffix(clean, ".sql")
	clean = strings.TrimSuffix(clean, ".gz")

	// Search for YYYYMMDD_HHMMSS or YYYYMMDDHHMMSS
	parts := strings.Split(clean, "_")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if len(p) == 15 && strings.Contains(p, "_") {
			if t, err := time.Parse("20060102_150405", p); err == nil {
				return t
			}
		}
		if len(p) == 6 && i > 0 && len(parts[i-1]) == 8 {
			combined := parts[i-1] + "_" + p
			if t, err := time.Parse("20060102_150405", combined); err == nil {
				return t
			}
		}
		if len(p) == 14 {
			if t, err := time.Parse("20060102150405", p); err == nil {
				return t
			}
		}
	}
	return fallback
}

// candidateBackupDirs returns directories on the control plane where backups reside.
func (a *API) candidateBackupDirs(svc *types.Service) []string {
	var dirs []string
	if a.DataDir != "" {
		dirs = append(dirs, filepath.Join(a.DataDir, "backups", svc.ID))
		dirs = append(dirs, filepath.Join(a.DataDir, "backups"))
	}
	// Central replication directory on mainframe storage
	dirs = append(dirs, "/data/backups/postgres")
	dirs = append(dirs, "/home/rogue-server/data/postgresql/backups")
	return dirs
}

// handleServiceBackups returns all snapshot backups available for a service.
func (a *API) handleServiceBackups(w http.ResponseWriter, r *http.Request) {
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}

	seen := make(map[string]bool)
	var items []ServiceBackupItem

	dirs := a.candidateBackupDirs(svc)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if seen[name] {
				continue
			}
			if strings.HasSuffix(name, ".log") || name == "latest.sql.gz" || name == "latest.sql" {
				continue
			}
			if !strings.HasSuffix(name, ".sql.gz") && !strings.HasSuffix(name, ".sql") &&
				!strings.HasSuffix(name, ".dump") && !strings.HasSuffix(name, ".tar.gz") {
				continue
			}

			info, err := e.Info()
			if err != nil {
				continue
			}

			seen[name] = true
			created := parseTimestampFromFilename(name, info.ModTime())

			format := "sql.gz"
			if strings.HasSuffix(name, ".sql") {
				format = "sql"
			} else if strings.HasSuffix(name, ".dump") {
				format = "dump"
			} else if strings.HasSuffix(name, ".tar.gz") {
				format = "tar.gz"
			}

			replicated := strings.HasPrefix(dir, "/data/backups")

			items = append(items, ServiceBackupItem{
				Filename:      name,
				SizeBytes:     info.Size(),
				SizeFormatted: formatBytes(info.Size()),
				CreatedAt:     created,
				Replicated:    replicated,
				Format:        format,
			})
		}
	}

	// Sort newest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	writeJSON(w, items)
}

// handleCreateServiceBackup triggers an instant snapshot of the service's database.
func (a *API) handleCreateServiceBackup(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !requireServiceControl(w, member) {
		return
	}

	taskID := NewID()
	ch := make(chan types.TaskResult, 1)
	pendingBackups.Store(taskID, ch)
	defer pendingBackups.Delete(taskID)

	payload, _ := json.Marshal(DeployPayload{
		ServiceID:    svc.ID,
		Name:         svc.Name,
		Image:        svc.Image,
		ContainerID:  svc.ContainerID,
		ExternalName: svc.ExternalName,
	})

	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID:        taskID,
		Kind:      types.TaskBackup,
		ServiceID: svc.ID,
		Payload:   string(payload),
		CreatedAt: time.Now(),
	})

	a.Store.AddEvent("backup", "Triggered instant database snapshot for "+svc.Name, svc.NodeID, svc.ID)

	select {
	case res := <-ch:
		if !res.OK {
			http.Error(w, "backup execution failed: "+res.Log, http.StatusInternalServerError)
			return
		}
		a.Store.AddEvent("backup", fmt.Sprintf("Snapshot created for %s: %s", svc.Name, res.Log), svc.NodeID, svc.ID)
		writeJSON(w, map[string]any{
			"ok":      true,
			"message": res.Log,
		})
	case <-time.After(90 * time.Second):
		http.Error(w, "snapshot timed out waiting for node agent", http.StatusGatewayTimeout)
	case <-r.Context().Done():
	}
}

// handleDownloadServiceBackup streams the requested snapshot file.
func (a *API) handleDownloadServiceBackup(w http.ResponseWriter, r *http.Request) {
	svc, _, _ := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}

	filename := filepath.Base(r.PathValue("filename"))
	if filename == "" || filename == "." || filename == "/" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	dirs := a.candidateBackupDirs(svc)
	var filePath string
	for _, dir := range dirs {
		cand := filepath.Join(dir, filename)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			filePath = cand
			break
		}
	}

	if filePath == "" {
		http.Error(w, fmt.Sprintf("backup snapshot %q not found", filename), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if strings.HasSuffix(filename, ".gz") {
		w.Header().Set("Content-Type", "application/gzip")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	http.ServeFile(w, r, filePath)
}

// handleRestoreServiceBackup restores a database service from a selected snapshot.
func (a *API) handleRestoreServiceBackup(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !requireServiceControl(w, member) {
		return
	}

	var req struct {
		Filename        string `json:"filename"`
		ConfirmDatabase string `json:"confirmDatabase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Filename = filepath.Base(strings.TrimSpace(req.Filename))
	if req.Filename == "" || req.Filename == "." {
		http.Error(w, "snapshot filename is required", http.StatusBadRequest)
		return
	}

	expectedConfirm := svc.Name
	if req.ConfirmDatabase != expectedConfirm &&
		req.ConfirmDatabase != svc.ExternalName &&
		req.ConfirmDatabase != "CONFIRM" {
		http.Error(w, fmt.Sprintf("confirmation must match %q to authorize snapshot restoration", expectedConfirm), http.StatusBadRequest)
		return
	}

	taskID := NewID()
	ch := make(chan types.TaskResult, 1)
	pendingBackups.Store(taskID, ch)
	defer pendingBackups.Delete(taskID)

	payload, _ := json.Marshal(DeployPayload{
		ServiceID:    svc.ID,
		Name:         svc.Name,
		Image:        svc.Image,
		ContainerID:  svc.ContainerID,
		ExternalName: svc.ExternalName,
		Path:         req.Filename,
	})

	a.Store.EnqueueTask(svc.NodeID, types.AgentTask{
		ID:        taskID,
		Kind:      types.TaskRestore,
		ServiceID: svc.ID,
		Payload:   string(payload),
		CreatedAt: time.Now(),
	})

	a.Store.AddEvent("restore", fmt.Sprintf("Initiated database restore for %s from %s", svc.Name, req.Filename), svc.NodeID, svc.ID)

	select {
	case res := <-ch:
		if !res.OK {
			http.Error(w, "restore failed: "+res.Log, http.StatusInternalServerError)
			return
		}
		a.Store.AddEvent("restore", fmt.Sprintf("Database %s successfully restored from %s", svc.Name, req.Filename), svc.NodeID, svc.ID)
		writeJSON(w, map[string]any{
			"ok":  true,
			"log": res.Log,
		})
	case <-time.After(120 * time.Second):
		http.Error(w, "restore operation timed out waiting for node agent", http.StatusGatewayTimeout)
	case <-r.Context().Done():
	}
}
