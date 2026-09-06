package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestEncryptedStateRestoreAndIntegrity(t *testing.T) {
	t.Setenv("RACKOPS_STATE_KEY_FILE", "")
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Mutate(func() {
		s.Services["db"] = &types.Service{ID: "db", GitToken: "stored-git-secret", Env: []types.EnvVar{{Key: "PASSWORD", Value: "stored-password"}}}
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(sealed, stateMagic) || bytes.Contains(sealed, []byte("stored-password")) || json.Valid(sealed) {
		t.Fatal("state was not encrypted or could be misread as legacy JSON")
	}
	// Restore into an independent directory using the separately saved key.
	restore := t.TempDir()
	key, _ := os.ReadFile(filepath.Join(dir, "state.key"))
	if err := os.WriteFile(filepath.Join(restore, "state.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restore, "state.json"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(restore)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Services["db"].Env[0].Value != "stored-password" || reopened.Services["db"].GitToken != "stored-git-secret" {
		t.Fatal("restored data mismatch")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	// Damaged data and wrong keys cannot initialize an empty replacement store.
	corrupt := append([]byte(nil), sealed...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := decryptState(key, corrupt); err == nil {
		t.Fatal("accepted tampered ciphertext")
	}
	wrong := make([]byte, 32)
	if _, err := decryptState(wrong, sealed); err == nil {
		t.Fatal("accepted wrong key")
	}
	missingKey := t.TempDir()
	if err := os.WriteFile(filepath.Join(missingKey, "state.json"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(missingKey); err == nil {
		t.Fatal("opened encrypted state without original key")
	}
}

func TestLegacyStateMigratesAndEmptyStateFailsClosed(t *testing.T) {
	t.Setenv("RACKOPS_STATE_KEY_FILE", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"instanceName":"legacy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.InstanceName != "legacy" {
		t.Fatal("lost legacy metadata")
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.HasPrefix(onDisk, stateMagic) {
		t.Fatal("legacy snapshot remained plaintext")
	}
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "state.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(empty); err == nil {
		t.Fatal("empty state silently reset installation")
	}
}
