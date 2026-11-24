package modules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"modularnost/internal/agent"
	"modularnost/internal/firewall"
)

func TestModuleCredentialsAreSeparateFromSystemCredentials(t *testing.T) {
	const secret = "test-secret"
	r := NewRegistry(nil, secret)
	for _, name := range []string{"agent", "firewall", "service:agent", "service:firewall"} {
		if r.ValidToken(name, agent.Token(secret)) || r.ValidToken(name, firewall.Token(secret)) {
			t.Errorf("system credential accepted for module %q", name)
		}
	}
}

func TestTokenAndPermissions(t *testing.T) {
	r := NewRegistry(nil, "secret")
	tok := r.Token("hello")
	if !r.ValidToken("hello", tok) {
		t.Fatal("a module's own token was rejected")
	}
	// One module's token must not work for another.
	if r.ValidToken("evil", tok) {
		t.Fatal("token accepted for a different name")
	}
	if r.ValidToken("hello", "") || r.ValidToken("hello", "deadbeef") {
		t.Fatal("empty or forged token accepted")
	}
	// Different secrets must produce different tokens.
	if NewRegistry(nil, "other").Token("hello") == tok {
		t.Fatal("token does not depend on the secret")
	}

	m := Module{Manifest: Manifest{Permissions: []string{"service:read"}}}
	if !m.Allowed("service:read") || m.Allowed("service:update") {
		t.Fatal("permission check is lying")
	}
}

// A module cannot call the core until it knows its token, and discovery is the
// only moment the core reaches it before anyone opens a page.
func TestManifestFetchCarriesTheToken(t *testing.T) {
	var gotName, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotName, gotToken = r.Header.Get("X-Panel-Module"), r.Header.Get("X-Panel-Token")
		w.Write([]byte(`{"contract":3,"name":"hello","version":"0.1.0"}`))
	}))
	defer srv.Close()

	r := NewRegistry(nil, "secret")
	m := Module{Name: "hello", BaseURL: srv.URL}
	if err := r.fetchManifest(context.Background(), &m); err != nil {
		t.Fatal(err)
	}
	if gotName != "hello" || !r.ValidToken("hello", gotToken) {
		t.Fatalf("module got name %q token %q", gotName, gotToken)
	}
}

func TestRegistryStopsAtRedirects(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/action/example" {
			w.Header().Set("Location", "/other")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := NewRegistry(nil, "secret")
	m := Module{Name: "example", BaseURL: srv.URL}
	if _, err := r.Act(t.Context(), m, "/action/example", nil, Viewer{}); err == nil {
		t.Error("action accepted a redirect response")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("sent %d requests, want 1", got)
	}
}
