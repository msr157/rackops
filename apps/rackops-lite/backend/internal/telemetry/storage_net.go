package telemetry

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// ---- Block devices ----

type diskIO struct {
	sectorsRead, sectorsWritten uint64
	at                          time.Time
}

// discoverDrives enumerates physical block devices from /sys/block, joins
// mount + usage info from /proc/mounts + statfs, and derives live throughput
// from /sys/block/<dev>/stat deltas.
func (c *Collector) discoverDrives() []types.Drive {
	var drives []types.Drive
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return drives
	}
	mounts := deviceMounts()
	for _, e := range entries {
		name := e.Name()
		// Skip virtual devices.
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "dm-") ||
			strings.HasPrefix(name, "sr") {
			continue
		}
		dir := filepath.Join("/sys/block", name)
		sizeSectors, _ := strconv.ParseInt(readTrim(filepath.Join(dir, "size")), 10, 64)
		sizeGB := float64(sizeSectors) * 512 / 1e9
		if sizeGB < 0.1 {
			continue
		}

		d := types.Drive{
			Device: "/dev/" + name,
			Model:  readTrim(filepath.Join(dir, "device", "model")),
			SizeGB: round1(sizeGB),
			Health: "unknown",
		}

		// Type: nvme by name; rotational flag for ssd/hdd.
		switch {
		case strings.HasPrefix(name, "nvme"):
			d.Type = "nvme"
		case readTrim(filepath.Join(dir, "queue", "rotational")) == "1":
			d.Type = "hdd"
		default:
			d.Type = "ssd"
		}

		// Throughput from /sys/block/<dev>/stat (sectors read/written).
		if rd, wr, ok := readDiskStat(dir); ok {
			now := time.Now()
			if prev, seen := c.prevDiskIO[name]; seen {
				dt := now.Sub(prev.at).Seconds()
				if dt > 0 {
					d.ReadMBps = round1(float64(rd-prev.sectorsRead) * 512 / 1e6 / dt)
					d.WriteMBps = round1(float64(wr-prev.sectorsWritten) * 512 / 1e6 / dt)
				}
			}
			c.prevDiskIO[name] = diskIO{rd, wr, now}
		}

		// NVMe temperature via hwmon under the device.
		if d.Type == "nvme" {
			d.TempC = nvmeTemp(dir)
		}

		// Mount + usage: prefer the largest mounted partition of this device.
		var bestMount string
		var bestSize float64
		for dev, mp := range mounts {
			if strings.HasPrefix(dev, "/dev/"+name) {
				total, available, err := filesystemSpace(mp)
				if err == nil {
					if total > bestSize {
						bestSize = total
						bestMount = mp
						d.UsedGB = round1(total - available)
					}
				}
			}
		}
		d.MountPoint = bestMount
		if d.UsedGB > 0 && d.SizeGB > 0 {
			switch {
			case d.UsedGB/d.SizeGB > 0.95:
				d.Health = "critical"
			case d.UsedGB/d.SizeGB > 0.85:
				d.Health = "warning"
			default:
				d.Health = "ok"
			}
		}
		drives = append(drives, d)
	}
	return drives
}

// sampleDiskCounters primes throughput counters without building Drive objects.
func (c *Collector) sampleDiskCounters() {
	entries, _ := os.ReadDir("/sys/block")
	for _, e := range entries {
		dir := filepath.Join("/sys/block", e.Name())
		if rd, wr, ok := readDiskStat(dir); ok {
			c.prevDiskIO[e.Name()] = diskIO{rd, wr, time.Now()}
		}
	}
}

// readDiskStat returns cumulative sectors read/written for a block device.
func readDiskStat(dir string) (rd, wr uint64, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(string(data))
	if len(f) < 7 {
		return 0, 0, false
	}
	rd, _ = strconv.ParseUint(f[2], 10, 64) // sectors read
	wr, _ = strconv.ParseUint(f[6], 10, 64) // sectors written
	return rd, wr, true
}

// nvmeTemp finds a temperature sensor under the NVMe device's hwmon dir.
func nvmeTemp(blockDir string) float64 {
	globs, _ := filepath.Glob(filepath.Join(blockDir, "device", "hwmon*", "temp1_input"))
	if len(globs) == 0 {
		globs, _ = filepath.Glob(filepath.Join(blockDir, "device", "device", "hwmon", "hwmon*", "temp1_input"))
	}
	for _, g := range globs {
		if v, err := strconv.ParseFloat(readTrim(g), 64); err == nil {
			return v / 1000.0
		}
	}
	return 0
}

// deviceMounts maps device path -> mount point from /proc/mounts.
func deviceMounts() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		// Skip snap and boot noise; keep first (primary) mount of a device.
		if strings.HasPrefix(f[1], "/snap") || strings.HasPrefix(f[1], "/boot/efi") {
			continue
		}
		if _, seen := out[f[0]]; !seen {
			out[f[0]] = f[1]
		}
	}
	return out
}

// ---- NICs ----

type netIO struct {
	rxBytes, txBytes uint64
	at               time.Time
}

// discoverNICs lists physical + mesh interfaces with live throughput.
func (c *Collector) discoverNICs() []types.NIC {
	var nics []types.NIC
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nics
	}
	for _, e := range entries {
		name := e.Name()
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") ||
			strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "podman") {
			continue
		}
		dir := filepath.Join("/sys/class/net", name)
		n := types.NIC{
			Name: name,
			MAC:  readTrim(filepath.Join(dir, "address")),
		}
		if sp, err := strconv.ParseInt(readTrim(filepath.Join(dir, "speed")), 10, 64); err == nil {
			n.SpeedMb = sp
		} else {
			n.SpeedMb = -1
		}
		rx, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "statistics", "rx_bytes")), 10, 64)
		tx, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "statistics", "tx_bytes")), 10, 64)
		now := time.Now()
		if prev, seen := c.prevNetIO[name]; seen {
			dt := now.Sub(prev.at).Seconds()
			if dt > 0 {
				n.RxMBps = round2(float64(rx-prev.rxBytes) / 1e6 / dt)
				n.TxMBps = round2(float64(tx-prev.txBytes) / 1e6 / dt)
			}
		}
		c.prevNetIO[name] = netIO{rx, tx, now}
		nics = append(nics, n)
	}
	return nics
}

func (c *Collector) sampleNetCounters() {
	entries, _ := os.ReadDir("/sys/class/net")
	for _, e := range entries {
		dir := filepath.Join("/sys/class/net", e.Name())
		rx, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "statistics", "rx_bytes")), 10, 64)
		tx, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "statistics", "tx_bytes")), 10, 64)
		c.prevNetIO[e.Name()] = netIO{rx, tx, time.Now()}
	}
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
