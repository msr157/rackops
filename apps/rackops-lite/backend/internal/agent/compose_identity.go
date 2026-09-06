package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rackops/rackops/internal/types"
)

var composeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func composeLocation(p DeployPayload) (string, string, error) {
	if !composeID.MatchString(p.ServiceID) {
		return "", "", fmt.Errorf("invalid Compose service identity")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, ".rackops", "stacks", sanitize(p.Name)+"-"+p.ServiceID)
	project := "rackops-" + p.ServiceID
	// Preserve existing volume/network names for legacy stacks. Ownership is
	// verified against the working directory before every runtime operation.
	if p.ContainerID == "rackops-"+sanitize(p.Name) {
		project = p.ContainerID
	}
	return dir, project, nil
}

func verifyComposeOwnership(dir, project string, run runtimeOutput) error {
	data, err := run("ps", "-aq", "--filter", "label=com.docker.compose.project="+project)
	if err != nil {
		return fmt.Errorf("cannot verify Compose ownership: %w", err)
	}
	for _, id := range strings.Fields(string(data)) {
		data, err = run("inspect", "--format", "{{json .Config.Labels}}", id)
		if err != nil {
			return fmt.Errorf("cannot inspect Compose ownership: %w", err)
		}
		var labels map[string]string
		if json.Unmarshal(data, &labels) != nil || labels["com.docker.compose.project"] != project || labels["com.docker.compose.project.working_dir"] == "" || filepath.Clean(labels["com.docker.compose.project.working_dir"]) != filepath.Clean(dir) {
			return fmt.Errorf("Compose project contains containers from another or unverified stack; inspect ownership before continuing")
		}
	}
	return nil
}

// Single quotes suppress Compose interpolation, preserving passwords with $
// and spaces. Reject control characters rather than generating extra entries.
func composeEnvironment(env []types.EnvVar) ([]byte, error) {
	var out strings.Builder
	seen := map[string]bool{}
	for _, entry := range env {
		if !envKey.MatchString(entry.Key) || seen[entry.Key] || strings.HasPrefix(entry.Key, "COMPOSE_") {
			return nil, fmt.Errorf("invalid, duplicate or reserved Compose environment key")
		}
		if strings.ContainsAny(entry.Value, "\x00\r\n") || strings.HasSuffix(entry.Value, "\\") || strings.Contains(entry.Value, "\\'") {
			return nil, fmt.Errorf("Compose environment values cannot contain control characters, a backslash before a single quote, or a trailing backslash")
		}
		seen[entry.Key] = true
		out.WriteString(entry.Key + "='" + strings.ReplaceAll(entry.Value, "'", "\\'") + "'\n")
	}
	return []byte(out.String()), nil
}

func composeFilesArgs(dir, project, files string, args ...string) []string {
	return append([]string{"compose", "--project-directory", dir, "--env-file", filepath.Join(files, ".env"), "-f", filepath.Join(files, "compose.yml"), "-p", project}, args...)
}

func composeArgs(dir, project string, args ...string) ([]string, error) {
	files := dir
	active, err := os.ReadFile(filepath.Join(dir, "active-revision"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		name := string(active)
		if !strings.HasPrefix(name, "revision-") || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid active Compose revision")
		}
		files = filepath.Join(dir, name)
	} else {
		// Older versions omitted .env for stacks with no variables. Supply an
		// explicit empty file so parent-directory environments cannot leak in.
		if _, statErr := os.Stat(filepath.Join(dir, ".env")); os.IsNotExist(statErr) {
			if writeErr := writeComposeFile(filepath.Join(dir, ".env"), nil); writeErr != nil && !os.IsExist(writeErr) {
				return nil, writeErr
			}
		} else if statErr != nil {
			return nil, statErr
		}
	}
	return composeFilesArgs(dir, project, files, args...), nil
}

func writeComposeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (r *Runtime) composeAction(p DeployPayload, action string) (string, error) {
	dir, project, err := composeLocation(p)
	if err != nil {
		return "", err
	}
	if err = verifyComposeOwnership(dir, project, func(args ...string) ([]byte, error) { return r.command(r.Bin, args...).Output() }); err != nil {
		return "", err
	}
	args, err := composeArgs(dir, project, action)
	if err != nil {
		return "", err
	}
	cmd := r.command(r.Bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), &recoveryError{fmt.Errorf("Compose %s did not complete: %w", action, err)}
	}
	return string(out), nil
}
