package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func setupTestBackupService(t *testing.T) (*API, *types.Service) {
	a, _ := testAPI(t, types.RoleOwner)
	a.DataDir = t.TempDir()
	var orgID string
	a.Store.View(func() {
		for id := range a.Store.Orgs {
			orgID = id
			break
		}
	})

	svc := &types.Service{
		ID:        NewID(),
		Name:      "postgres-test",
		Status:    types.StatusRunning,
		NodeID:    "node-1",
		ProjectID: "p1",
		CreatedAt: time.Now(),
	}
	a.Store.Mutate(func() {
		a.Store.Nodes["node-1"] = &types.Node{
			ID:     "node-1",
			Name:   "worker-1",
			OrgID:  orgID,
			Status: types.NodeOnline,
		}
		a.Store.Projects["p1"] = &types.Project{
			ID:    "p1",
			Name:  "proj",
			OrgID: orgID,
		}
		a.Store.Services[svc.ID] = svc
	})
	return a, svc
}

func TestServiceBackupsListing(t *testing.T) {
	a, svc := setupTestBackupService(t)
	router := a.Router()

	backupDir := filepath.Join(a.DataDir, "backups", svc.ID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("failed to create test backup directory: %v", err)
	}
	defer os.RemoveAll(backupDir)

	mockFile := filepath.Join(backupDir, "dump_20261016_120000.sql.gz")
	if err := os.WriteFile(mockFile, []byte("mock-compressed-dump"), 0644); err != nil {
		t.Fatalf("failed to create test mock snapshot: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/services/"+svc.ID+"/backups", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken(t, a))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var items []ServiceBackupItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	found := false
	for _, it := range items {
		if it.Filename == "dump_20261016_120000.sql.gz" {
			found = true
			if it.SizeBytes != int64(len("mock-compressed-dump")) {
				t.Errorf("expected size %d, got %d", len("mock-compressed-dump"), it.SizeBytes)
			}
			if it.Format != "sql.gz" {
				t.Errorf("expected format sql.gz, got %s", it.Format)
			}
			break
		}
	}
	if !found {
		t.Fatalf("mock backup not found in items: %+v", items)
	}
}

func TestServiceBackupDownload(t *testing.T) {
	a, svc := setupTestBackupService(t)
	router := a.Router()

	backupDir := filepath.Join(a.DataDir, "backups", svc.ID)
	_ = os.MkdirAll(backupDir, 0755)
	defer os.RemoveAll(backupDir)

	mockContent := "database snapshot content 12345"
	mockFile := filepath.Join(backupDir, "dump_test.sql.gz")
	_ = os.WriteFile(mockFile, []byte(mockContent), 0644)

	// Download existing
	req := httptest.NewRequest(http.MethodGet, "/api/services/"+svc.ID+"/backups/dump_test.sql.gz/download", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken(t, a))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for download, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != mockContent {
		t.Fatalf("expected content %q, got %q", mockContent, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "dump_test.sql.gz") {
		t.Errorf("expected disposition with filename, got %s", rec.Header().Get("Content-Disposition"))
	}

	// Download missing
	reqMissing := httptest.NewRequest(http.MethodGet, "/api/services/"+svc.ID+"/backups/nonexistent.sql.gz/download", nil)
	reqMissing.Header.Set("Authorization", "Bearer "+ownerToken(t, a))
	recMissing := httptest.NewRecorder()
	router.ServeHTTP(recMissing, reqMissing)

	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing backup, got %d", recMissing.Code)
	}
}

func TestServiceBackupRestoreValidation(t *testing.T) {
	a, svc := setupTestBackupService(t)
	router := a.Router()

	// Incorrect confirmation
	body := `{"filename":"dump_1.sql.gz","confirmDatabase":"wrong-name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/services/"+svc.ID+"/backups/restore", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ownerToken(t, a))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for mismatched confirmation, got %d: %s", rec.Code, rec.Body.String())
	}
}
