package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestAssistantTenantIsolation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Create org-a and org-b
	orgA := &types.Organization{
		ID:   "org-a",
		Name: "Org Alpha",
		Members: []types.OrgMember{
			{UserID: "usr-a", Role: types.OrgRoleAdmin},
		},
	}
	orgB := &types.Organization{
		ID:   "org-b",
		Name: "Org Beta",
		Members: []types.OrgMember{
			{UserID: "usr-b", Role: types.OrgRoleAdmin},
		},
	}

	userA := &types.User{
		ID:    "usr-a",
		Email: "alpha@example.com",
		Role:  types.RoleAdmin,
	}

	// Add nodes: nodeA in orgA, nodeB in orgB
	nodeA := &types.Node{
		ID:     "node-a",
		OrgID:  "org-a",
		Name:   "alpha-worker-01",
		Status: types.NodeOnline,
		Hardware: types.Hardware{
			CPUModel:   "AMD EPYC 7763",
			MemTotalMB: 65536,
			Drives: []types.Drive{
				{Device: "/dev/nvme0n1", Type: "nvme", SizeGB: 1000, UsedGB: 200, Health: "PASSED"},
			},
		},
		Metrics: types.Metrics{CPUPct: 15.0, MemUsedMB: 8192, TempC: 42.0},
	}
	nodeB := &types.Node{
		ID:     "node-b",
		OrgID:  "org-b",
		Name:   "beta-secret-worker",
		Status: types.NodeOnline,
		Hardware: types.Hardware{
			CPUModel:   "Intel Xeon Platinum 8380",
			MemTotalMB: 131072,
			Drives: []types.Drive{
				{Device: "/dev/sda", Type: "ssd", SizeGB: 2000, UsedGB: 1500, Health: "PASSED"},
			},
		},
		Metrics: types.Metrics{CPUPct: 95.0, MemUsedMB: 120000, TempC: 88.0},
	}

	// Add services: svcA in orgA, svcB in orgB
	svcA := &types.Service{
		ID:        "svc-a",
		Name:      "alpha-frontend",
		ProjectID: "proj-a",
		NodeID:    "node-a",
		Status:    types.StatusRunning,
	}
	svcB := &types.Service{
		ID:        "svc-b",
		Name:      "beta-classified-service",
		ProjectID: "proj-b",
		NodeID:    "node-b",
		Status:    types.StatusFailed,
	}

	store.Mutate(func() {
		store.Orgs["org-a"] = orgA
		store.Orgs["org-b"] = orgB
		store.Users["usr-a"] = userA
		store.Nodes["node-a"] = nodeA
		store.Nodes["node-b"] = nodeB
		store.Services["svc-a"] = svcA
		store.Services["svc-b"] = svcB
		store.Projects["proj-a"] = &types.Project{ID: "proj-a", Name: "Project A", OrgID: "org-a"}
		store.Projects["proj-b"] = &types.Project{ID: "proj-b", Name: "Project B", OrgID: "org-b"}
	})

	api := &API{Store: store}

	// 1. Direct fleetContext check
	ctxA := api.fleetContext("org-a")
	if !strings.Contains(ctxA, "alpha-worker-01") || !strings.Contains(ctxA, "alpha-frontend") {
		t.Fatalf("expected org-a resources in fleetContext, got: %s", ctxA)
	}
	if strings.Contains(ctxA, "beta-secret-worker") || strings.Contains(ctxA, "beta-classified-service") {
		t.Fatalf("cross-tenant leak: org-b resources found in org-a fleetContext: %s", ctxA)
	}

	// 2. Direct builtinAssistant placement check
	ansWhere := api.builtinAssistant("where should I deploy", "org-a")
	if !strings.Contains(ansWhere, "alpha-worker-01") {
		t.Errorf("expected alpha-worker-01 in placement, got: %s", ansWhere)
	}
	if strings.Contains(ansWhere, "beta-secret-worker") {
		t.Errorf("cross-tenant leak: beta-secret-worker ranked in org-a placement: %s", ansWhere)
	}

	// 3. Direct builtinAssistant thermal check
	ansThermal := api.builtinAssistant("thermal state", "org-a")
	if !strings.Contains(ansThermal, "alpha-worker-01") {
		t.Errorf("expected alpha-worker-01 in thermal, got: %s", ansThermal)
	}
	if strings.Contains(ansThermal, "beta-secret-worker") || strings.Contains(ansThermal, "88°C") {
		t.Errorf("cross-tenant leak: beta thermal details revealed to org-a: %s", ansThermal)
	}

	// 4. Direct builtinAssistant failed services check
	ansFailed := api.builtinAssistant("why fail", "org-a")
	if strings.Contains(ansFailed, "beta-classified-service") {
		t.Errorf("cross-tenant leak: beta failed service revealed to org-a: %s", ansFailed)
	}

	// 5. Direct builtinAssistant storage check
	ansStorage := api.builtinAssistant("storage state", "org-a")
	if !strings.Contains(ansStorage, "alpha-worker-01") {
		t.Errorf("expected alpha-worker-01 in storage, got: %s", ansStorage)
	}
	if strings.Contains(ansStorage, "beta-secret-worker") {
		t.Errorf("cross-tenant leak: beta drives revealed to org-a: %s", ansStorage)
	}

	// 6. Direct builtinAssistant default fleet totals check
	ansDefault := api.builtinAssistant("hello assistant", "org-a")
	if !strings.Contains(ansDefault, "Fleet: 1/1 nodes online, 1 services running") {
		t.Errorf("expected scoped fleet totals (1/1 nodes, 1 services), got: %s", ansDefault)
	}

	handler := api.authMiddleware(http.HandlerFunc(api.handleAssistant))

	// 7. HTTP endpoint check with JWT
	tok := signJWT(store.SessionSecret(), jwtClaims{
		Sub:  "usr-a",
		Role: string(types.RoleAdmin),
		Exp:  time.Now().Add(time.Hour).Unix(),
	})

	body, _ := json.Marshal(map[string]any{
		"message": "where should I run my app?",
	})
	req := httptest.NewRequest("POST", "/api/assistant", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Rackops-Org", "org-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got: %d (%s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Answer   string `json:"answer"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.Contains(resp.Answer, "alpha-worker-01") {
		t.Errorf("expected answer to reference alpha-worker-01, got: %s", resp.Answer)
	}
	if strings.Contains(resp.Answer, "beta-secret-worker") {
		t.Errorf("answer leaked beta-secret-worker to org-a: %s", resp.Answer)
	}

	// 8. Unauthenticated denial
	unauthReq := httptest.NewRequest("POST", "/api/assistant", bytes.NewReader(body))
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401 for unauthenticated assistant call, got %d", unauthRec.Code)
	}
}
