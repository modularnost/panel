package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	status, body := send(t, f.c, "GET", f.base+"/api/v1/modules/m/page?path=/&query=range%3D1h%26service%3Dshop", "", "")
	blocks, _ := body["blocks"].([]any)
	if status != 200 || body["ui"] != float64(1) || len(blocks) != 1 {
		t.Fatalf("%d %v", status, body)
	}
	if f.seen.path != "/" || f.seen.query != "range=1h&service=shop" || f.seen.user != "ann" || f.seen.role != store.RoleViewer {
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
