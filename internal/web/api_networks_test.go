package web

import (
	"errors"
	"testing"

	"modularnost/internal/docker"
)

func TestValidateNetworkChanges(t *testing.T) {
	state := docker.ServiceNetworkState{Stack: "app", Attachments: []docker.NetworkAttachment{{ID: "active"}, {ID: "protected"}}}
	networks := []docker.Network{
		{ID: "active", Scope: "swarm", Stack: "app"},
		{ID: "protected", Scope: "swarm", Protected: true},
		{ID: "foreign", Scope: "swarm", Stack: "other"},
		{ID: "available", Scope: "swarm"},
		{ID: "local", Scope: "local"},
	}
	for _, tt := range []struct {
		name    string
		changes networkChanges
		valid   bool
	}{
		{"move", networkChanges{Add: []string{"available"}, Remove: []string{"active"}}, true},
		{"protected", networkChanges{Remove: []string{"protected"}}, false},
		{"other stack", networkChanges{Add: []string{"foreign"}}, false},
		{"already attached", networkChanges{Add: []string{"active"}}, false},
		{"not attached", networkChanges{Remove: []string{"available"}}, false},
		{"duplicate", networkChanges{Add: []string{"available", "available"}}, false},
		{"local", networkChanges{Add: []string{"local"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			add, remove, err := validateNetworkChanges(state, networks, tt.changes)
			if tt.valid && (err != nil || len(add) != 1 || len(remove) != 1) {
				t.Fatalf("add=%v remove=%v err=%v", add, remove, err)
			}
			if !tt.valid && !errors.Is(err, docker.ErrInvalidNetwork) {
				t.Fatalf("expected invalid network, got %v", err)
			}
		})
	}
}
