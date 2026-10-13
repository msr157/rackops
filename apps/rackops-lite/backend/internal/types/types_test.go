package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNode_JSONSerialization(t *testing.T) {
	node := Node{
		ID:       "node-123",
		Name:     "Mainframe Node",
		Role:     RoleServer,
		Status:   NodeOnline,
		IP:       "192.168.1.10",
		MeshIP:   "10.77.0.1",
		PublicIP: "1.2.3.4",
		OS:       "linux",
		Arch:     "amd64",
		JoinedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		Hardware: Hardware{
			CPUModel:   "AMD Ryzen 9",
			CPUCores:   16,
			CPUThreads: 32,
			MemTotalMB: 65536,
		},
		Metrics: Metrics{
			CPUPct:     12.5,
			MemUsedMB:  16384,
			UptimeSec:  86400,
			Containers: 8,
		},
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("failed to marshal Node: %v", err)
	}

	var parsed Node
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal Node: %v", err)
	}

	if parsed.ID != node.ID || parsed.Role != RoleServer || parsed.Hardware.CPUModel != "AMD Ryzen 9" {
		t.Errorf("roundtrip node mismatch: got %+v, want %+v", parsed, node)
	}
}

func TestService_JSONSerialization(t *testing.T) {
	svc := Service{
		ID:            "svc-db",
		Name:          "Postgres Production",
		Kind:          KindDatabase,
		Image:         "postgres:16-alpine",
		Port:          5432,
		ContainerPort: 5432,
		Status:        StatusRunning,
		AutoPlace:     true,
		NodeID:        "node-123",
	}

	data, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("failed to marshal Service: %v", err)
	}

	var parsed Service
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal Service: %v", err)
	}

	if parsed.ID != svc.ID || parsed.Port != 5432 || parsed.Image != svc.Image || parsed.Kind != KindDatabase {
		t.Errorf("roundtrip service mismatch: got %+v, want %+v", parsed, svc)
	}
}

func TestDomain_WildcardBase(t *testing.T) {
	d1 := Domain{Name: "*.example.com", Wildcard: true}
	if got := d1.WildcardBase(); got != "example.com" {
		t.Errorf("expected example.com, got %q", got)
	}

	d2 := Domain{Name: "app.example.com", Wildcard: false}
	if got := d2.WildcardBase(); got != "" {
		t.Errorf("expected empty string for non-wildcard, got %q", got)
	}
}

func TestUser_Sanitized(t *testing.T) {
	u := &User{
		ID:             "usr-1",
		Email:          "admin@example.com",
		PasswordHash:   "secret-hash-12345",
		ResetTokenHash: "reset-token-hash",
		ResetExpires:   time.Now().Add(time.Hour),
		Role:           RoleOwner,
	}

	clean := u.Sanitized()
	if clean.PasswordHash != "" || clean.ResetTokenHash != "" || !clean.ResetExpires.IsZero() {
		t.Errorf("Sanitized user still contained sensitive fields: %+v", clean)
	}
	if clean.Email != u.Email || clean.Role != RoleOwner {
		t.Errorf("Sanitized user dropped valid public fields: %+v", clean)
	}
}

func TestAPIToken_Sanitized(t *testing.T) {
	tok := &APIToken{
		ID:        "tok-1",
		UserID:    "usr-1",
		Name:      "CI token",
		TokenHash: "secret-token-hash",
	}

	clean := tok.Sanitized()
	if clean.TokenHash != "" {
		t.Errorf("expected empty TokenHash, got %q", clean.TokenHash)
	}
	if clean.Name != "CI token" {
		t.Errorf("expected CI token, got %q", clean.Name)
	}
}

func TestFrontDoorSpec_Equal(t *testing.T) {
	s1 := FrontDoorSpec{
		Enabled:     true,
		POPAddr:     "pop.rackops.in:443",
		TenantID:    "t-1",
		LocalTarget: "127.0.0.1:8099",
		Hostnames:   []string{"app1.link.rackops.in", "app2.link.rackops.in"},
	}

	s2 := FrontDoorSpec{
		Enabled:     true,
		POPAddr:     "pop.rackops.in:443",
		TenantID:    "t-1",
		LocalTarget: "127.0.0.1:8099",
		Hostnames:   []string{"app1.link.rackops.in", "app2.link.rackops.in"},
	}

	if !s1.Equal(s2) {
		t.Errorf("expected s1 == s2")
	}

	s3 := s2
	s3.Hostnames = []string{"app1.link.rackops.in"}
	if s1.Equal(s3) {
		t.Errorf("expected s1 != s3 when hostnames differ")
	}
}

func TestOrganization_Member(t *testing.T) {
	org := &Organization{
		ID:   "org-1",
		Name: "Acme Corp",
		Members: []OrgMember{
			{UserID: "u-1", Role: OrgRoleOwner},
			{UserID: "u-2", Role: OrgRoleMember},
		},
	}

	m1 := org.Member("u-1")
	if m1 == nil || m1.Role != OrgRoleOwner {
		t.Errorf("expected u-1 owner, got %+v", m1)
	}

	mNone := org.Member("u-999")
	if mNone != nil {
		t.Errorf("expected nil for non-existent member, got %+v", mNone)
	}
}

