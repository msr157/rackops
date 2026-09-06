package server

import (
	"bytes"
	"github.com/rackops/rackops/internal/types"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupOwnerOnlyAndRestoreRefusesOverwrite(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner, types.RoleAdmin, types.RoleMember)
	for _, role := range []string{"admin", "member"} {
		r := do(a, "GET", "/api/backups/state", "", map[string]string{"Authorization": "Bearer " + loginToken(t, a, role+"@test.local")})
		if r.Code != 403 {
			t.Fatalf("%s exported state: %d", role, r.Code)
		}
	}
	r := do(a, "GET", "/api/backups/state", "", map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")})
	if r.Code != 200 || !bytes.HasPrefix(r.Body.Bytes(), stateMagic) || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("backup: %d", r.Code)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if err := RestoreStateSnapshot(dest, r.Body.Bytes(), a.Store.stateKey); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dest, "state.json"))
	if err := RestoreStateSnapshot(dest, r.Body.Bytes(), a.Store.stateKey); err == nil {
		t.Fatal("restore overwrote an existing directory")
	}
	after, _ := os.ReadFile(filepath.Join(dest, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed restore changed previous state")
	}
	s, err := NewStore(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Users) != len(a.Store.Users) {
		t.Fatal("restore lost accounts")
	}
}
