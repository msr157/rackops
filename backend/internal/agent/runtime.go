package agent

import (
	"encoding/json"
	"fmt"
	"net"
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
// helpers) plus any external containers already running on the host. Reporting
// the labeled ones lets the control plane detect missing containers and heal
// them; reporting the external ones lets it adopt pre-existing workloads so
// they show up and become manageable.
func (r *Runtime) List() []types.ContainerStatus {
	stats := r.stats()
	list := r.listLabeled("rackops.service", false, stats)
	list = append(list, r.listLabeled("rackops.tunnel", true, stats)...)
	return append(list, r.listExternal(stats)...)
}

// listExternal collects containers NOT created by RackOps — anything without a
// rackops label and not named with the rackops- prefix. These are surfaced so
// the control plane can adopt them.
func (r *Runtime) listExternal(stats map[string]containerStat) []types.ContainerStatus {
	out, err := exec.Command(r.Bin, "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.State}}|{{.Ports}}|{{.Labels}}").Output()
	if err != nil {
		return nil
	}
	var list []types.ContainerStatus
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 6)
		if len(f) < 6 {
			continue
		}
		name := f[1]
		labels := f[5]
		// Skip anything RackOps owns: labeled containers (services, tunnels,
		// edge) and anything under the rackops- naming convention.
		if strings.Contains(labels, "rackops.service=") || strings.Contains(labels, "rackops.tunnel=") ||
			strings.HasPrefix(name, "rackops-") {
			continue
		}
		cs := types.ContainerStatus{
			ID: f[0], Name: name, Image: f[2], State: strings.ToLower(f[3]), External: true,
			HostPort: firstPublishedPort(f[4]),
		}
		if st, ok := stats[cs.ID]; ok {
			cs.CPUPct = st.cpu
			cs.MemUsedMB = st.memMB
		}
		list = append(list, cs)
	}
	return list
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
// hostPort is the host port the container was actually published on (may differ
// from the requested one if it was taken); 0 for compose stacks / host-network.
func (r *Runtime) Deploy(p DeployPayload) (containerID string, hostPort int, logOut string, err error) {
	var logs strings.Builder
	name := containerName(p)

	// Compose stacks take a separate path (they publish their own ports).
	if p.ComposeYML != "" {
		cid, out, e := r.deployCompose(p, &logs)
		return cid, 0, out, e
	}

	switch {
	case p.GitRepo != "":
		// Build an image from the repo, then run it.
		image, buildErr := r.buildFromGit(p, &logs)
		if buildErr != nil {
			return "", 0, logs.String(), buildErr
		}
		p.Image = image
	case p.Image == "":
		return "", 0, "", fmt.Errorf("service has neither an image nor a git repo")
	default:
		// Pull the image.
		logs.WriteString("$ " + r.Bin + " pull " + p.Image + "\n")
		out, pullErr := exec.Command(r.Bin, "pull", p.Image).CombinedOutput()
		logs.Write(out)
		if pullErr != nil {
			return "", 0, logs.String(), fmt.Errorf("pull failed: %w", pullErr)
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
	// Port publishing: -p <hostPort>:<containerPort>. The container port is what
	// the app listens on inside (fixed by the image); the host port is what it's
	// reachable on. They're only equal by coincidence — publishing host:host
	// (the old behaviour) silently broke every app with a fixed internal port.
	containerPort := p.ContainerPort
	if containerPort == 0 {
		containerPort = p.Port // image/git deploys: the given port is the container port
	}
	if p.HostNet {
		args = append(args, "--network", "host")
	} else if containerPort > 0 {
		// Claim a host port that's actually free on this machine (covers ports
		// held by non-RackOps processes, not just other services).
		hostPort = firstFreePort(p.Port, containerPort)
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, containerPort))
		if p.Port > 0 && hostPort != p.Port {
			logs.WriteString(fmt.Sprintf("host port %d busy — published on %d instead\n", p.Port, hostPort))
		}
	} else {
		args = append(args, "-P") // publish exposed ports to ephemeral host ports
	}
	// Extra ports (e.g. DNS :53): publish on the same host port when free, else
	// the next free one — so one busy secondary port (a host resolver already on
	// :53 is common) doesn't sink the whole deploy. The container side is kept.
	for _, extra := range p.Ports {
		hp := firstFreePort(extra, extra)
		args = append(args, "-p", fmt.Sprintf("%d:%d", hp, extra))
		if hp != extra {
			logs.WriteString(fmt.Sprintf("extra port %d busy on host — published on %d instead\n", extra, hp))
		}
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
			return "", 0, logs.String(), err
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
	if err != nil {
		return "", hostPort, logs.String(), fmt.Errorf("run failed: %w", err)
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
			return cid, hostPort, logs.String(), fmt.Errorf("container exited immediately (state %s) — see output above", st)
		}
	}
	return cid, hostPort, logs.String(), nil
}

// firstFreePort returns prefer if it can be bound on this host, else the next
// bindable port; containerPort is the fallback when no host port was requested.
func firstFreePort(prefer, containerPort int) int {
	start := prefer
	if start <= 0 {
		start = containerPort
	}
	if start <= 0 {
		start = 8080
	}
	for p := start; p <= 65535 && p < start+2000; p++ {
		if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p)); err == nil {
			_ = ln.Close()
			return p
		}
	}
	return start
}

// portsRe extracts the host side of a docker "Ports" column entry, e.g.
// "0.0.0.0:8080->80/tcp, :::8080->80/tcp" -> 8080.
var portsRe = regexp.MustCompile(`:(\d+)->`)

// firstPublishedPort returns the first published host port from docker's Ports
// column, or 0 if the container publishes none.
func firstPublishedPort(ports string) int {
	if m := portsRe.FindStringSubmatch(ports); m != nil {
		p, _ := strconv.Atoi(m[1])
		return p
	}
	return 0
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
