package main

import "testing"

func TestHumanAndBars(t *testing.T) {
	for _, c := range []struct {
		in   uint64
		want string
	}{{512, "512 B"}, {1024, "1.0 KiB"}, {1536, "1.5 KiB"}, {1 << 30, "1.0 GiB"}} {
		if got := human(c.in); got != c.want {
			t.Fatalf("human(%d) = %q, want %q", c.in, got, c.want)
		}
	}
	// The bar never exceeds 100 or goes negative.
	if got := (stat{CPU: 250}).CPUBar(); got != 100 {
		t.Fatalf("CPUBar = %d", got)
	}
	s := stat{Mem: 512, MemLimit: 1024}
	if got := s.MemBar(); got != 50 {
		t.Fatalf("MemBar = %d", got)
	}
	// With no memory limit there is no bar, not a division by zero.
	if got := (stat{Mem: 512}).MemBar(); got != 0 {
		t.Fatalf("MemBar with no limit = %d", got)
	}
}
