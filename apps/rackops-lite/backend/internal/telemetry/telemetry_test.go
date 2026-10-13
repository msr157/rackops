package telemetry

import (
	"math"
	"testing"
)

func TestCPUTimes(t *testing.T) {
	ct := cpuTimes{
		user:    100,
		nice:    20,
		system:  80,
		idle:    700,
		iowait:  50,
		irq:     5,
		softirq: 15,
		steal:   30,
	}

	expectedTotal := uint64(1000)
	if ct.total() != expectedTotal {
		t.Errorf("expected total %d, got %d", expectedTotal, ct.total())
	}

	expectedBusy := uint64(250) // total - idle(700) - iowait(50) = 250
	if ct.busy() != expectedBusy {
		t.Errorf("expected busy %d, got %d", expectedBusy, ct.busy())
	}
}

func TestCPUPercent(t *testing.T) {
	t1 := cpuTimes{user: 100, idle: 900} // total 1000, busy 100
	t2 := cpuTimes{user: 200, idle: 1800} // total 2000, busy 200

	pct := cpuPercent(t1, t2)
	// delta total = 1000, delta busy = 100 -> 10.0%
	if math.Abs(pct-10.0) > 0.001 {
		t.Errorf("expected 10%%, got %f", pct)
	}

	// 0 delta
	pctZero := cpuPercent(t1, t1)
	if pctZero != 0 {
		t.Errorf("expected 0%% on zero delta, got %f", pctZero)
	}
}

func TestValAfterColon(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"model name : AMD Ryzen 9 5950X", "AMD Ryzen 9 5950X"},
		{"cpu cores : 16", "16"},
		{"nocolon", ""},
		{": empty left", "empty left"},
	}

	for _, c := range cases {
		got := valAfterColon(c.input)
		if got != c.expected {
			t.Errorf("valAfterColon(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestCollectorExecution(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}

	hw := c.Hardware()
	if hw.CPUThreads <= 0 {
		t.Errorf("expected positive CPUThreads, got %d", hw.CPUThreads)
	}

	metrics := c.Metrics()
	if metrics.Timestamp.IsZero() {
		t.Error("expected valid timestamp on sampled metrics")
	}
}

func TestHostStaticInfo(t *testing.T) {
	h := Host()
	if h.OS == "" {
		t.Error("expected non-empty OS")
	}
	if h.Arch == "" {
		t.Error("expected non-empty Arch")
	}
}
