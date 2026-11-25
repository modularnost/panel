package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestReadModuleBundleFromDocker(t *testing.T) {
	image := os.Getenv("PANEL_MODULE_BUNDLE_IMAGE")
	if image == "" {
		t.Skip("set PANEL_MODULE_BUNDLE_IMAGE to a module image pinned by digest")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	bundle, err := c.ReadModuleBundle(t.Context(), image)
	if err != nil {
		t.Fatal(err)
	}
	for _, stack := range bundle.StackFiles {
		if len(bundle.Files[stack]) == 0 {
			t.Fatalf("missing stack file %q", stack)
		}
	}
	t.Logf("stack files %v, encrypted registry credentials required: %t", bundle.StackFiles, bundle.RegistryCredentials)
}

func TestReadModuleBundle(t *testing.T) {
	const repo = "ghcr.io/example/observability-module"
	digest := "sha256:" + strings.Repeat("a", 64)
	image := repo + "@" + digest
	for _, bad := range []string{repo + ":edge", repo + ":edge@" + digest} {
		var c Client
		if _, err := c.ReadModuleBundle(t.Context(), bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}

	for _, test := range []struct {
		name       string
		archive    []byte
		cancelCopy bool
		wantErr    string
	}{
		{name: "valid", archive: moduleBundleArchive(t, "", 0)},
		{name: "duplicate file", archive: moduleBundleArchive(t, "deploy/stacks/app.yaml", tar.TypeReg), wantErr: "invalid module bundle file"},
		{name: "symlink", archive: moduleBundleArchive(t, "deploy/assets/link", tar.TypeSymlink), wantErr: "invalid module bundle file"},
		{name: "traversal", archive: moduleBundleArchive(t, "deploy/assets/../escape", tar.TypeReg), wantErr: "invalid module bundle"},
		{name: "missing files", archive: []byte{}, wantErr: "missing module bundle manifest.json"},
		{name: "canceled read", cancelCopy: true, wantErr: "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var lifecycle atomic.Int32
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/_ping":
					w.Header().Set("API-Version", "1.51")
				case r.Method == http.MethodPost && r.URL.Path == "/v1.51/images/create":
					if lifecycle.Add(1) != 1 || r.URL.Query().Get("fromImage") != repo || r.URL.Query().Get("tag") != digest {
						t.Error("expected to pull the catalog digest first")
					}
					_, _ = io.WriteString(w, `{"status":"complete"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/v1.51/containers/create":
					var input struct {
						container.Config
						HostConfig container.HostConfig
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					if lifecycle.Add(1) != 2 || input.Image != image || input.HostConfig.NetworkMode != "none" || !strings.HasPrefix(r.URL.Query().Get("name"), "panel-module-bundle-") {
						t.Error("expected to create an isolated container from the same digest")
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"Id":"bundle-test"}`)
				case r.Method == http.MethodGet && r.URL.Path == "/v1.51/containers/bundle-test/archive":
					if lifecycle.Add(1) != 3 || r.URL.Query().Get("path") != "/usr/share/modularnost/deploy" {
						t.Error("expected to copy the deployment files from the stopped container")
					}
					if test.cancelCopy {
						cancel()
						<-r.Context().Done()
						return
					}
					w.Header().Set("X-Docker-Container-Path-Stat", base64.StdEncoding.EncodeToString([]byte(`{"name":"deploy"}`)))
					_, _ = w.Write(test.archive)
				case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1.51/containers/panel-module-bundle-"):
					if lifecycle.Add(1) != 4 || r.URL.Query().Get("v") != "1" {
						t.Error("expected to remove the temporary container with its volumes")
					}
					w.WriteHeader(http.StatusNoContent)
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
			bundle, err := c.ReadModuleBundle(ctx, image)
			if lifecycle.Load() != 4 {
				t.Errorf("unexpected Docker lifecycle length %d", lifecycle.Load())
			}
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected error %q, got %v", test.wantErr, err)
				}
				return
			}
			if err != nil || !slices.Equal(bundle.StackFiles, []string{"stacks/app.yaml", "stacks/collector.yml"}) ||
				string(bundle.Files["assets/settings.conf"]) != "example settings" || bundle.RegistryCredentials {
				t.Fatalf("unexpected module bundle: %v, %v", bundle, err)
			}
		})
	}
}

func moduleBundleArchive(t *testing.T, extra string, extraType byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	w := tar.NewWriter(&archive)
	for name, content := range map[string]string{
		"deploy/manifest.json":        `{"schema_version":1,"stack_files":["stacks/app.yaml","stacks/collector.yml"]}`,
		"deploy/stacks/app.yaml":      "services: {}\n",
		"deploy/stacks/collector.yml": "services: {}\n",
		"deploy/assets/settings.conf": "example settings",
	} {
		if err := w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		if err := w.WriteHeader(&tar.Header{Name: extra, Typeflag: extraType, Linkname: "../elsewhere"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestModuleBundleManifest(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":2,"stack_files":["stack.yml"]}`,
		`{"schema_version":1,"stack_files":["missing.yml"]}`,
		`{"schema_version":1,"stack_files":["stack.yml","stack.yml"]}`,
		`{"schema_version":1,"stack_files":["/stack.yml"]}`,
		`{"schema_version":1,"stack_files":["stack.yml"],"execute":"setup.sh"}`,
	} {
		if _, err := parseModuleBundleManifest(map[string][]byte{"manifest.json": []byte(raw), "stack.yml": []byte("services: {}\n")}); err == nil {
			t.Errorf("accepted manifest %s", raw)
		}
	}
	bundle, err := parseModuleBundleManifest(map[string][]byte{
		"manifest.json": []byte(`{"schema_version":1,"stack_files":["stack.yml"],"registry_credentials":true}`),
		"stack.yml":     []byte("services: {}\n"),
	})
	if err != nil || !bundle.RegistryCredentials {
		t.Fatalf("expected a registry credential requirement, got %v, %v", bundle, err)
	}
}
