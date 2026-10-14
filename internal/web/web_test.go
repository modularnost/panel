package web

import (
	"testing"

	"modularnost/internal/docker"
)

func TestHostFromRule(t *testing.T) {
	rule := "Host(" + backtick + "app.example.com" + backtick + ")"
	if got := hostFromRule(rule); got != "app.example.com" {
		t.Fatalf("got %q", got)
	}
	if got := hostFromRule(""); got != "" {
		t.Fatalf("empty rule: %q", got)
	}
}

// Routes panics on a pattern conflict, and that happens at startup — which is
// far too late to find out. One call catches it here instead.
func TestRoutesRegister(t *testing.T) {
	(&Server{}).Routes()
}

func TestTraefikLabels(t *testing.T) {
	l := traefikLabels("web", "app.example.com", "8080", "websecure", "le")
	for k, want := range map[string]string{
		"traefik.enable":                                     "true",
		"traefik.http.routers.web.rule":                      "Host(" + backtick + "app.example.com" + backtick + ")",
		"traefik.http.routers.web.tls":                       "true",
		"traefik.http.routers.web.tls.certresolver":          "le",
		"traefik.http.services.web.loadbalancer.server.port": "8080",
	} {
		if l[k] != want {
			t.Errorf("%s = %q, want %q", k, l[k], want)
		}
	}

	plain := traefikLabels("web", "app.example.com", "80", "web", "")
	if plain["traefik.http.routers.web.tls"] != "" {
		t.Error("no resolver must leave TLS off")
	}

	// Empty value means "delete the label", so nothing set takes the service off Traefik.
	if off := traefikLabels("web", "", "", "websecure", ""); off["traefik.enable"] != "" {
		t.Errorf("host and port both empty must disable Traefik: %q", off["traefik.enable"])
	}
}

func TestValidateTraefik(t *testing.T) {
	ok := [][4]string{
		{"app.example.com", "8080", "websecure", "le"},
		{"", "", "", ""},
		{"a-b.example.com", "65535", "web_2", ""},
	}
	for _, c := range ok {
		if err := validateTraefik(c[0], c[1], c[2], c[3]); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	bad := [][4]string{
		{"a" + backtick + ") || Host(" + backtick + "b", "80", "web", ""},
		{"a b.com", "80", "web", ""},
		{"app.example.com", "0", "web", ""},
		{"app.example.com", "65536", "web", ""},
		{"app.example.com", "http", "web", ""},
		{"app.example.com", "80", "web,admin", ""},
		{"app.example.com", "80", "web", "le/../x"},
	}
	for _, c := range bad {
		if err := validateTraefik(c[0], c[1], c[2], c[3]); err == nil {
			t.Errorf("%v must be refused", c)
		}
	}
}

func TestRefuseDrainKeepsAManagerThatCanRunThePanel(t *testing.T) {
	m := func(id, availability, status string) docker.Node {
		return docker.Node{ID: id, Role: "manager", Availability: availability, Status: status}
	}
	w := docker.Node{ID: "w1", Role: "worker", Availability: "active", Status: "ready"}

	for name, c := range map[string]struct {
		nodes       []docker.Node
		id, to      string
		wantRefusal bool
	}{
		"the only manager":                   {[]docker.Node{m("m1", "active", "ready"), w}, "m1", "drain", true},
		"pausing the only manager":           {[]docker.Node{m("m1", "active", "ready")}, "m1", "pause", true},
		"another manager is down":            {[]docker.Node{m("m1", "active", "ready"), m("m2", "active", "down")}, "m1", "drain", true},
		"another manager is already drained": {[]docker.Node{m("m1", "active", "ready"), m("m2", "drain", "ready")}, "m1", "drain", true},
		"a second ready manager exists":      {[]docker.Node{m("m1", "active", "ready"), m("m2", "active", "ready")}, "m1", "drain", false},
		"a worker":                           {[]docker.Node{m("m1", "active", "ready"), w}, "w1", "drain", false},
		"bringing a node back":               {[]docker.Node{m("m1", "drain", "ready")}, "m1", "active", false},
		"a manager that is already drained":  {[]docker.Node{m("m1", "drain", "ready")}, "m1", "pause", false},
	} {
		err := refuseDrain(c.nodes, c.id, c.to)
		if (err != nil) != c.wantRefusal {
			t.Errorf("%s: refused=%v, want %v (%v)", name, err != nil, c.wantRefusal, err)
		}
	}
}
