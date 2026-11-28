package agent

import (
	"strings"
	"testing"
)

func TestHostMemoryUsesAvailableMemory(t *testing.T) {
	total, available := hostMemory(strings.NewReader("MemTotal: 4096 kB\nMemFree: 512 kB\nMemAvailable: 1024 kB\n"))
	if total == nil || available == nil || *total != 4<<20 || *available != 1<<20 {
		t.Fatalf("unexpected memory sample: total=%v available=%v", total, available)
	}
	if total, available = hostMemory(strings.NewReader("MemTotal: 4096 kB\nMemFree: 512 kB\n")); total != nil || available != nil {
		t.Fatal("missing available memory became a valid sample")
	}
}

func TestHostCPUTicksExcludeGuestDoubleCount(t *testing.T) {
	total, idle, ok := cpuTicks("cpu 20 10 50 100 6 3 2 1 9 4")
	if !ok || total != 192 || idle != 106 {
		t.Fatalf("CPU ticks: total=%v idle=%v valid=%v", total, idle, ok)
	}
	if _, _, ok = cpuTicks("cpu 20 no 50 100"); ok {
		t.Fatal("invalid ticks became a valid sample")
	}
}
