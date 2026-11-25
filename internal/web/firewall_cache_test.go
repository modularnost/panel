package web

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modularnost/internal/firewall"
)

func TestFirewallSpecCacheCoalescesAndInvalidates(t *testing.T) {
	var c firewallSpecCache
	var calls atomic.Int32
	load := func(ctx context.Context) (firewall.Spec, time.Time, error) {
		n := calls.Add(1)
		return firewall.Spec{Peers: []string{strconv.Itoa(int(n))}}, time.Now().Add(firewallSpecTTL), nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			spec, err := c.get(t.Context(), load)
			if err != nil || len(spec.Peers) != 1 || spec.Peers[0] != "1" {
				t.Errorf("spec = %+v, %v", spec, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("loaded %d times", calls.Load())
	}
	c.invalidate()
	if spec, err := c.get(t.Context(), load); err != nil || len(spec.Peers) != 1 || spec.Peers[0] != "2" {
		t.Fatalf("spec after invalidation = %+v, %v", spec, err)
	}
}
