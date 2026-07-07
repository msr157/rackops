package agent

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NodeID returns this machine's stable identifier, generating and persisting
// one under the user config dir (e.g. %AppData%\rackops-agent\node-id on
// Windows, ~/.config/rackops-agent/node-id on Linux) on first run.
func NodeID() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	path := filepath.Join(dir, "rackops-agent", "node-id")

	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}

	id, err := newUUID()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist node id: %w", err)
	}
	return id, nil
}

// newUUID generates a random RFC 4122 v4 UUID without external deps.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
