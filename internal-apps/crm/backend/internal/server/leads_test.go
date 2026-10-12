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

func TestLeadRedactionKeepsRetryTombstone(t *testing.T) {
	e := newTestEnv(t)
	token := e.login(t)
	body := map[string]any{"installId": testInstallID, "id": "lead-retention", "email": "private@example.com", "name": "Private", "industry": "Software", "notes": "Private note"}
	e.doJSON(t, "POST", "/ingest/v1/leads", "", body, nil)
	var leads []Lead
	e.doJSON(t, "GET", "/api/leads", token, nil, &leads)
	req := map[string]string{"installId": testInstallID, "receivedAt": leads[0].ReceivedAt, "expectedStatus": "spam"}
	path := "/api/leads/lead-retention/redact"
	if r := e.doJSON(t, "POST", path, "", req, nil); r.StatusCode != 401 {
		t.Fatal("unauthenticated redaction")
	}
	if r := e.doJSON(t, "POST", path, token, req, nil); r.StatusCode != 409 {
		t.Fatal("active lead redacted")
	}
	if r := e.doJSON(t, "POST", "/api/leads/lead-retention/status", token, map[string]string{"installId": testInstallID, "status": "spam"}, nil); r.StatusCode != 204 {
		t.Fatal("spam unavailable")
	}
	req["installId"] = "wrong-install"
	if r := e.doJSON(t, "POST", path, token, req, nil); r.StatusCode != 409 {
		t.Fatal("wrong install redacted")
	}
	req["installId"] = testInstallID
	for i := 0; i < 2; i++ {
		if r := e.doJSON(t, "POST", path, token, req, nil); r.StatusCode != 204 {
			t.Fatal("redaction retry failed")
		}
	}
	e.doJSON(t, "POST", "/ingest/v1/leads", "", body, nil)
	e.doJSON(t, "GET", "/api/leads", token, nil, &leads)
	if len(leads) != 1 || leads[0].Status != "redacted" || leads[0].Email != "" || leads[0].Name != "" || leads[0].Notes != "" || leads[0].Industry != "" {
		t.Fatal("retry restored contact details")
	}
	if r := e.doJSON(t, "POST", "/api/leads/lead-retention/status", token, map[string]string{"installId": testInstallID, "status": "new"}, nil); r.StatusCode != 404 {
		t.Fatal("redacted tombstone reopened")
	}
}
