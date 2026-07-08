package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// deployCompose writes the stack's compose file to a managed directory and
// brings it up with `<runtime> compose up -d`.
func (r *Runtime) deployCompose(p DeployPayload, logs *strings.Builder) (string, string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".rackops", "stacks", sanitize(p.Name)+"-"+p.ServiceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(p.ComposeYML), 0o644); err != nil {
		return "", "", err
	}

	// Env vars become the stack's .env file.
	if len(p.Env) > 0 {
		var env strings.Builder
		for _, e := range p.Env {
			env.WriteString(e.Key + "=" + e.Value + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env.String()), 0o600); err != nil {
			return "", "", err
		}
	}

	project := "rackops-" + sanitize(p.Name)
	args := []string{"compose", "-p", project, "up", "-d", "--remove-orphans"}
	logs.WriteString("$ " + r.Bin + " " + strings.Join(args, " ") + "\n")
	cmd := exec.Command(r.Bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	logs.Write(out)
	if err != nil {
		return "", logs.String(), fmt.Errorf("compose up failed: %w", err)
	}

	// Label the stack's containers so List() picks them up. compose doesn't
	// apply external labels post-hoc, so we track by project name instead:
	// report the project as the "container ID".
	logs.WriteString("stack up: " + project + "\n")
	return project, logs.String(), nil
}

// composeDown tears a stack down.
func (r *Runtime) composeDown(p DeployPayload) (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".rackops", "stacks", sanitize(p.Name)+"-"+p.ServiceID)
	project := "rackops-" + sanitize(p.Name)
	cmd := exec.Command(r.Bin, "compose", "-p", project, "down")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
