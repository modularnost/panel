package web

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

func TestStackPreflightReadsApprovedImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case strings.HasPrefix(r.URL.Path, "/v1.51/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = io.WriteString(w, `{"Config":{"Labels":{"com.docker.swarm.service.name":"panel_panel"}},"NetworkSettings":{"Networks":{"panelnet":{}}}}`)
		case r.URL.Path == "/v1.51/networks":
			_, _ = io.WriteString(w, `[{"Name":"panelnet","Driver":"overlay"}]`)
		case r.URL.Path == "/v1.51/images/create":
			if q := r.URL.Query(); q.Get("fromImage") != "ghcr.io/example/observability-module" || q.Get("tag") != digest {
				t.Errorf("unexpected image pull: %v", q)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"registry requires authentication"}`)
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
	t.Setenv("PANEL_NETWORK", "")
	dk, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()
	s := Server{Docker: dk}
	_, _, _, err = s.prepareCatalogStack(t.Context(), modules.CatalogEntry{Install: modules.CatalogInstall{Image: "ghcr.io/example/observability-module@" + digest}}, nil, false)
	var p *apiProblem
	if !errors.As(err, &p) || p.status != http.StatusBadGateway || p.Detail.Code != "module_bundle_unavailable" {
		t.Fatalf("unexpected preflight result: %v", err)
	}
}

func TestModuleCatalogIsAdminOnlyAndStaleInstallsAreRefused(t *testing.T) {
	c, base, db := apiServerWith(t, func(s *Server) {
		if err := s.DB.SaveModuleCatalog(t.Context(), []byte(`{"schema_version":1,"modules":[]}`), time.Now()); err != nil {
			t.Fatal(err)
		}
		s.Catalog = modules.NewCatalog(t.Context(), s.DB)
	})
	catalog := base + "/api/v1/module-catalog"
	preflight := catalog + "/observability/preflight"
	confirmation := `{"version":"0.1.0","revision":"` + strings.Repeat("a", 64) + `"}`
	if status, _ := send(t, c, "GET", catalog, "", ""); status != http.StatusUnauthorized {
		t.Fatalf("signed-out access returned %d", status)
	}
	if status, _ := send(t, c, "POST", preflight, "application/json", confirmation); status != http.StatusUnauthorized {
		t.Fatalf("signed-out preflight returned %d", status)
	}
	signIn(t, c, base)
	if status, _ := send(t, c, "GET", catalog, "", ""); status != http.StatusForbidden {
		t.Fatalf("viewer access returned %d", status)
	}
	if status, _ := send(t, c, "POST", preflight, "application/json", confirmation); status != http.StatusForbidden {
		t.Fatalf("viewer preflight returned %d", status)
	}
	if _, err := db.CreateUser("root", "catalog-admin-key", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if status, body := send(t, c, "POST", base+"/api/v1/auth/login", "application/json",
		`{"username":"root","password":"catalog-admin-key"}`); status != http.StatusOK {
		t.Fatalf("admin login: %d %v", status, body)
	}
	if status, body := send(t, c, "GET", catalog, "", ""); status != http.StatusOK || body["state"] != "stale" {
		t.Fatalf("cached catalog: %d %v", status, body)
	}
	if status, body := send(t, c, "POST", catalog+"/metrics/install", "application/json",
		`{"version":"0.4.0","revision":"`+strings.Repeat("a", 64)+`"}`); status != http.StatusConflict || errorCode(body) != "catalog_stale" {
		t.Fatalf("stale install: %d %v", status, body)
	}
	if status, body := send(t, c, "POST", preflight, "application/json", confirmation); status != http.StatusConflict || errorCode(body) != "catalog_stale" {
		t.Fatalf("stale preflight: %d %v", status, body)
	}
}
