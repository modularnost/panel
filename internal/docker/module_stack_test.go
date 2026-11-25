package docker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRemoveCatalogStackRechecksDisappearingNetwork(t *testing.T) {
	var removed atomic.Bool
	var networkLists atomic.Int32
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/services":
			_, _ = io.WriteString(w, `[{"ID":"module-service","Spec":{"Name":"test_module"}}]`)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.51/services/module-service":
			removed.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/networks":
			if !removed.Load() || !strings.Contains(r.URL.Query().Get("filters"), "com.docker.stack.namespace=test") {
				t.Error("expected cleanup to check stack-owned networks after removing services")
			}
			if networkLists.Add(1) == 1 {
				_, _ = io.WriteString(w, `[{"Id":"old-network","Name":"test_default"}]`)
			} else {
				_, _ = io.WriteString(w, `[]`)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.51/networks/old-network":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.RemoveCatalogStack(t.Context(), "test"); err != nil || networkLists.Load() < 2 {
		t.Fatalf("expected cleanup to confirm that the stack network disappeared: %v", err)
	}
}

func TestCatalogStackRejectsHostResources(t *testing.T) {
	for _, compose := range []string{
		"include: /etc/shadow\nservices: {module: {}}",
		"services: {module: {env_file: /etc/shadow}}",
		"services: {module: {privileged: true}}",
		"services: {module: {volumes: ['/var/run/docker.sock:/var/run/docker.sock']}}",
		"services: {module: {deploy: {labels: {panel.module: true}}}}",
		"services: {module: {ports: ['80:8080']}}",
		"services: {module: {}}\n---\nservices: {other: {}}",
	} {
		if _, err := catalogComposeFragment([]byte(compose)); err == nil {
			t.Errorf("accepted unsupported module Compose: %q", compose)
		}
	}
}

func TestRenderCatalogStack(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI is required for Compose merge")
	}
	primary := "ghcr.io/example/collector@sha256:" + strings.Repeat("a", 64)
	sidecar := "ghcr.io/example/worker@sha256:" + strings.Repeat("b", 64)
	t.Setenv("PANEL_TEST_SECRET", "secret-that-must-not-be-expanded")
	bundle := ModuleBundle{
		StackFiles: []string{"base.yml", "overrides.yml"},
		Files: map[string][]byte{
			"base.yml":      []byte("services:\n  module:\n    environment:\n      FEATURE: first\n      LITERAL: '${PANEL_TEST_SECRET}'\n  worker:\n    image: " + sidecar + "\n"),
			"overrides.yml": []byte("services:\n  module:\n    environment:\n      FEATURE: second\n"),
		},
	}
	spec := CatalogStack{Image: primary, PanelNetwork: "panelnet", CoreURL: "http://panel:8080", Labels: map[string]string{"panel.module": "true"}}
	compose, count, err := RenderCatalogStack(t.Context(), bundle, spec)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || strings.Count(compose, "- panel") != 1 || strings.Contains(compose, "secret-that-must-not-be-expanded") {
		t.Fatalf("unexpected rendered module stack: %s", compose)
	}
	for _, expected := range []string{primary, sidecar, "FEATURE: second", "$${PANEL_TEST_SECRET}", "panel.module: \"true\"", "PANEL_CORE_URL: http://panel:8080"} {
		if !strings.Contains(compose, expected) {
			t.Errorf("missing %q in stack: %s", expected, compose)
		}
	}
	bundle.Files["base.yml"] = []byte("services:\n  module: {}\n  worker:\n    image: alpine:latest\n")
	if _, _, err := RenderCatalogStack(t.Context(), bundle, spec); err == nil {
		t.Fatal("accepted a mutable sidecar image")
	}
}
