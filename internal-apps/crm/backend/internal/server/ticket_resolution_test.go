package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolutionHTTPUsesPolledRevision(t *testing.T) {
	e := newTestEnv(t)
	secret := strings.Repeat("a", 64)
	response, _ := e.forwardTicket(t, "http-resolution", secret, "message")
	if response.StatusCode != 200 {
		t.Fatal("forward failed")
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/ingest/v1/tickets/http-resolution/replies?installId="+testInstallID+"&cursor=0", nil)
	req.Header.Set("X-Poll-Secret", secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var page struct {
		Revision *int64 `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil || page.Revision == nil {
		t.Fatal("missing resolution revision", err)
	}
	var outcome struct {
		Applied bool `json:"applied"`
	}
	body := map[string]any{"installId": testInstallID, "pollSecret": secret, "actionId": "resolve", "expectedRevision": *page.Revision}
	response = e.doJSON(t, "POST", "/ingest/v1/tickets/http-resolution/resolve", "", body, &outcome)
	if response.StatusCode != 200 || !outcome.Applied {
		t.Fatal("HTTP resolution failed")
	}
	body["pollSecret"] = "wrong"
	response = e.doJSON(t, "POST", "/ingest/v1/tickets/http-resolution/resolve", "", body, nil)
	if response.StatusCode != 403 {
		t.Fatal("receipt replay bypasses auth")
	}
	delete(body, "expectedRevision")
	response = e.doJSON(t, "POST", "/ingest/v1/tickets/http-resolution/resolve", "", body, nil)
	if response.StatusCode != 400 {
		t.Fatal("missing version accepted")
	}
}

func TestResolutionIsScopedVersionedAndReplaySafe(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 64)
	_, _, err = s.UpsertTicket(&Ticket{ID: "ticket", InstallID: testInstallID, Status: "open"}, time.Now(), secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveTicket("ticket", testInstallID, "wrong", "unauthorized", 0); err != errTicketCredential {
		t.Fatal("wrong credential accepted")
	}
	if _, err := s.ResolveTicket("ticket", "foreign", secret, "unauthorized", 0); err != errTicketCredential {
		t.Fatal("foreign install accepted")
	}
	applied, err := s.ResolveTicket("ticket", testInstallID, secret, "first", 0)
	if err != nil || !applied {
		t.Fatal("resolution failed", err)
	}
	if err := s.AddMessage("ticket", testInstallID, TicketMessage{ID: "reply", Author: "agent", Body: "new activity", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	applied, err = s.ResolveTicket("ticket", testInstallID, secret, "first", 0)
	if err != nil || !applied {
		t.Fatal("receipt lost on restart", err)
	}
	ticket, err := s.GetTicket("ticket", testInstallID)
	if err != nil || ticket.Status != "pending" {
		t.Fatal("retry overwrote newer agent reply")
	}
	applied, err = s.ResolveTicket("ticket", testInstallID, secret, "stale", 0)
	if err != nil || applied {
		t.Fatal("stale action accepted", err)
	}
	if _, err := s.ResolveTicket("ticket", testInstallID, secret, "first", 1); err == nil {
		t.Fatal("action identity can change")
	}
	var revision int64
	s.db.QueryRow(`SELECT revision FROM ticket_revisions WHERE ticket_id=? AND install_id=?`, "ticket", testInstallID).Scan(&revision)
	if err := s.SetTicketStatus("ticket", testInstallID, "open", time.Now()); err != nil {
		t.Fatal(err)
	}
	applied, err = s.ResolveTicket("ticket", testInstallID, secret, "manual-conflict", revision)
	if err != nil || applied {
		t.Fatal("manual status change overwritten", err)
	}
}
