package edge

import "testing"

func TestWorkspaceReservation(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "one", PublicKey: testKey(t), Workspace: "team"}); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{"team", "Team", "two.labels", "-bad"} {
		if _, err := r.Upsert(Tenant{ID: "two", PublicKey: testKey(t), Workspace: workspace}); err == nil {
			t.Fatalf("accepted workspace %q", workspace)
		}
	}
	if _, ok := r.Get("two"); ok {
		t.Fatal("invalid registration mutated state")
	}
}
