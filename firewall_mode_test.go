package main

import (
	"context"
	"testing"
	"time"
)

func TestFirewallModeStopsWhenCanceled(t *testing.T) {
	t.Setenv("PANEL_MODULE_SECRET", "test-secret")
	t.Setenv("PANEL_CORE_URL", "http://127.0.0.1:1")
	t.Setenv("PANEL_NODE", "test-node")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		firewallMode(ctx, "firewall")
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("firewall mode did not stop after cancellation")
	}
}
