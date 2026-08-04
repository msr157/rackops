package telemetry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rackops/rackops/internal/types"
)

// discoverGPUs finds NVIDIA GPUs via nvidia-smi and AMD/Intel GPUs via sysfs.
func discoverGPUs() []types.GPU {
	var gpus []types.GPU
	gpus = append(gpus, nvidiaGPUs()...)
	gpus = append(gpus, drmGPUs(len(gpus))...)
	return gpus
}

// nvidiaGPUs queries nvidia-smi if present.
func nvidiaGPUs() []types.GPU {
	var gpus []types.GPU
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return gpus
	}
	out, err := exec.Command(path,
		"--query-gpu=index,name,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return gpus
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(f[0]))
		vt, _ := strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		vu, _ := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64)
		util, _ := strconv.ParseFloat(strings.TrimSpace(f[4]), 64)
		temp, _ := strconv.ParseFloat(strings.TrimSpace(f[5]), 64)
		pw, _ := strconv.ParseFloat(strings.TrimSpace(f[6]), 64)
		gpus = append(gpus, types.GPU{
			Index: idx, Vendor: "nvidia", Model: strings.TrimSpace(f[1]),
			VRAMTotal: vt, VRAMUsed: vu, UtilPct: util, TempC: temp, PowerW: pw,
		})
	}
	return gpus
}

// drmGPUs discovers AMD/Intel GPUs through /sys/class/drm.
func drmGPUs(startIdx int) []types.GPU {
	var gpus []types.GPU
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]")
	idx := startIdx
	for _, card := range cards {
		dev := filepath.Join(card, "device")
		vendorID := readTrim(filepath.Join(dev, "vendor"))
		var vendor string
		switch vendorID {
		case "0x1002":
			vendor = "amd"
		case "0x8086":
			vendor = "intel"
		case "0x10de":
			continue // NVIDIA handled by nvidia-smi
		default:
			continue
		}
		g := types.GPU{Index: idx, Vendor: vendor, Model: gpuModelName(dev, vendorID)}

		// AMD exposes VRAM + busy% + hwmon directly.
		if vendor == "amd" {
			if v, err := strconv.ParseInt(readTrim(filepath.Join(dev, "mem_info_vram_total")), 10, 64); err == nil {
				g.VRAMTotal = v / 1024 / 1024
			}
			if v, err := strconv.ParseInt(readTrim(filepath.Join(dev, "mem_info_vram_used")), 10, 64); err == nil {
				g.VRAMUsed = v / 1024 / 1024
			}
			if v, err := strconv.ParseFloat(readTrim(filepath.Join(dev, "gpu_busy_percent")), 64); err == nil {
				g.UtilPct = v
			}
			// hwmon: temp + power
			hws, _ := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*"))
			for _, hw := range hws {
				if v, err := strconv.ParseFloat(readTrim(filepath.Join(hw, "temp1_input")), 64); err == nil {
					g.TempC = v / 1000
				}
				// power1_average (older) or power1_input (newer), microwatts
				for _, pf := range []string{"power1_average", "power1_input"} {
					if v, err := strconv.ParseFloat(readTrim(filepath.Join(hw, pf)), 64); err == nil && v > 0 {
						g.PowerW = round1(v / 1e6)
						break
					}
				}
			}
		}
		gpus = append(gpus, g)
		idx++
	}
	return gpus
}

// gpuModelName tries several sources for a human-readable GPU name.
func gpuModelName(dev, vendorID string) string {
	// AMD: product name sometimes in market_name via amdgpu
	for _, f := range []string{"product_name", "market_name"} {
		if s := readTrim(filepath.Join(dev, f)); s != "" {
			return s
		}
	}
	// Fallback: lspci description for this PCI address.
	addr := filepath.Base(resolveLink(dev))
	if addr != "" {
		if out, err := exec.Command("lspci", "-s", addr).Output(); err == nil {
			line := strings.TrimSpace(string(out))
			if i := strings.Index(line, ": "); i > 0 {
				return line[i+2:]
			}
		}
	}
	deviceID := readTrim(filepath.Join(dev, "device"))
	name := map[string]string{"0x1002": "AMD GPU", "0x8086": "Intel GPU"}[vendorID]
	if deviceID != "" {
		return name + " (" + deviceID + ")"
	}
	return name
}

func resolveLink(p string) string {
	r, err := os.Readlink(p)
	if err != nil {
		return p
	}
	return r
}
