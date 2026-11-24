package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/api/pkg/authconfig"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

func TestModuleServiceIsRefusedBeforeItIsCreated(t *testing.T) {
	for name, c := range map[string]struct {
		in   moduleService
		want string
	}{
		"a good one":          {moduleService{Name: "shop", Image: "127.0.0.1:5000/shop:1", Env: []string{"A=b"}, Labels: map[string]string{"traefik.enable": "true"}}, ""},
		"a bad name":          {moduleService{Name: "-shop", Image: "i"}, "name:"},
		"no image":            {moduleService{Name: "shop", Image: " "}, "image is required"},
		"a panel label":       {moduleService{Name: "shop", Image: "i", Labels: map[string]string{"panel.module": "true"}}, "panel.*"},
		"the owner label":     {moduleService{Name: "shop", Image: "i", Labels: map[string]string{labelOwner: "other"}}, "panel.*"},
		"an env without name": {moduleService{Name: "shop", Image: "i", Env: []string{"=x"}}, "env:"},
	} {
		err := c.in.validate()
		if (err == nil) != (c.want == "") || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestTagIsOnlyATag(t *testing.T) {
	for tag, ok := range map[string]bool{
		"1": true, "abc123": true, "v1.2.3-rc.1": true, "_x": true,
		"": false, "a:b": false, "a/b": false, "-x": false, "a b": false, "x@sha256:1": false,
	} {
		if tagRe.MatchString(tag) != ok {
			t.Errorf("%q: %v, want %v", tag, !ok, ok)
		}
	}
}

func TestModuleNeedsServiceCreateToCreateOrMove(t *testing.T) {
	reg := modules.NewRegistry(nil, "secret")
	reg.Pin(modules.Module{Name: "m", Manifest: modules.Manifest{Permissions: []string{"service:update"}}})
	_, base, _ := apiServerWith(t, func(s *Server) { s.Modules = reg })

	for _, path := range []string{"/internal/services", "/internal/services/x/image"} {
		req, _ := http.NewRequest("POST", base+path, strings.NewReader(`{}`))
		req.Header.Set("X-Panel-Module", "m")
		req.Header.Set("X-Panel-Token", reg.Token("m"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s without the right: %d", path, resp.StatusCode)
		}
	}
}

func TestModuleServicePullRegistry(t *testing.T) {
	const host = "registry.example.net:5000"
	const other = "other.example.net"
	const service = `{"ID":"shop","Spec":{"Name":"shop","Labels":{"panel.owner":"m"},"TaskTemplate":{"ContainerSpec":{"Image":"` + host + `/shop:1"}}}}`
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/v1.51")
		switch {
		case path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case strings.HasPrefix(path, "/containers/"):
			fmt.Fprint(w, `{"Config":{},"NetworkSettings":{"Networks":{"panelnet":{}}}}`)
		case path == "/services":
			fmt.Fprint(w, "["+service+"]")
		case path == "/tasks":
			fmt.Fprint(w, "[]")
		case path == "/services/shop":
			fmt.Fprint(w, service)
		case strings.HasPrefix(path, "/distribution/"), path == "/services/create", path == "/services/shop/update":
			image := strings.TrimPrefix(path, "/distribution/")
			if r.Method == http.MethodPost {
				var spec struct {
					TaskTemplate struct {
						ContainerSpec struct{ Image string }
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
					t.Error(err)
				}
				image = spec.TaskTemplate.ContainerSpec.Image
			}
			token := r.Header.Get("X-Registry-Auth")
			if strings.HasPrefix(image, host+"/") {
				auth, err := authconfig.Decode(token)
				if err != nil || auth.Username != "reader" || auth.Password != "test-token" || auth.ServerAddress != host {
					t.Errorf("%s: expected credentials for the approved registry", path)
				}
			} else if token != "" {
				t.Errorf("%s: unexpected credentials for %s", path, image)
			}
			fmt.Fprint(w, `{"ID":"shop","Descriptor":{"digest":"sha256:`+strings.Repeat("a", 64)+`"}}`)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "1.51")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	dk, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dk.Close() })
	encryption, err := store.RegistryCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	reg := modules.NewRegistry(nil, "secret")
	reg.Pin(modules.Module{Name: "m", PullRegistry: host, Manifest: modules.Manifest{Permissions: []string{"service:create"}}})
	c, base, db := apiServerWith(t, func(s *Server) {
		s.Docker, s.Modules, s.RegistryEncryption = dk, reg, encryption
	})
	for _, name := range []string{host, other} {
		if err := db.SaveRegistry(store.Registry{Host: name, Username: "reader"}, "test-token", encryption); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/internal/services", `{"name":"shop","image":"` + host + `/shop:1"}`, http.StatusCreated},
		{"/internal/services", `{"name":"shop","image":"` + other + `/shop:1"}`, http.StatusCreated},
		{"/internal/services/shop/image", `{"tag":"2"}`, http.StatusOK},
	} {
		req, err := http.NewRequest(http.MethodPost, base+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Panel-Module", "m")
		req.Header.Set("X-Panel-Token", reg.Token("m"))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: got %d, want %d", tc.path, resp.StatusCode, tc.status)
		}
	}
}
