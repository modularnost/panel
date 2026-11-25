package docker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

func TestManagedRegistryCompose(t *testing.T) {
	const base = `
services:
  module: {image: ignore}
  registry:
    image: registry:3
    secrets: [{source: registry_cert}]
secrets:
  registry_cert: {external: true}
  unused: {external: true}
`
	const overlay = `
services:
  registry-pull:
    image: caddy:2
`
	bundle := ModuleBundle{RegistryCredentials: true, StackFiles: []string{"base", "overlay"}, Files: map[string][]byte{"base": []byte(base), "overlay": []byte(overlay)}}
	in := ManagedRegistry{Endpoint: "registry.example:5443", Hostname: "registry.example", BuildNetwork: "private-build", PullNetwork: "private-pull", PullEntrypoint: "private-tls", Node: "testnode"}
	got, err := registryCompose(bundle, in, "/tmp/Caddyfile", "recipe", map[string]bool{"registry-pull": true})
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Services map[string]map[string]any
		Secrets  map[string]any
	}
	if err := yaml.Unmarshal([]byte(got), &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Services) != 1 || spec.Services["registry"] == nil || len(spec.Secrets) != 1 || spec.Secrets["registry_cert"] == nil {
		t.Fatalf("unexpected partial retry compose:\n%s", got)
	}
	for _, want := range []string{"node.id==testnode", "registry_data:/var/lib/registry", "private-build", "private-pull", "external: true", `traefik.enable: "false"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	got, err = registryCompose(bundle, in, "/tmp/Caddyfile", "recipe", nil)
	if err != nil || !strings.Contains(got, `HostSNI("registry.example")`) || !strings.Contains(got, `.tls.passthrough: "true"`) {
		t.Fatalf("missing private TLS passthrough route: %v\n%s", err, got)
	}
	bundle.Files["overlay"] = []byte(overlay + "    ports: [443]\n")
	if _, err := registryCompose(bundle, in, "/tmp/Caddyfile", "recipe", nil); err == nil {
		t.Fatal("accepted a bundle exposing a registry port")
	}
}

func TestRegistryProbePullsAnonymouslyFirst(t *testing.T) {
	const host = "192.0.2.15:5443"
	digest := "sha256:" + strings.Repeat("a", 64)
	image := host + "/probe@" + digest
	if !ValidRegistryProbe(image, host) || ValidRegistryProbe(image, "192.0.2.15") || ValidRegistryProbe(host+"/probe:latest", host) {
		t.Fatal("registry probe did not enforce the exact endpoint and digest")
	}
	for _, tt := range []struct {
		name, failure, result string
		pulls                 int
	}{
		{"authenticated", "unauthorized", "verified", 2},
		{"public", "", "anonymous_pull_allowed", 1},
		{"untrusted", "x509: unknown authority", "anonymous_pull_unverified", 1},
		{"insecure", "", "insecure_registry", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var pulls atomic.Int32
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/_ping":
					w.Header().Set("API-Version", "1.51")
				case strings.HasSuffix(r.URL.Path, "/info"):
					if tt.name == "insecure" {
						_, _ = io.WriteString(w, `{"RegistryConfig":{"InsecureRegistryCIDRs":["192.0.2.0/24"]}}`)
					} else {
						_, _ = io.WriteString(w, `{"RegistryConfig":{}}`)
					}
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
					attempt := pulls.Add(1)
					auth := r.Header.Get("X-Registry-Auth")
					if r.URL.Query().Get("fromImage") != host+"/probe" || r.URL.Query().Get("tag") != digest || attempt == 1 && auth != "" || attempt == 2 && auth != "encoded" {
						t.Errorf("unexpected pull query or credentials, attempt %d", attempt)
					}
					if attempt == 1 && tt.failure != "" {
						_, _ = io.WriteString(w, `{"errorDetail":{"message":"`+tt.failure+`"}}`)
					} else {
						_, _ = io.WriteString(w, `{"status":"complete"}`)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer daemon.Close()
			t.Setenv("DOCKER_HOST", daemon.URL)
			t.Setenv("DOCKER_API_VERSION", "")
			c, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			result, err := c.RegistryProbe(t.Context(), host, image, "encoded")
			if count := int(pulls.Load()); err != nil || result != tt.result || count != tt.pulls {
				t.Fatalf("result=%q err=%v pulls=%d, want %q and %d pulls", result, err, count, tt.result, tt.pulls)
			}
		})
	}
}

func TestProvisionManagedRegistryOnSwarm(t *testing.T) {
	image := os.Getenv("PANEL_TEST_BUILDER_IMAGE")
	if image == "" {
		t.Skip("set PANEL_TEST_BUILDER_IMAGE for a disposable Swarm")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	node, err := c.LocalSwarmNode(t.Context())
	if err != nil || node == "" {
		t.Fatalf("local node: %q %v", node, err)
	}
	spec := ManagedRegistry{Endpoint: os.Getenv("PANEL_TEST_PULL_HOST"), Hostname: os.Getenv("PANEL_TEST_PULL_DNS"), BuildNetwork: "build247", PullNetwork: "pull247", Node: node}
	for range 2 {
		if err := c.ProvisionManagedRegistry(t.Context(), image, spec); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := c.ManagedRegistryExists(t.Context(), image, spec); !ok || err != nil {
		t.Fatalf("registry exists=%t err=%v", ok, err)
	}
}

func TestRegistryProbeOnCleanWorker(t *testing.T) {
	image := os.Getenv("PANEL_TEST_PROBE_IMAGE")
	if image == "" {
		t.Skip("set PANEL_TEST_PROBE_IMAGE to a prebuilt image on a disposable Swarm")
	}
	host := os.Getenv("PANEL_TEST_PULL_HOST")
	if !ValidRegistryProbe(image, host) {
		t.Fatal("probe image must be pinned to the endpoint")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	images, err := c.api.ImageList(t.Context(), client.ImageListOptions{})
	if err != nil || len(images.Items) != 0 {
		t.Fatalf("expected a clean worker, got %d images and %v", len(images.Items), err)
	}
	auth, err := authconfig.Encode(registry.AuthConfig{ServerAddress: host, Username: os.Getenv("PANEL_TEST_PULL_USER"), Password: os.Getenv("PANEL_TEST_PULL_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.RegistryProbe(t.Context(), host, image, auth)
	if result != "verified" || err != nil {
		t.Fatalf("registry probe result=%s err=%v", result, err)
	}
}
