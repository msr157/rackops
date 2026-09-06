package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// recoveryError means a command may have changed the active workload. The
// server must pause automated retries until the operator inspects its data.
type recoveryError struct{ err error }

func (e *recoveryError) Error() string {
	return "deployment requires inspection before recovery: " + e.err.Error()
}
func (e *recoveryError) Unwrap() error { return e.err }

type runtimeOutput func(...string) ([]byte, error)

// Retain by container ID, after validating RackOps ownership. Never remove the
// old container or its volumes, and never mistake an unavailable daemon for an
// absent container. Retained containers are excluded from active inventory.
func retainPrevious(p DeployPayload, run runtimeOutput) (string, error) {
	data, err := run("ps", "-a", "--format", "{{.ID}}|{{.Names}}")
	if err != nil {
		return "", fmt.Errorf("cannot verify current container: %w", err)
	}
	id := ""
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 2)
		if len(fields) != 2 {
			return "", fmt.Errorf("invalid current-container inventory")
		}
		if fields[1] == containerName(p) {
			id = fields[0]
		}
	}
	if id == "" {
		return "", nil
	}
	data, err = run("inspect", "--format", "{{json .Config.Labels}}", id)
	if err != nil {
		return "", fmt.Errorf("cannot verify container ownership: %w", err)
	}
	var labels map[string]string
	if json.Unmarshal(data, &labels) != nil || labels["rackops.service"] != p.ServiceID {
		return "", fmt.Errorf("existing container is not owned by this RackOps service")
	}
	nonce := make([]byte, 6)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	retained := containerName(p) + "-rackops-retained-" + hex.EncodeToString(nonce)
	// Docker's "always" policy can restart a manually stopped container after
	// daemon restart. Retained revisions must never become a second writer.
	if _, err := run("update", "--restart=no", id); err != nil {
		return "", &recoveryError{fmt.Errorf("disabling previous container restart did not complete: %w", err)}
	}
	if _, err := run("stop", "--time", "30", id); err != nil {
		return "", &recoveryError{fmt.Errorf("stopping previous container did not complete: %w", err)}
	}
	if _, err := run("rename", id, retained); err != nil {
		return "", &recoveryError{fmt.Errorf("previous container remains stopped under its original name: %w", err)}
	}
	return retained, nil
}

func retainedContainer(name string) bool {
	const marker = "-rackops-retained-"
	index := strings.LastIndex(name, marker)
	if index < 0 {
		return false
	}
	suffix := name[index+len(marker):]
	_, err := hex.DecodeString(suffix)
	return len(suffix) == 12 && err == nil
}

func containerHealth(data []byte) (bool, error) {
	var state struct {
		Status string `json:"Status"`
		Health *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	}
	if json.Unmarshal(data, &state) != nil || state.Status == "" {
		return false, fmt.Errorf("invalid container health response")
	}
	if state.Status != "running" {
		return false, fmt.Errorf("container is %s", state.Status)
	}
	if state.Health == nil {
		return true, nil
	}
	switch state.Health.Status {
	case "healthy":
		return true, nil
	case "starting":
		return false, nil
	default:
		return false, fmt.Errorf("container health is %s", state.Health.Status)
	}
}
