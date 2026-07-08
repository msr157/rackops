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

// Deploy builds (git) or pulls (image) and runs the container for a service.
func (r *Runtime) Deploy(p DeployPayload) (containerID, logOut string, err error) {
	var logs strings.Builder
	name := containerName(p)

	// Compose stacks take a separate path.
	if p.ComposeYML != "" {
		return r.deployCompose(p, &logs)
	}

	switch {
	case p.GitRepo != "":
		// Build an image from the repo, then run it.
		image, buildErr := r.buildFromGit(p, &logs)
		if buildErr != nil {
			return "", logs.String(), buildErr
		}
		p.Image = image
	case p.Image == "":
		return "", "", fmt.Errorf("service has neither an image nor a git repo")
	default:
		// Pull the image.
		logs.WriteString("$ " + r.Bin + " pull " + p.Image + "\n")
		out, pullErr := exec.Command(r.Bin, "pull", p.Image).CombinedOutput()
		logs.Write(out)
		if pullErr != nil {
			return "", logs.String(), fmt.Errorf("pull failed: %w", pullErr)
		}
	}

	// Replace any prior container for this service.
	_ = exec.Command(r.Bin, "rm", "-f", name).Run()

	args := []string{"run", "-d", "--name", name,
		"--label", "rackops.service=" + p.ServiceID,
		"--restart", "unless-stopped"}
	if p.HostNet {
		args = append(args, "--network", "host")
	} else if p.Port > 0 {
		// Publish the service's internal port on the same host port.
		args = append(args, "-p", fmt.Sprintf("%d:%d", p.Port, p.Port))
	} else {
		args = append(args, "-P") // publish exposed ports to ephemeral host ports
	}
	for _, extra := range p.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d", extra, extra))
	}
	for _, e := range p.Env {
		args = append(args, "-e", e.Key+"="+e.Value)
	}
	// Named volumes: persist template-declared container paths under
	// rackops-managed volumes so redeploys keep data.
	for _, cpath := range p.Volumes {
		volName := "rackops-" + p.ServiceID + "-" + sanitize(strings.ReplaceAll(strings.Trim(cpath, "/"), "/", "-"))
		args = append(args, "-v", volName+":"+cpath)
	}
	if p.PinnedPath != "" {
		args = append(args, "-v", strings.TrimSuffix(p.PinnedPath, "/")+"/rackops/"+sanitize(p.Name)+":/data")
	}
	if p.MemLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", p.MemLimitMB))
	}
	if p.GPU {
		args = append(args, "--gpus", "all")
	}
	args = append(args, p.Image)
	if p.Command != "" {
		args = append(args, strings.Fields(p.Command)...)
	}

	logs.WriteString("$ " + r.Bin + " " + strings.Join(args, " ") + "\n")
	out, err := exec.Command(r.Bin, args...).CombinedOutput()
	logs.Write(out)
	if err != nil && p.Port > 0 && strings.Contains(string(out), "address already in use") {
		// Host port taken — retry publishing to an ephemeral host port.
		logs.WriteString("host port busy — retrying with an ephemeral port\n")
		_ = exec.Command(r.Bin, "rm", "-f", name).Run()
		for i, a := range args {
			if a == "-p" && i+1 < len(args) && args[i+1] == fmt.Sprintf("%d:%d", p.Port, p.Port) {
				args[i+1] = fmt.Sprintf("%d", p.Port) // container port only = random host port
			}
		}
		logs.WriteString("$ " + r.Bin + " " + strings.Join(args, " ") + "\n")
		out, err = exec.Command(r.Bin, args...).CombinedOutput()
		logs.Write(out)
		if err == nil {
			// Surface the actual mapping in the log.
			if mapped, mapErr := exec.Command(r.Bin, "port", name).Output(); mapErr == nil {
				logs.WriteString("published ports:\n" + string(mapped))
			}
		}
	}
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
