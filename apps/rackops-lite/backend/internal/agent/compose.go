package agent

import (
	"fmt"
	"os"

	"path/filepath"
	"strings"
)

// deployCompose writes the stack's compose file to a managed directory and
// brings it up with `<runtime> compose up -d`.
func (r *Runtime) deployCompose(p DeployPayload, logs *logSink) (string, string, error) {
	dir, project, err := composeLocation(p)
	if err != nil {
		return "", "", err
	}
	if err = verifyComposeOwnership(dir, project, func(args ...string) ([]byte, error) { return r.command(r.Bin, args...).Output() }); err != nil {
		return "", "", err
	}
	env, err := composeEnvironment(p.Env)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	revision, err := os.MkdirTemp(dir, "revision-")
	if err != nil {
		return "", "", err
	}
	if err := writeComposeFile(filepath.Join(revision, "compose.yml"), []byte(p.ComposeYML)); err != nil {
		return "", "", err
	}

	// Always replace the environment, including when every key was removed.
	if err := writeComposeFile(filepath.Join(revision, ".env"), env); err != nil {
		return "", "", err
	}
	check := r.command(r.Bin, composeFilesArgs(dir, project, revision, "config", "--quiet")...)
	check.Dir = dir
	if err := check.Run(); err != nil {
		return "", logs.String(), fmt.Errorf("Compose configuration validation failed: %w", err)
	}
	// Publish one pointer so an interrupted update never mixes old YAML with
	// new environment values. Previous revisions remain available for inspection.
	pointer, err := os.CreateTemp(dir, ".active-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(pointer.Name())
	if _, err = pointer.WriteString(filepath.Base(revision)); err == nil {
		err = pointer.Sync()
	}
	closeErr := pointer.Close()
	if err != nil {
		return "", "", err
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if err = os.Rename(pointer.Name(), filepath.Join(dir, "active-revision")); err != nil {
		return "", "", err
	}
	args := composeFilesArgs(dir, project, revision, "up", "-d", "--wait", "--wait-timeout", "60")
	logs.WriteString("$ " + r.Bin + " " + strings.Join(args, " ") + "\n")
	cmd := r.command(r.Bin, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logs, logs // stream image pulls + container creation live
	if err := cmd.Run(); err != nil {
		return "", logs.String(), &recoveryError{fmt.Errorf("compose up or health validation failed: %w", err)}
	}

	// Label the stack's containers so List() picks them up. compose doesn't
	// apply external labels post-hoc, so we track by project name instead:
	// report the project as the "container ID".
	logs.WriteString("stack up: " + project + "\n")
	return project, logs.String(), nil
}

// composeDown tears a stack down.
func (r *Runtime) composeDown(p DeployPayload) (string, error) {
	return r.composeAction(p, "down")
}

// composeLogs fetches aggregated logs for a stack's containers.
func (r *Runtime) composeLogs(p DeployPayload, tail int, since string) (string, error) {
	dir, project, err := composeLocation(p)
	if err != nil {
		return "", err
	}
	if err = verifyComposeOwnership(dir, project, func(args ...string) ([]byte, error) { return r.command(r.Bin, args...).Output() }); err != nil {
		return "", err
	}
	args, err := composeArgs(dir, project, "logs", "--tail", fmt.Sprintf("%d", tail))
	if err != nil {
		return "", err
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	cmd := r.command(r.Bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
