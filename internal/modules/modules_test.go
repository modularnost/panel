package modules

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/firewall"
)

func TestDiscoveryServesCachedListWhileRefreshingAndHonorsForget(t *testing.T) {
	busy, release := make(chan struct{}), make(chan struct{})
	var scans atomic.Int32
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("API-Version", "1.40")
		case "/v1.40/services":
			if scans.Add(1) == 1 {
				close(busy)
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}
			_, _ = io.WriteString(w, `[]`)
		case "/v1.40/tasks":
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	dk, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	r := NewRegistry(dk, "test-secret")
	r.cached = []Module{{Name: "previous"}}
	r.at = time.Now().Add(-ttl)
	result := make(chan error, 1)
	go func() {
		_, err := r.list(ctx, true)
		result <- err
	}()
	select {
	case <-busy:
	case <-ctx.Done():
		t.Fatal("discovery refresh did not start")
	}
	quick, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	if mods, err := r.List(quick); err != nil || len(mods) != 1 || mods[0].Name != "previous" {
		t.Fatalf("List while refreshing = %v, %v", mods, err)
	}
	if _, ok := r.Get(quick, "previous"); ok {
		t.Fatal("Get accepted a module whose manifest has expired")
	}
	r.Forget()
	quick, stopAfterForget := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stopAfterForget()
	if _, err := r.List(quick); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("List after Forget returned %v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if mods, err := r.List(ctx); err != nil || len(mods) != 0 || scans.Load() != 2 {
		t.Fatalf("List after refresh = %v, %v, Docker scans = %d", mods, err, scans.Load())
	}
}

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
