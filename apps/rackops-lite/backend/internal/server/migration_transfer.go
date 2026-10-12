package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rackops/rackops/internal/migration"
)

// Transfer routes authenticate an enrolled node, never a legacy install-wide
// join token. Artifact ownership and destination binding are checked per request.
func (a *API) handleMigrationTransfer(w http.ResponseWriter, r *http.Request) {
	if a.Cluster != nil {
		http.Error(w, "migration transfer requires a single server", 409)
		return
	}
	a.migrationTransferMu.Lock()
	defer a.migrationTransferMu.Unlock()
	var artifact BackupArtifact
	nodeID := r.Header.Get("X-Rackops-Node")
	allowed := false
	a.Store.View(func() {
		credential := a.Store.NodeCredentials[nodeID]
		stored := a.Store.BackupArtifacts[r.PathValue("id")]
		if credential == nil || stored == nil || credential.OrgID != stored.OrgID || !a.agentAuthorizedLocked(nodeID, r.Header.Get("X-Rackops-Token")) {
			return
		}
		job := a.Store.MigrationJobs[stored.JobID]
		if job == nil || job.Job.OrgID != stored.OrgID || (job.Job.Stage != migration.Transfer && job.Job.Stage != migration.Restore) || (stored.State != "ready" && stored.State != "source_uploaded" && stored.State != "destination_received") {
			return
		}
		if nodeID != stored.SourceNodeID && nodeID != stored.DestinationNodeID {
			return
		}
		artifact = *stored
		allowed = true
	})
	if !allowed {
		http.Error(w, "artifact transfer unavailable", 403)
		return
	}
	isSource := nodeID == artifact.SourceNodeID
	if r.Method != http.MethodGet && !isSource {
		http.Error(w, "only the bound source may upload", 403)
		return
	}
	if r.Method == http.MethodPost && r.PathValue("action") != "" && r.PathValue("action") != "finalize" {
		http.Error(w, "unknown transfer action", 404)
		return
	}
	name := sha256.Sum256([]byte(artifact.OrgID + ":" + artifact.ID))
	parent := filepath.Join(a.Store.dataDir, "migration-transfer")
	if err := os.MkdirAll(parent, 0700); err != nil {
		http.Error(w, "transfer storage unavailable", 503)
		return
	}
	dir := filepath.Join(parent, hex.EncodeToString(name[:]))
	receiver, err := migration.OpenReceiver(dir, artifact.Transfer)
	if err != nil {
		http.Error(w, "transfer manifest or storage conflict", 409)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost && r.PathValue("action") == "finalize" {
		if _, err := receiver.Finalize(r.Context()); err != nil {
			http.Error(w, "complete artifact integrity verification failed", 409)
			return
		}
		writeJSON(w, map[string]bool{"complete": true})
		return
	}
	if r.Method == http.MethodGet && isSource {
		offset, err := receiver.Offset()
		if err != nil {
			http.Error(w, "transfer prefix unavailable", 409)
			return
		}
		writeJSON(w, map[string]int64{"offset": offset})
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 || offset >= artifact.Transfer.Bytes {
		http.Error(w, "invalid transfer offset", 400)
		return
	}
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid transfer chunk", 400)
			return
		}
		next, err := receiver.Append(r.Context(), offset, body, r.Header.Get("X-Rackops-Chunk-SHA256"))
		if err != nil {
			http.Error(w, "transfer chunk or offset conflict", 409)
			return
		}
		writeJSON(w, map[string]int64{"offset": next})
		return
	}
	// Finalize publishes this name only after the complete encrypted digest
	// matches. The destination verifies its own complete copy again before restore.
	file, err := os.Open(filepath.Join(dir, "artifact.enc"))
	if err != nil {
		http.Error(w, "verified artifact is not ready", 409)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Transfer.Bytes {
		http.Error(w, "verified artifact unavailable", 409)
		return
	}
	chunk := make([]byte, min(int64(1<<20), artifact.Transfer.Bytes-offset))
	if _, err := file.ReadAt(chunk, offset); err != nil {
		http.Error(w, "artifact read failed", 503)
		return
	}
	digest := sha256.Sum256(chunk)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Rackops-Chunk-SHA256", hex.EncodeToString(digest[:]))
	_, _ = w.Write(chunk)
}
