package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func queuedReset(t *testing.T) (*API, string, time.Time) {
	t.Helper()
	a, ids := testAPI(t, types.RoleMember)
	id := ids[types.RoleMember]
	now := time.Now().UTC()
	a.Store.Mutate(func() {
		u := a.Store.Users[id]
		u.ResetTokenHash = "fixture-hash"
		u.ResetExpires = now.Add(time.Hour)
		a.Store.ResetEmails[id] = &ResetEmail{UserID: id, Email: u.Email, Link: "https://fixture.invalid/reset-password?token=fixture-secret", TokenHash: u.ResetTokenHash, ExpiresAt: u.ResetExpires}
	})
	return a, id, now
}

func TestResetDeliveryRetriesSameCredentialAfterRestart(t *testing.T) {
	a, id, now := queuedReset(t)
	requests := make(chan string, 8)
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Link string `json:"link"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(400)
			return
		}
		requests <- payload.Link
		if len(requests) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	a.deliverResetEmail(id, now)
	if len(requests) != 1 {
		t.Fatal("first delivery missing")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(filepath.Join(a.Store.dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("fixture-secret")) {
		t.Fatal("queued credential stored in plaintext")
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	a.retryResetEmails(now.Add(59 * time.Second))
	if len(requests) != 1 {
		t.Fatal("retry ignored persisted schedule")
	}
	a.retryResetEmails(now.Add(time.Minute))
	if len(requests) != 2 {
		t.Fatal("restart lost pending delivery")
	}
	if first, second := <-requests, <-requests; first != second {
		t.Fatal("retry changed credential")
	}
	a.Store.View(func() {
		if a.Store.ResetEmails[id] != nil {
			t.Error("successful delivery still queued")
		}
	})
}

func TestResetDeliveryDropsInvalidCredentials(t *testing.T) {
	for _, reason := range []string{"expired", "account-expired", "replaced", "deleted", "attempt-limit", "email-changed"} {
		t.Run(reason, func(t *testing.T) {
			a, id, now := queuedReset(t)
			calls := make(chan struct{}, 1)
			crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls <- struct{}{}; w.WriteHeader(204) }))
			defer crm.Close()
			t.Setenv("RACKOPS_CRM_URL", crm.URL)
			a.Store.Mutate(func() {
				switch reason {
				case "expired":
					a.Store.ResetEmails[id].ExpiresAt = now
				case "account-expired":
					a.Store.Users[id].ResetExpires = now
				case "replaced":
					a.Store.Users[id].ResetTokenHash = "new-hash"
				case "deleted":
					delete(a.Store.Users, id)
				case "attempt-limit":
					a.Store.ResetEmails[id].Attempts = 6
				case "email-changed":
					a.Store.Users[id].Email = "changed@test.local"
				}
			})
			a.retryResetEmails(now)
			if len(calls) != 0 {
				t.Fatal("invalid reset credential delivered")
			}
			a.Store.View(func() {
				if a.Store.ResetEmails[id] != nil {
					t.Error("invalid delivery retained")
				}
			})
		})
	}
}

func TestResetDeliveryDoesNotSendWithoutDurableState(t *testing.T) {
	a, id, now := queuedReset(t)
	calls := make(chan struct{}, 1)
	crm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls <- struct{}{}
		w.WriteHeader(204)
	}))
	defer crm.Close()
	t.Setenv("RACKOPS_CRM_URL", crm.URL)
	// Stop background persistence before injecting an invalid destination.
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	original := a.Store.dataDir
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Store.dataDir = blocked
	a.deliverResetEmail(id, now)
	a.Store.dataDir = original
	if len(calls) != 0 {
		t.Fatal("sent credential before persisting retry intent")
	}
	a.Store.View(func() {
		if a.Store.ResetEmails[id] == nil {
			t.Error("disk failure discarded delivery")
		}
	})
	a.retryResetEmails(now.Add(time.Minute))
	if len(calls) != 1 {
		t.Fatal("delivery did not recover after storage became writable")
	}
}
