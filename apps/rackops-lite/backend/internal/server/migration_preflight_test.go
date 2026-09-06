package server

import (
	"encoding/json"
	"github.com/rackops/rackops/internal/types"
	"strings"
	"testing"
	"time"
)

func TestMigrationReviewProtectsSourceAndSecrets(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Nodes["n"] = &types.Node{ID: "n", OrgID: org, OS: "linux", Arch: "amd64", Status: types.NodeOnline, LastSeen: time.Now()}
		a.Store.Services["app"] = &types.Service{ID: "app", ProjectID: "p", NodeID: "n", Kind: types.KindDatabase, Image: "postgres:16", GPU: true, GitToken: "fixture-secret", Mounts: []types.Mount{{Source: "/private/data"}}, Env: []types.EnvVar{{Key: "PASSWORD", Value: "fixture-secret"}}}
	})
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	for _, provider := range []string{"aws", "azure", "gcp"} {
		r := do(a, "POST", "/api/services/app/migration-preflight", `{"provider":"`+provider+`","region":"test-region","architecture":"arm64"}`, headers)
		if r.Code != 200 {
			t.Fatalf("review: %d %s", r.Code, r.Body)
		}
		var review migrationReview
		if err := json.Unmarshal(r.Body.Bytes(), &review); err != nil {
			t.Fatal(err)
		}
		if review.CanStart || review.CostEstimate != nil || review.DataBytes != nil || review.DowntimeSeconds != nil {
			t.Fatal("review promised unverified migration")
		}
		for _, code := range []string{"architecture_change", "gpu", "storage_backup", "database_backup", "cloud_validation", "migration_engine"} {
			found := false
			for _, b := range review.Blockers {
				if b.Code == code {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s", code)
			}
		}
		if strings.Contains(r.Body.String(), "fixture-secret") || strings.Contains(r.Body.String(), "/private/data") {
			t.Fatal("review leaked configuration")
		}
	}
	if a.Store.Services["app"].NodeID != "n" || len(a.Store.TaskQueue) != 0 {
		t.Fatal("preflight mutated source")
	}
	if r := do(a, "POST", "/api/services/app/migration-preflight", `{"provider":"unknown","region":"x"}`, headers); r.Code != 400 {
		t.Fatal("invalid provider accepted")
	}
}
