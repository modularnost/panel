package modules

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modularnost/internal/store"
)

const catalogFixture = `{"schema_version":1,"modules":[{
	"id":"metrics","title":"Metrics","summary":"Historical metrics","maintainer":"Modularnost",
	"source_url":"https://example.org/metrics","requires":{"contract":3,"min_panel_version":"0.7.0"},
	"permissions":["stats:read"],"install":{"kind":"swarm_service","version":"0.4.0",
	"image":"ghcr.io/modularnost/metrics-module@sha256:5ad2ee64fa1c76ac720162f914d16e595b23053ede126d39ac2e27c21a189c61",
	"data_volume":{"mount_path":"/data"}}
}]}`

func TestCatalogValidation(t *testing.T) {
	for _, test := range []struct{ old, new string }{
		{`"schema_version":1`, `"schema_version":2`},
		{`"/data"`, `"/data/../other"`},
		{`"0.4.0"`, `"0.4"`},
		{`"stats:read"`, `"stats:read","stats:read"`},
		{`"https://example.org/metrics"`, `"javascript:alert(1)"`},
		{`"permissions"`, `"unknown"`},
		{`"kind":"swarm_service"`, `"kind":"swarm_service","unknown":true`},
		{`"kind":"swarm_service"`, `"kind":""`},
		{`"id":"metrics"`, `"screenshots":[{"path":"screenshots/other/a.png","alt":"A"}],"id":"metrics"`},
	} {
		if _, err := decodeCatalog([]byte(strings.Replace(catalogFixture, test.old, test.new, 1))); err == nil {
			t.Errorf("accepted %s", test.new)
		}
	}
	entries, err := decodeCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	if e.Incompatibility("v0.7.0") != "" || e.Incompatibility("v0.10.0") != "" ||
		e.Incompatibility("v0.7.0-rc.1") == "" || e.Incompatibility("dev") == "" {
		t.Fatal("wrong compatibility")
	}
	edited := e
	edited.Install.Version = "0.4.1"
	if edited.Revision() == e.Revision() {
		t.Fatal("install changes must invalidate confirmations")
	}
}

func TestCatalogSkipsUnsupportedInstallKinds(t *testing.T) {
	const builder = `{
		"id":"builder","title":"Builder","summary":"Builds and deploys apps","maintainer":"Modularnost",
		"source_url":"https://example.org/builder","requires":{"contract":3},
		"permissions":["service:create"],"install":{"kind":"future_stack","bundle":{"version":1}}
	}`
	body := strings.TrimSuffix(catalogFixture, "]}") + "," + builder + "]}"
	entries, err := decodeCatalog([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "metrics" {
		t.Fatalf("expected only metrics, got %+v", entries)
	}
	if _, err := decodeCatalog([]byte(strings.Replace(body, `"id":"builder"`, `"id":"Metrics"`, 1))); err == nil {
		t.Fatal("accepted duplicate IDs across install kinds")
	}
}

func TestCatalogSwarmStack(t *testing.T) {
	const entry = `{"schema_version":1,"modules":[{
		"id":"builder","title":"Builder","summary":"Builds and deploys apps","maintainer":"Modularnost",
		"source_url":"https://github.com/modularnost/builder_module","requires":{"contract":3},
		"permissions":["service:read","service:create"],"install":{"kind":"swarm_stack","version":"0.1.0",
		"image":"ghcr.io/modularnost/builder-module@sha256:5ad2ee64fa1c76ac720162f914d16e595b23053ede126d39ac2e27c21a189c61"}
	}]}`
	entries, err := decodeCatalog([]byte(entry))
	if err != nil || len(entries) != 1 || entries[0].Install.Kind != CatalogSwarmStack {
		t.Fatalf("expected a digest-pinned Swarm stack, got %v, %v", entries, err)
	}
	other := strings.Replace(entry, `"id":"builder"`, `"id":"observability"`, 1)
	other = strings.Replace(other, "ghcr.io/modularnost/builder-module@", "registry.example.org/acme/observability@", 1)
	if entries, err := decodeCatalog([]byte(other)); err != nil || len(entries) != 1 || entries[0].ID != "observability" {
		t.Fatalf("expected another module to use the same install format, got %v, %v", entries, err)
	}
	for _, test := range []struct{ old, new string }{
		{"builder-module@", "builder-module:latest@"},
		{`"kind":"swarm_stack"`, `"kind":"swarm_stack","port":8080`},
	} {
		if _, err := decodeCatalog([]byte(strings.Replace(entry, test.old, test.new, 1))); err == nil {
			t.Errorf("accepted %s", test.new)
		}
	}
}

func TestCatalogKeepsLastGoodSnapshot(t *testing.T) {
	var reply atomic.Value
	reply.Store("invalid")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(reply.Load().(string)))
	}))
	defer server.Close()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "panel.db"))
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := NewCatalog(t.Context(), db)
	c.url = server.URL
	if err := c.Refresh(t.Context()); err == nil || !c.Status().CheckedAt.IsZero() {
		t.Fatal("first failed check should be unavailable")
	}
	reply.Store(catalogFixture)
	if err := c.Refresh(t.Context()); err != nil || !c.Status().Fresh(time.Now()) {
		t.Fatalf("valid catalog not saved: %v", err)
	}
	reply.Store(strings.Repeat(" ", maxCatalogBytes+1))
	if err := c.Refresh(t.Context()); err == nil || c.Status().Fresh(time.Now()) || len(c.Status().Entries) != 1 {
		t.Fatalf("failed refresh must preserve entries and mark them stale: %v", err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c = NewCatalog(t.Context(), reopened)
	if c.Status().Fresh(time.Now()) || len(c.Status().Entries) != 1 {
		t.Fatal("restarted panel must show the cache but require a refresh before installing")
	}
}
