package agent

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rackops/rackops/internal/types"
)

// Runtime wraps a Docker-compatible container CLI (docker or podman).
type Runtime struct {
	Bin string
}

// DetectRuntime finds docker or podman on PATH (docker preferred).
func DetectRuntime() (*Runtime, error) {
	for _, bin := range []string{"docker", "podman"} {
		if path, err := exec.LookPath(bin); err == nil {
			// Verify the daemon/CLI actually responds.
			if err := exec.Command(path, "version", "--format", "{{.Client.Version}}").Run(); err == nil {
				return &Runtime{Bin: path}, nil
			}
			// podman works daemonless even if the check format differs
			if bin == "podman" {
				if err := exec.Command(path, "--version").Run(); err == nil {
					return &Runtime{Bin: path}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("neither docker nor podman available")
}

// List returns rackops-labeled containers with live stats.
func (r *Runtime) List() []types.ContainerStatus {
	out, err := exec.Command(r.Bin, "ps", "-a",
		"--filter", "label=rackops.service",
		"--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.State}}|{{.Labels}}").Output()
	if err != nil {
		return nil
	}
	var list []types.ContainerStatus
	stats := r.stats()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) < 5 {
			continue
		}
		cs := types.ContainerStatus{ID: f[0], Name: f[1], Image: f[2], State: strings.ToLower(f[3])}
		// Extract rackops.service label value.
		for _, lbl := range strings.Split(f[4], ",") {
			if kv := strings.SplitN(lbl, "=", 2); len(kv) == 2 && kv[0] == "rackops.service" {
				cs.ServiceID = kv[1]
			}
		}
		if st, ok := stats[cs.ID]; ok {
			cs.CPUPct = st.cpu
			cs.MemUsedMB = st.memMB
		}
		list = append(list, cs)
	}
	return list
}

type containerStat struct {
	cpu   float64
	memMB int64
}

// stats samples per-container CPU/memory in one shot.
func (r *Runtime) stats() map[string]containerStat {
	out, err := exec.Command(r.Bin, "stats", "--no-stream",
		"--format", "{{.ID}}|{{.CPUPerc}}|{{.MemUsage}}").Output()
	if err != nil {
		return nil
	}
	m := map[string]containerStat{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "|")
		if len(f) < 3 {
			continue
		}
		var st containerStat
		st.cpu, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(f[1]), "%"), 64)
		// MemUsage looks like "24.5MiB / 15.2GiB"
		mem := strings.Fields(f[2])
		if len(mem) > 0 {
			st.memMB = parseMemMB(mem[0])
		}
		// Truncate ID to ps's short form for matching.
		id := f[0]
		if len(id) > 12 {
			id = id[:12]
		}
		m[id] = st
	}
	return m
}

func parseMemMB(s string) int64 {
	for suffix, mult := range map[string]float64{"GiB": 1024, "MiB": 1, "KiB": 1.0 / 1024, "MB": 0.954, "GB": 954, "kB": 0.001, "B": 0} {
		if strings.HasSuffix(s, suffix) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(s, suffix), 64)
			if err != nil {
				return 0
			}
			return int64(v * mult)
		}
	}
	return 0
}

// Deploy pulls the image and runs the container for a service.
// Git/compose deploys are handled by later milestones' build pipeline.
func (r *Runtime) Deploy(p DeployPayload) (containerID, logOut string, err error) {
	var logs strings.Builder
	name := containerName(p)

	if p.Image == "" {
		return "", "", fmt.Errorf("git and compose deploys land in milestone 3 — provide an image for now")
	}

	// Pull.
	logs.WriteString("$ " + r.Bin + " pull " + p.Image + "\n")
	out, err := exec.Command(r.Bin, "pull", p.Image).CombinedOutput()
	logs.Write(out)
	if err != nil {
		return "", logs.String(), fmt.Errorf("pull failed: %w", err)
	}

	// Replace any prior container for this service.
	_ = exec.Command(r.Bin, "rm", "-f", name).Run()

	args := []string{"run", "-d", "--name", name,
		"--label", "rackops.service=" + p.ServiceID,
		"--restart", "unless-stopped"}
	if p.Port > 0 {
		// Publish the service's internal port on the same host port.
		args = append(args, "-p", fmt.Sprintf("%d:%d", p.Port, p.Port))
	} else {
		args = append(args, "-P") // publish exposed ports to ephemeral host ports
	}
	for _, e := range p.Env {
		args = append(args, "-e", e.Key+"="+e.Value)
	}
	for _, v := range p.Volumes {
		args = append(args, "-v", v)
	}
	if p.PinnedPath != "" {
		args = append(args, "-v", strings.TrimSuffix(p.PinnedPath, "/")+"/rackops/"+sanitize(p.Name)+":/data")
	}
	if p.MemLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", p.MemLimitMB))
	}
	args = append(args, p.Image)

	logs.WriteString("$ " + r.Bin + " " + strings.Join(args, " ") + "\n")
	out, err = exec.Command(r.Bin, args...).CombinedOutput()
	logs.Write(out)
	if err != nil {
		return "", logs.String(), fmt.Errorf("run failed: %w", err)
	}
	// The container ID is the last non-empty output line (podman-docker's
	// "Emulate Docker CLI" banner and pull progress may precede it).
	cid := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 12 && isHex(line) {
			cid = line
		}
	}
	if len(cid) > 12 {
		cid = cid[:12]
	}
	logs.WriteString("container started: " + cid + "\n")
	return cid, logs.String(), nil
}

// Simple runs a one-shot container command (stop/start/restart/rm).
func (r *Runtime) Simple(args ...string) (string, error) {
	out, err := exec.Command(r.Bin, args...).CombinedOutput()
	return string(out), err
}

// Logs fetches recent logs for a container.
func (r *Runtime) Logs(container string, tail int) (string, error) {
	out, err := exec.Command(r.Bin, "logs", "--tail", strconv.Itoa(tail), container).CombinedOutput()
	return string(out), err
}

// isHex reports whether s consists solely of hex digits.
func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return len(s) > 0
}

// MarshalJSON keeps Runtime opaque in reports.
func (r *Runtime) MarshalJSON() ([]byte, error) { return json.Marshal(r.Bin) }
