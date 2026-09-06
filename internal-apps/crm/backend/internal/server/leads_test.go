package server

import "testing"

func TestLeadInboxAndRetryIsolation(t *testing.T) {
	e := newTestEnv(t)
	body := map[string]any{"installId": testInstallID, "id": "lead-fixture", "email": "lead@example.com", "name": "Example", "industry": "Software", "notes": "Private enquiry"}
	if r := e.doJSON(t, "POST", "/ingest/v1/leads", "", body, nil); r.StatusCode != 200 {
		t.Fatalf("ingest: %d", r.StatusCode)
	}
	if r := e.doJSON(t, "GET", "/api/leads", "", nil, nil); r.StatusCode != 401 {
		t.Fatal("public lead inbox")
	}
	if r := e.doJSON(t, "POST", "/api/leads/lead-fixture/status", "", map[string]string{"installId": testInstallID, "status": "closed"}, nil); r.StatusCode != 401 {
		t.Fatal("public lead mutation")
	}
	token := e.login(t)
	if r := e.doJSON(t, "POST", "/api/leads/lead-fixture/status", token, map[string]string{"installId": "another-install", "status": "closed"}, nil); r.StatusCode != 404 {
		t.Fatal("wrong installation matched")
	}
	if r := e.doJSON(t, "POST", "/api/leads/lead-fixture/status", token, map[string]string{"installId": testInstallID, "status": "qualified"}, nil); r.StatusCode != 204 {
		t.Fatal("status update failed")
	}
	body["notes"] = "retry must not overwrite"
	e.doJSON(t, "POST", "/ingest/v1/leads", "", body, nil)
	var leads []Lead
	e.doJSON(t, "GET", "/api/leads", token, nil, &leads)
	if len(leads) != 1 || leads[0].Status != "qualified" || leads[0].Notes != "Private enquiry" {
		t.Fatalf("duplicate changed lead: %+v", leads)
	}
}
