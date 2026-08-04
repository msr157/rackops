package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestFoldAveragesWithinBucketAndCaps(t *testing.T) {
	var ring []Sample
	n := 0

	// Two readings in the same bucket average; a third in the next appends.
	ring, n = fold(ring, n, 60, 3, 10, 100, 40)
	ring, n = fold(ring, n, 60, 3, 30, 300, 60)
	if len(ring) != 1 || n != 2 {
		t.Fatalf("same bucket: len=%d n=%d", len(ring), n)
	}
	if ring[0].CPU != 20 || ring[0].Mem != 200 || ring[0].Temp != 50 {
		t.Fatalf("average wrong: %+v", ring[0])
	}
	ring, n = fold(ring, n, 120, 3, 50, 500, 0)
	if len(ring) != 2 || n != 1 {
		t.Fatalf("new bucket: len=%d n=%d", len(ring), n)
	}

	// The ring never exceeds its cap; oldest buckets fall off the front.
	ring, _ = fold(ring, n, 180, 3, 1, 1, 0)
	ring, _ = fold(ring, 1, 240, 3, 2, 2, 0)
	if len(ring) != 3 || ring[0].T != 120 {
		t.Fatalf("cap: len=%d first=%d", len(ring), ring[0].T)
	}
}

func TestObserveSkipsPlumbingContainers(t *testing.T) {
	h := &History{Nodes: map[string]*series{}, Services: map[string]*series{}}
	h.Observe("n1", types.Metrics{CPUPct: 12, MemUsedMB: 1024, TempC: 51}, []types.ContainerStatus{
		{ServiceID: "svc1", CPUPct: 5, MemUsedMB: 64},
		{ServiceID: "svc2", Tunnel: true},        // tunnel helper: plumbing
		{ServiceID: ingressServiceID, CPUPct: 1}, // edge pseudo-service
		{ServiceID: ""},                          // unmanaged container
	})
	if len(h.NodePoints("n1", true)) != 1 {
		t.Fatal("node sample not recorded")
	}
	if len(h.Services) != 1 || h.Services["svc1"] == nil {
		t.Fatalf("want exactly svc1 recorded, got %d series", len(h.Services))
	}
	// Nil receiver: every read answers empty, ingest is a no-op.
	var nilH *History
	nilH.Observe("n1", types.Metrics{}, nil)
	if pts := nilH.NodePoints("n1", true); len(pts) != 0 {
		t.Fatal("nil history must answer empty")
	}
}

func TestFleetPointsAveragesCPUAndSumsMem(t *testing.T) {
	h := &History{Nodes: map[string]*series{
		"a": {Fine: []Sample{{T: 60, CPU: 10, Mem: 100}, {T: 120, CPU: 20, Mem: 100}}},
		"b": {Fine: []Sample{{T: 60, CPU: 30, Mem: 50}}}, // offline by 120: leaves a gap
	}, Services: map[string]*series{}}

	pts := h.FleetPoints([]string{"a", "b"}, true)
	if len(pts) != 2 {
		t.Fatalf("want 2 buckets, got %d", len(pts))
	}
	if pts[0].T != 60 || pts[0].CPU != 20 || pts[0].Mem != 150 {
		t.Fatalf("bucket 60 wrong: %+v", pts[0])
	}
	// The gap bucket averages only over nodes that reported — no zero-fill.
	if pts[1].T != 120 || pts[1].CPU != 20 || pts[1].Mem != 100 {
		t.Fatalf("bucket 120 wrong: %+v", pts[1])
	}
}

func TestMetricsHistoryEndpointOrgScoping(t *testing.T) {
	a, _ := testAPI(t, types.RoleAdmin)
	token := loginToken(t, a, "admin@test.local")
	hdr := map[string]string{"Authorization": "Bearer " + token}

	var defaultOrg string
	a.Store.View(func() {
		for id := range a.Store.Orgs {
			defaultOrg = id
		}
	})
	a.Store.Mutate(func() {
		a.Store.Nodes["mine"] = &types.Node{ID: "mine", OrgID: defaultOrg, JoinedAt: time.Now()}
		a.Store.Nodes["theirs"] = &types.Node{ID: "theirs", OrgID: "some-other-org", JoinedAt: time.Now()}
	})
	a.History = &History{Nodes: map[string]*series{
		"mine":   {Fine: []Sample{{T: 60, CPU: 10, Mem: 100}}},
		"theirs": {Fine: []Sample{{T: 60, CPU: 99, Mem: 999}}},
	}, Services: map[string]*series{}}

	// Own node: 200 with its points.
	rec := do(a, http.MethodGet, "/api/metrics/history?scope=node&id=mine", "", hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("own node: status %d: %s", rec.Code, rec.Body)
	}
	var resp historyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Points) != 1 || resp.Points[0].CPU != 10 || resp.StepSec != fineStep {
		t.Fatalf("own node points wrong: %+v", resp)
	}

	// A node outside the active org must be indistinguishable from absent.
	rec = do(a, http.MethodGet, "/api/metrics/history?scope=node&id=theirs", "", hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign node: want 404, got %d", rec.Code)
	}

	// Fleet scope only merges the org's own machines.
	rec = do(a, http.MethodGet, "/api/metrics/history?scope=fleet", "", hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("fleet: status %d: %s", rec.Code, rec.Body)
	}
	resp = historyResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Points) != 1 || resp.Points[0].Mem != 100 {
		t.Fatalf("fleet must exclude foreign nodes: %+v", resp.Points)
	}

	// Bad window is a client error, not a silent default.
	rec = do(a, http.MethodGet, "/api/metrics/history?window=3d", "", hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad window: want 400, got %d", rec.Code)
	}
}
