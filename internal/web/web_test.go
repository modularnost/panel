package web

import "testing"

func TestHostFromRule(t *testing.T) {
	rule := "Host(" + backtick + "app.example.com" + backtick + ")"
	if got := hostFromRule(rule); got != "app.example.com" {
		t.Fatalf("got %q", got)
	}
	if got := hostFromRule(""); got != "" {
		t.Fatalf("empty rule: %q", got)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/stacks?x=1":     "/stacks?x=1",
		"":                "/",
		"//evil.com":      "/",
		"/\\evil.com":     "/",
		"https://evil.io": "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
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
