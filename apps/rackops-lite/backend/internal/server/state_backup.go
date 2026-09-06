package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rackops/rackops/internal/types"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// EncryptedSnapshot never includes the separately managed encryption key.
func (s *Store) EncryptedSnapshot() ([]byte, error) {
	s.mu.RLock()
	data, err := json.Marshal(s)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return encryptState(s.stateKey, data)
}

func (a *API) handleStateBackup(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	owner := false
	if claims != nil {
		a.Store.View(func() { u := a.Store.Users[claims.Sub]; owner = u != nil && u.Role == types.RoleOwner })
	}
	if !owner {
		http.Error(w, "only the installation owner can export control-plane state", http.StatusForbidden)
		return
	}
	data, err := a.Store.EncryptedSnapshot()
	if err != nil {
		http.Error(w, "could not create encrypted snapshot", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rackops-state-%s.enc"`, time.Now().UTC().Format("20060102T150405Z")))
	_, _ = w.Write(data)
}

// RestoreStateSnapshot validates before writing and never overwrites a live
// install. Volume data and loose agent/mesh identity files are separate.
func RestoreStateSnapshot(dataDir string, sealed, key []byte) error {
	plain, err := decryptState(key, sealed)
	if err != nil {
		return err
	}
	var snapshot Store
	if err := json.Unmarshal(plain, &snapshot); err != nil {
		return err
	}
	if snapshot.SchemaVersion != 1 || len(snapshot.JWTSecret) < 32 || snapshot.Projects == nil || snapshot.Services == nil {
		return errors.New("snapshot has an unsupported schema or incomplete control-plane state")
	}
	if err := os.Mkdir(dataDir, 0700); err != nil {
		return fmt.Errorf("restore requires a new directory with an existing parent: %w", err)
	}
	if err := atomicStateWrite(filepath.Join(dataDir, "state.key"), key); err != nil {
		return err
	}
	return atomicStateWrite(filepath.Join(dataDir, "state.json"), sealed)
}
