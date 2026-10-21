package web

import (
	"net/http"
	"strings"
	"testing"

	"modularnost/internal/modules"
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
