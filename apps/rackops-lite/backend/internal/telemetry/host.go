package telemetry

import (
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Host gathers static host identity information.
func Host() types.HostInfo {
	h := types.HostInfo{
		OS:   osRelease(),
		Arch: runtime.GOARCH,
	}
	h.Hostname, _ = os.Hostname()
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.Kernel = strings.TrimSpace(string(data))
	}
	h.IP = outboundIP()
	return h
}

// osRelease returns a pretty OS name from /etc/os-release.
func osRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

// outboundIP finds the primary local IP by opening a UDP socket (no traffic sent).
func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// PublicIP discovers this host's internet-visible IPv4 by asking well-known
// reflector services (tried in order, short timeout). Returns "" when offline
// or unreachable. Callers should cache — this makes an outbound HTTP request.
func PublicIP() string {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, url := range []string{
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
		"https://icanhazip.com",
	} {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
			return s
		}
	}
	return ""
}
