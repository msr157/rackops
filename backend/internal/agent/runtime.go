package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

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

// List returns rackops-labeled containers (services and quick-tunnel
// helpers) with live stats. Reporting both lets the control plane detect
// missing containers and heal them.
func (r *Runtime) List() []types.ContainerStatus {
	stats := r.stats()
	list := r.listLabeled("rackops.service", false, stats)
	return append(list, r.listLabeled("rackops.tunnel", true, stats)...)
}

// listLabeled collects containers carrying one rackops label.
func (r *Runtime) listLabeled(label string, tunnel bool, stats map[string]containerStat) []types.ContainerStatus {
	out, err := exec.Command(r.Bin, "ps", "-a",
		"--filter", "label="+label,
		"--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.State}}|{{.Labels}}").Output()
	if err != nil {
		return nil
	}
	var list []types.ContainerStatus
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) < 5 {
			continue
		}
		cs := types.ContainerStatus{ID: f[0], Name: f[1], Image: f[2], State: strings.ToLower(f[3]), Tunnel: tunnel}
		for _, lbl := range strings.Split(f[4], ",") {
			if kv := strings.SplitN(lbl, "=", 2); len(kv) == 2 && kv[0] == label {
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

	restart := p.RestartPol
	if restart == "" {
		restart = "unless-stopped"
	}
	args := []string{"run", "-d", "--name", name,
		"--label", "rackops.service=" + p.ServiceID,
		"--restart", restart}
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
	// User-managed mounts: bind mounts and drive-placed volumes.
	for _, m := range p.Mounts {
		spec, err := mountSpec(p, m)
		if err != nil {
			return "", logs.String(), err
		}
		args = append(args, "-v", spec)
	}
	if p.PinnedPath != "" {
		args = append(args, "-v", strings.TrimSuffix(p.PinnedPath, "/")+"/rackops/"+sanitize(p.Name)+":/data")
	}
	if p.MemLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", p.MemLimitMB))
	}
	if p.CPULimit > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", p.CPULimit))
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
	// Capture the first seconds of runtime output — an instant crash (bad
	// command, port bind, missing env) shows up right here in the deploy log
	// instead of requiring a separate trip to the Logs tab.
	time.Sleep(2 * time.Second)
	if first, err := exec.Command(r.Bin, "logs", "--tail", "40", name).CombinedOutput(); err == nil && len(strings.TrimSpace(string(first))) > 0 {
		logs.WriteString("--- first container output ---\n")
		logs.Write(first)
		if !strings.HasSuffix(string(first), "\n") {
			logs.WriteString("\n")
		}
	}
	if state, err := exec.Command(r.Bin, "inspect", "-f", "{{.State.Status}}", name).Output(); err == nil {
		st := strings.TrimSpace(string(state))
		logs.WriteString("container state after start: " + st + "\n")
		if st != "running" {
			return cid, logs.String(), fmt.Errorf("container exited immediately (state %s) — see output above", st)
		}
	}
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

// LogsSince fetches logs after an RFC3339 timestamp (empty = all recent).
func (r *Runtime) LogsSince(container string, tail int, since string) (string, error) {
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, container)
	out, err := exec.Command(r.Bin, args...).CombinedOutput()
	return string(out), err
}

// mountSpec renders one -v argument for a user mount. Volume mounts with a
// chosen drive become bind mounts of a rackops-managed directory on that
// drive (created on first use), so the data physically lives where the user
// picked.
func mountSpec(p DeployPayload, m types.Mount) (string, error) {
	if m.Target == "" || !strings.HasPrefix(m.Target, "/") {
		return "", fmt.Errorf("mount target must be an absolute container path (got %q)", m.Target)
	}
	suffix := ""
	if m.ReadOnly {
		suffix = ":ro"
	}
	switch m.Type {
	case types.MountBind:
		if !strings.HasPrefix(m.Source, "/") {
			return "", fmt.Errorf("bind mount source must be an absolute host path (got %q)", m.Source)
		}
		return m.Source + ":" + m.Target + suffix, nil
	case types.MountVolume:
		volName := sanitize(m.Source)
		if volName == "" {
			volName = sanitize(strings.ReplaceAll(strings.Trim(m.Target, "/"), "/", "-"))
		}
		if m.DrivePath != "" {
			// Place the volume directory on the chosen drive.
			dir := strings.TrimSuffix(m.DrivePath, "/") + "/rackops-volumes/" + p.ServiceID + "/" + volName
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("create volume dir %s: %w", dir, err)
			}
			return dir + ":" + m.Target + suffix, nil
		}
		return "rackops-" + p.ServiceID + "-" + volName + ":" + m.Target + suffix, nil
	default:
		return "", fmt.Errorf("unknown mount type %q", m.Type)
	}
}

// StartQuickTunnel launches a cloudflared quick-tunnel container for a
// service port and returns the assigned trycloudflare.com URL. Host
// networking lets cloudflared reach the service at 127.0.0.1:<port>.
// Quick tunnels need no Cloudflare account; the URL changes on restart.
func (r *Runtime) StartQuickTunnel(serviceID string, port int) (string, error) {
	if port <= 0 {
		return "", fmt.Errorf("service has no port to tunnel")
	}
	name := quickTunnelName(serviceID)
	_ = exec.Command(r.Bin, "rm", "-f", name).Run()

	out, err := exec.Command(r.Bin, "run", "-d", "--name", name,
		"--label", "rackops.tunnel="+serviceID,
		"--network", "host",
		"--restart", "unless-stopped",
		"cloudflare/cloudflared:latest",
		"tunnel", "--no-autoupdate", "--url", fmt.Sprintf("http://127.0.0.1:%d", port),
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cloudflared start failed: %s", strings.TrimSpace(string(out)))
	}

	// cloudflared prints the assigned URL within a few seconds; first run may
	// also pull the image, so poll generously.
	re := regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		logs, _ := exec.Command(r.Bin, "logs", name).CombinedOutput()
		if m := re.Find(logs); m != nil {
			return string(m), nil
		}
		// Bail early if the container already died (bad image/arch, no net).
		state, _ := exec.Command(r.Bin, "inspect", "-f", "{{.State.Running}}", name).Output()
		if strings.TrimSpace(string(state)) == "false" {
			tail := logs
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			_ = exec.Command(r.Bin, "rm", "-f", name).Run()
			return "", fmt.Errorf("cloudflared exited: %s", strings.TrimSpace(string(tail)))
		}
		time.Sleep(2 * time.Second)
	}
	_ = exec.Command(r.Bin, "rm", "-f", name).Run()
	return "", fmt.Errorf("timed out waiting for the trycloudflare URL (network blocking outbound?)")
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
