package telemetry

import (
	"net"
	"os"
	"runtime"
	"strings"

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
