package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

// uiFixture is a pinned module "m" with two ui pages: a viewer's page and an
// operator's tab. seen records what the module was asked.
type uiFixture struct {
	c      *http.Client
	base   string
	server *Server
	reg    *modules.Registry
	seen   struct{ method, path, query, user, role, body string }
	answer string
}

func TestModulePageStreamRefreshesOnConnectAndReconnect(t *testing.T) {
	f := newUIFixture(t)
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+"/api/v1/modules/m/events?path=/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := f.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var event, data string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				break
			}
		}
		if resp.StatusCode != http.StatusOK || event != "changed" || data != `{"path":"/"}` {
			t.Fatalf("first SSE event: %d %q %q (%v)", resp.StatusCode, event, data, scanner.Err())
		}
		cancel()
	}
}

func TestModuleStatsHintsOnlyItsWatchedPagesOnRequest(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/info"):
			_, _ = io.WriteString(w, `{}`)
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
	reg := modules.NewRegistry(nil, "secret")
	reg.Pin(modules.Module{Name: "m", Manifest: modules.Manifest{Permissions: []string{"stats:read"}}})
	s := &Server{Modules: reg, Metrics: agent.NewCollector("", "", "", dk)}
	handler := s.Routes()
	_, stopOther := s.Pages.Subscribe("other", "/other")
	defer stopOther()
	assertHint := func(query, token string, status int, hint, cache string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/internal/stats"+query, nil)
		req.Header.Set("X-Panel-Module", "m")
		req.Header.Set("X-Panel-Token", token)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status || res.Header().Get("X-Panel-Watched-Pages") != hint || res.Header().Get("Cache-Control") != cache {
			t.Fatalf("stats %s: got %d %v %s", query, res.Code, res.Header(), res.Body)
		}
	}
	token := reg.Token("m")
	assertHint("?page_interest=1", "", http.StatusUnauthorized, "", "")
	assertHint("?page_interest=1", token, http.StatusOK, "none", "no-store")
	_, stop := s.Pages.Subscribe("m", "/service")
	defer stop()
	_, stopRoot := s.Pages.Subscribe("m", "/")
	defer stopRoot()
	assertHint("", token, http.StatusOK, "", "")
	assertHint("?page_interest=1", token, http.StatusOK, "path=%2F&path=%2Fservice", "no-store")
	stop()
	assertHint("?page_interest=1", token, http.StatusOK, "path=%2F", "no-store")
}

func newUIFixture(t *testing.T) *uiFixture {
	t.Helper()
	f := &uiFixture{answer: `{"ui":1,"blocks":[{"type":"text","text":"hello"}]}`}
	mod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen.method, f.seen.path, f.seen.query = r.Method, r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		f.seen.body = string(b)
		f.seen.user, f.seen.role = r.Header.Get("X-Panel-User"), r.Header.Get("X-Panel-Role")
		w.Write([]byte(f.answer))
	}))
	t.Cleanup(mod.Close)

	f.reg = modules.NewRegistry(nil, "secret")
	f.reg.Pin(modules.Module{Name: "m", BaseURL: mod.URL, Manifest: modules.Manifest{Contributions: []modules.Contribution{
		{Slot: modules.SlotNav, Path: "/", Role: store.RoleViewer},
		{Slot: modules.SlotServiceTab, Path: "/service", Role: store.RoleOperator},
		{Slot: modules.SlotNav, Path: "/action/danger", Role: store.RoleAdmin},
	}}})
	f.c, f.base, _ = apiServerWith(t, func(s *Server) { s.Modules = f.reg; f.server = s })
	if status, _ := send(t, f.c, "POST", f.base+"/api/v1/auth/login", "application/json", `{"username":"ann","password":"s3cret-pass"}`); status != 200 {
		t.Fatal("login failed")
	}
	return f
}

func TestModulePageIsTheModulesDocumentForTheViewer(t *testing.T) {
	f := newUIFixture(t)
	query := url.QueryEscape("service=store #1&range=1h&range=6h")
	status, body := send(t, f.c, "GET", f.base+"/api/v1/modules/m/page?path=/&query="+query, "", "")
	blocks, _ := body["blocks"].([]any)
	if status != 200 || body["ui"] != float64(1) || len(blocks) != 1 {
		t.Fatalf("%d %v", status, body)
	}
	if f.seen.path != "/" || f.seen.query != "range=1h&range=6h&service=store+%231" || f.seen.user != "ann" || f.seen.role != store.RoleViewer {
		t.Errorf("the module was asked %+v", f.seen)
	}
}

func TestModulePageRefusals(t *testing.T) {
	f := newUIFixture(t)
	for name, c := range map[string]struct {
		url    string
		status int
		code   string
	}{
		"a role below the page's":        {"/api/v1/modules/m/page?path=/service", 403, "forbidden"},
		"a page the module never said":   {"/api/v1/modules/m/page?path=/other", 404, "not_found"},
		"a module that is not installed": {"/api/v1/modules/nope/page?path=/", 404, "not_found"},
		"no path at all":                 {"/api/v1/modules/m/page", 422, "invalid_input"},
		"invalid query escape":           {"/api/v1/modules/m/page?path=/&query=" + url.QueryEscape("range=%xx"), 400, "bad_request"},
	} {
		if status, body := send(t, f.c, "GET", f.base+c.url, "", ""); status != c.status || errorCode(body) != c.code {
			t.Errorf("%s: %d %v, want %d %s", name, status, body, c.status, c.code)
		}
	}
	if f.seen.path != "" {
		t.Errorf("a refused request still reached the module: %+v", f.seen)
	}

	f.answer = `<html>`
	if status, body := send(t, f.c, "GET", f.base+"/api/v1/modules/m/page?path=/", "", ""); status != 502 || errorCode(body) != "module_error" {
		t.Errorf("a module that answers something else: %d %v", status, body)
	}

	anon := &http.Client{}
	if status, _ := send(t, anon, "GET", f.base+"/api/v1/modules/m/page?path=/", "", ""); status != 401 {
		t.Errorf("anonymous: %d", status)
	}
}

func TestModuleActionReachesTheModuleAsTheViewer(t *testing.T) {
	f := newUIFixture(t)
	f.answer = `{"message":"redeployed"}`
	url := f.base + "/api/v1/modules/m/action"
	status, body := send(t, f.c, "POST", url, "application/json", `{"page":"/","action":"/action/redeploy","values":{"service":"shop"}}`)
	if status != 200 || body["message"] != "redeployed" {
		t.Fatalf("%d %v", status, body)
	}
	if f.seen.method != "POST" || f.seen.path != "/action/redeploy" || f.seen.body != `{"service":"shop"}` || f.seen.user != "ann" {
		t.Errorf("the module was asked %+v", f.seen)
	}

	f.seen.path = ""
	for name, c := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"a role below the action's":         {`{"page":"/","action":"/action/danger"}`, 403, "forbidden"},
		"outside /action/":                  {`{"page":"/","action":"/service"}`, 400, "bad_request"},
		"climbing out of /action/":          {`{"page":"/","action":"/action/../manifest"}`, 400, "bad_request"},
		"from a page the module never said": {`{"page":"/other","action":"/action/x"}`, 404, "not_found"},
	} {
		if status, body := send(t, f.c, "POST", url, "application/json", c.body); status != c.status || errorCode(body) != c.code {
			t.Errorf("%s: %d %v, want %d %s", name, status, body, c.status, c.code)
		}
	}
	if f.seen.path != "" {
		t.Errorf("a refused action still reached the module: %+v", f.seen)
	}
}

func TestModuleReportsAChangedPage(t *testing.T) {
	f := newUIFixture(t)
	changed, stop := f.server.Pages.Subscribe("m", "/")
	defer stop()
	elsewhere, stopElsewhere := f.server.Pages.Subscribe("m", "/service")
	defer stopElsewhere()

	post := func(token, module, body string) int {
		req, _ := http.NewRequest("POST", f.base+"/internal/ui/changed", strings.NewReader(body))
		req.Header.Set("X-Panel-Module", module)
		req.Header.Set("X-Panel-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	token := f.reg.Token("m")
	if got := post(token, "m", `{"path":"/"}`); got != 204 || !fired(changed) {
		t.Fatalf("a valid report: %d, woken %v", got, fired(changed))
	}
	if fired(elsewhere) {
		t.Error("another page was woken")
	}
	for name, c := range map[string]struct {
		token, module, body string
		want                int
	}{
		"no token":                  {"", "m", `{"path":"/"}`, 401},
		"a token of another module": {f.reg.Token("other"), "m", `{"path":"/"}`, 401},
		"a page it never declared":  {token, "m", `{"path":"/other"}`, 404},
		"a body that is not a path": {token, "m", `nope`, 400},
	} {
		if got := post(c.token, c.module, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
	if fired(changed) {
		t.Error("a refused report still woke a viewer")
	}
}
