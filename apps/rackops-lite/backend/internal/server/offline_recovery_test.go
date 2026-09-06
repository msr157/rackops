package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rackops/rackops/internal/types"
	"golang.org/x/crypto/bcrypt"
)

func TestOfflineRecoveryPreservesSourceAndRejectsOldCredentials(t *testing.T) {
	t.Setenv("RACKOPS_STATE_KEY_FILE", "")
	a, ids := testAPI(t, types.RoleOwner)
	oldToken := loginToken(t, a, "owner@test.local")
	a.Store.Mutate(func() {
		a.Store.Tokens["key"] = &types.APIToken{ID: "key", UserID: ids[types.RoleOwner]}
		a.Store.Services["app"] = &types.Service{ID: "app", Env: []types.EnvVar{{Key: "DATABASE_PASSWORD", Value: "preserved-app-secret"}}}
	})
	sealed, err := a.Store.EncryptedSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), sealed...)
	target := filepath.Join(t.TempDir(), "recovered")
	if err := RecoverAccountSnapshot(target, sealed, a.Store.stateKey, "owner@test.local", "new-owner-password"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, sealed) {
		t.Fatal("source snapshot mutated")
	}
	if response := do(a, "GET", "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + oldToken}); response.Code != 200 {
		t.Fatal("source installation modified")
	}
	recovered, err := NewStore(target)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	user := recovered.Users[ids[types.RoleOwner]]
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("new-owner-password")) != nil {
		t.Fatal("recovery password not set")
	}
	if _, err := parseJWT(recovered.SessionSecret(), oldToken); err == nil {
		t.Fatal("old JWT accepted by recovered installation")
	}
	if len(recovered.Tokens) != 0 || recovered.Services["app"].Env[0].Value != "preserved-app-secret" {
		t.Fatal("credential revocation or app configuration preservation failed")
	}
	disk, err := os.ReadFile(filepath.Join(target, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("new-owner-password")) || bytes.Contains(disk, []byte("preserved-app-secret")) {
		t.Fatal("recovery wrote plaintext secrets")
	}
	if err := RecoverAccountSnapshot(target, sealed, a.Store.stateKey, "owner@test.local", "another-password"); err == nil {
		t.Fatal("overwrote recovery destination")
	}
}

func TestOfflineRecoveryRejectsBadInputBeforeCreatingDestination(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	sealed, err := a.Store.EncryptedSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		email, password string
		key             []byte
	}{
		{"missing@test.local", "long-enough-password", a.Store.stateKey},
		{"owner@test.local", "short", a.Store.stateKey},
		{"owner@test.local", "long-enough-password", make([]byte, 32)},
	} {
		target := filepath.Join(t.TempDir(), "rejected")
		if err := RecoverAccountSnapshot(target, sealed, tc.key, tc.email, tc.password); err == nil {
			t.Fatal("accepted invalid recovery input")
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("invalid recovery created destination")
		}
	}
}
