package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The RackOps CRM is the vendor-hosted service behind support tickets and
// password-reset emails. Everything here is OUTBOUND-only — self-hosted
// installs are typically behind NAT, so the CRM can never call back; replies
// are fetched by polling. The URL is compiled in on purpose: customers must
// never configure (or be able to redirect) vendor infrastructure from the
// dashboard. RACKOPS_CRM_URL exists for development against a local CRM only.
const defaultCRMURL = "https://crm.rackops.in"

func crmURL() string {
	if v := os.Getenv("RACKOPS_CRM_URL"); v != "" {
		return v
	}
	return defaultCRMURL
}

var crmClient = &http.Client{Timeout: 10 * time.Second}

// InstallID is a random, non-identifying UUID minted once per install. It
// lets the CRM correlate tickets and snapshots from the same instance without
// knowing anything else about it.
func LoadOrCreateInstallID(dataDir string) string {
	path := filepath.Join(dataDir, "install-id")
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return string(bytes.TrimSpace(b))
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	id := fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		log.Printf("crm: could not persist install id: %v", err)
	}
	return id
}

// crmPost sends one JSON payload to a CRM ingest endpoint.
func crmPost(path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := crmClient.Post(crmURL()+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("crm: %s returned %s", path, resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// relayResetEmail asks the CRM to send the password-reset email from the
// support address. Failure is logged without the recovery link or recipient.
func (a *API) relayResetEmail(email, link string) error {
	var name string
	a.Store.View(func() { name = a.Store.InstanceName })
	err := crmPost("/ingest/v1/reset-email", map[string]any{
		"installId":    a.InstallID,
		"instanceName": name,
		"email":        email,
		"link":         link,
		"version":      ServerVersion,
	}, nil)
	if err != nil {
		log.Print("auth: reset email relay unavailable; check CRM/SMTP delivery configuration")
	}
	return err
}
