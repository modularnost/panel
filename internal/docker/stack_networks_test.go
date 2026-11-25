package docker

import (
	"strings"
	"testing"
)

func TestEditStackNetworks(t *testing.T) {
	shared := Network{Name: "shared"}
	tests := []struct {
		name, compose    string
		add, remove      []Network
		contains, absent []string
		fail             bool
	}{
		{name: "implicit default", compose: "version: '3.8'\nservices:\n  web:\n    image: nginx\n", add: []Network{shared}, contains: []string{"- default", "- shared", "external: true", "name: shared"}},
		{name: "mapping preserves aliases and another service", compose: "services:\n  web:\n    image: nginx\n    networks:\n      app:\n        aliases: [web]\n      shared: null\n  worker:\n    image: nginx\n    networks: [shared]\nnetworks:\n  app: {}\n  shared: {external: true}\n", remove: []Network{shared}, contains: []string{"aliases: [web]", "networks: [shared]", "external: true"}, absent: []string{"shared: null"}},
		{name: "existing external name", compose: "services:\n  web:\n    image: nginx\n    networks: [app]\nnetworks:\n  app: {}\n  external_app: {external: true, name: shared}\n", add: []Network{shared}, contains: []string{"[app, external_app]"}, absent: []string{"shared_external"}},
		{name: "shared YAML anchor", compose: "services:\n  web:\n    image: nginx\n    networks: &shared [default]\n  worker:\n    image: nginx\n    networks: *shared\n", add: []Network{shared}, fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EditStackNetworks(tt.compose, "demo", "web", tt.add, tt.remove)
			if (err != nil) != tt.fail {
				t.Fatalf("compose=%q error=%v", got, err)
			}
			for _, wanted := range tt.contains {
				if !strings.Contains(got, wanted) {
					t.Errorf("missing %q in %s", wanted, got)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(got, unwanted) {
					t.Errorf("unexpected %q in %s", unwanted, got)
				}
			}
		})
	}
}
