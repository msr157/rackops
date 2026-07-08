// Package telemetry discovers hardware and samples live utilization on Linux
// by reading /proc and /sys directly — no external dependencies, no root
// required for the core metrics.
package telemetry

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Collector samples hardware state. It keeps previous counters so rates
// (CPU %, disk MB/s, NIC MB/s) can be derived between calls.
type Collector struct {
	prevCPU     cpuTimes
	prevDiskIO  map[string]diskIO
	prevNetIO   map[string]netIO
	prevSample  time.Time
	initialized bool
}

// NewCollector returns a Collector primed with an initial counter sample.
func NewCollector() *Collector {
	c := &Collector{
		prevDiskIO: map[string]diskIO{},
		prevNetIO:  map[string]netIO{},
	}
	// Prime counters so the first real Sample() yields sane rates.
	c.prevCPU, _ = readCPUTimes()
	c.sampleDiskCounters()
	c.sampleNetCounters()
	c.prevSample = time.Now()
	c.initialized = true
	return c
}

// Hardware performs full hardware discovery (called at startup and periodically).
func (c *Collector) Hardware() types.Hardware {
	hw := types.Hardware{
		CPUModel:   cpuModel(),
		CPUCores:   physicalCores(),
		CPUThreads: runtime.NumCPU(),
		MemTotalMB: memTotalMB(),
		GPUs:       discoverGPUs(),
		Drives:     c.discoverDrives(),
		NICs:       c.discoverNICs(),
	}
	return hw
}

// Metrics samples live utilization.
func (c *Collector) Metrics() types.Metrics {
	now := time.Now()
	m := types.Metrics{Timestamp: now}

	// CPU %
	cur, err := readCPUTimes()
	if err == nil {
		m.CPUPct = cpuPercent(c.prevCPU, cur)
		c.prevCPU = cur
	}

	// Load averages
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 3 {
			m.Load1, _ = strconv.ParseFloat(f[0], 64)
			m.Load5, _ = strconv.ParseFloat(f[1], 64)
			m.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}

	// Memory
	mi := readMeminfo()
	total := mi["MemTotal"]
	avail := mi["MemAvailable"]
	if total > 0 {
		usedKB := total - avail
		m.MemUsedMB = usedKB / 1024
		m.MemPct = float64(usedKB) / float64(total) * 100
	}
	m.SwapUsedMB = (mi["SwapTotal"] - mi["SwapFree"]) / 1024

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(data))
		if len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			m.UptimeSec = int64(up)
		}
	}

	// Hottest package temperature
	m.TempC = hottestTemp()

	// Process count
	m.ProcessCnt = countProcesses()

	c.prevSample = now
	return m
}

// ---- CPU ----

type cpuTimes struct{ user, nice, system, idle, iowait, irq, softirq, steal uint64 }

func (t cpuTimes) total() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}
func (t cpuTimes) busy() uint64 { return t.total() - t.idle - t.iowait }

func readCPUTimes() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)[1:]
			var v [8]uint64
			for i := 0; i < len(fields) && i < 8; i++ {
				v[i], _ = strconv.ParseUint(fields[i], 10, 64)
			}
			return cpuTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]}, nil
		}
	}
	return cpuTimes{}, fmt.Errorf("cpu line not found in /proc/stat")
}

func cpuPercent(prev, cur cpuTimes) float64 {
	dTotal := cur.total() - prev.total()
	if dTotal == 0 {
		return 0
	}
	return float64(cur.busy()-prev.busy()) / float64(dTotal) * 100
}

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "model name") {
			parts := strings.SplitN(sc.Text(), ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func physicalCores() int {
	// Count unique (physical id, core id) pairs; fall back to NumCPU.
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return runtime.NumCPU()
	}
	defer f.Close()
	cores := map[string]bool{}
	var phys, core string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "physical id"):
			phys = valAfterColon(line)
		case strings.HasPrefix(line, "core id"):
			core = valAfterColon(line)
			cores[phys+":"+core] = true
		}
	}
	if len(cores) > 0 {
		return len(cores)
	}
	return runtime.NumCPU()
}

func valAfterColon(s string) string {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// ---- Memory ----

func readMeminfo() map[string]int64 {
	out := map[string]int64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "kB"))
		v, _ := strconv.ParseInt(strings.TrimSpace(valStr), 10, 64)
		out[key] = v // kB
	}
	return out
}

func memTotalMB() int64 { return readMeminfo()["MemTotal"] / 1024 }

// ---- Thermals ----

// hottestTemp scans /sys/class/hwmon for the hottest CPU/package sensor.
func hottestTemp() float64 {
	best := 0.0
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		dir := filepath.Join("/sys/class/hwmon", e.Name())
		name := readTrim(filepath.Join(dir, "name"))
		// Prefer CPU-ish sensors but accept any as fallback.
		isCPU := name == "k10temp" || name == "coretemp" || name == "zenpower" || name == "cpu_thermal"
		temps, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, t := range temps {
			raw := readTrim(t)
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				continue
			}
			c := v / 1000.0
			if c <= 0 || c > 150 {
				continue
			}
			if isCPU && c > best {
				best = c
			} else if best == 0 && c > best {
				best = c
			}
		}
	}
	return best
}

// SensorReading is a labeled temperature from any hwmon chip.
type SensorReading struct {
	Chip  string  `json:"chip"`
	Label string  `json:"label"`
	TempC float64 `json:"tempC"`
}

// AllTemps returns every valid temperature sensor on the system.
func AllTemps() []SensorReading {
	var out []SensorReading
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return out
	}
	for _, e := range entries {
		dir := filepath.Join("/sys/class/hwmon", e.Name())
		chip := readTrim(filepath.Join(dir, "name"))
		temps, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, t := range temps {
			v, err := strconv.ParseFloat(readTrim(t), 64)
			if err != nil {
				continue
			}
			c := v / 1000.0
			if c <= 0 || c > 150 {
				continue
			}
			label := readTrim(strings.TrimSuffix(t, "_input") + "_label")
			if label == "" {
				label = filepath.Base(t)
			}
			out = append(out, SensorReading{Chip: chip, Label: label, TempC: c})
		}
	}
	return out
}

// ---- Processes ----

func countProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				n++
			}
		}
	}
	return n
}

// ---- helpers ----

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
