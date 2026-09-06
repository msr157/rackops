package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type savedNodeCredential struct {
	ServerURL  string `json:"serverUrl"`
	NodeID     string `json:"nodeId"`
	Credential string `json:"credential"`
}

func enrollAgent(ctx context.Context, cfg *Config, nodeID string, j *taskJournal, client *http.Client) error {
	if !strings.HasPrefix(cfg.Token, "rkenroll_") {
		return nil
	}
	server := strings.TrimRight(cfg.ServerURL, "/")
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid enrollment server URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return errors.New("node enrollment requires HTTPS (except loopback development)")
	}
	path := filepath.Join(j.dir, "node-credential")
	var identity savedNodeCredential
	sealed, err := os.ReadFile(path)
	if err == nil {
		plain, err := j.cipher.Open(nil, nil, sealed, []byte("node-credential"))
		if err != nil || json.Unmarshal(plain, &identity) != nil {
			return errors.New("saved node credential is damaged or its key is unavailable")
		}
		if identity.ServerURL != server || identity.NodeID != nodeID || !strings.HasPrefix(identity.Credential, "rknode_") {
			return errors.New("saved node credential belongs to another server or node")
		}
	} else if os.IsNotExist(err) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		identity = savedNodeCredential{ServerURL: server, NodeID: nodeID, Credential: "rknode_" + hex.EncodeToString(raw)}
		plain, _ := json.Marshal(identity)
		sealed = j.cipher.Seal(nil, nil, plain, []byte("node-credential"))
		// Persist before sending. Never overwrite an existing identity, including
		// when a second agent process races this initialization.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("could not create node credential")
		}
		_, err = file.Write(sealed)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return errors.New("could not persist node credential")
		}
	} else {
		return errors.New("could not read node credential")
	}
	body, _ := json.Marshal(map[string]string{"nodeId": nodeID, "credential": identity.Credential})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/agent/enroll", bytes.NewReader(body))
	if err != nil {
		return errors.New("could not build enrollment request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Rackops-Token", cfg.Token)
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(request)
	if err != nil {
		return errors.New("enrollment server unavailable; retry with the same data directory")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("enrollment rejected; check token expiry, server and node identity")
	}
	var result struct {
		NodeID string `json:"nodeId"`
		OrgID  string `json:"orgId"`
	}
	if json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4096)).Decode(&result) != nil || result.NodeID != nodeID || result.OrgID == "" || cfg.OrgID != "" && cfg.OrgID != result.OrgID {
		return errors.New("enrollment response does not match requested identity")
	}
	cfg.Token = identity.Credential
	cfg.OrgID = result.OrgID
	return nil
}
