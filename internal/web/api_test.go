package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

func apiClient(t *testing.T) (*http.Client, string) {
	c, base, _ := apiServer(t)
	return c, base
}

func apiServer(t *testing.T) (*http.Client, string, *store.DB) {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateUser("ann", "s3cret-pass", store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&Server{DB: db}).Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}, srv.URL, db
}

func send(t *testing.T, c *http.Client, method, url, contentType, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestAPISession(t *testing.T) {
	c, base := apiClient(t)
	const j = "application/json"

	if status, body := send(t, c, "GET", base+"/api/v1/me", "", ""); status != 401 || errorCode(body) != "unauthorized" {
		t.Fatalf("anonymous /me: %d %v", status, body)
	}
	if status, body := send(t, c, "POST", base+"/api/v1/auth/login", j, `{"username":"ann","password":"nope"}`); status != 401 || errorCode(body) != "invalid_credentials" {
		t.Fatalf("wrong password: %d %v", status, body)
	}
	status, body := send(t, c, "POST", base+"/api/v1/auth/login", j, `{"username":"ann","password":"s3cret-pass"}`)
	if status != 200 || body["username"] != "ann" || body["role"] != store.RoleViewer {
		t.Fatalf("login: %d %v", status, body)
	}
	if status, body := send(t, c, "GET", base+"/api/v1/me", "", ""); status != 200 || body["username"] != "ann" {
		t.Fatalf("/me after login: %d %v", status, body)
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/auth/logout", j, `{}`); status != 204 {
		t.Fatalf("logout: %d", status)
	}
	if status, _ := send(t, c, "GET", base+"/api/v1/me", "", ""); status != 401 {
		t.Fatalf("/me after logout: %d", status)
	}
}

func TestAPIRefusesNonJSONWrites(t *testing.T) {
	c, base := apiClient(t)
	// What a hostile page can do with a form: no JSON content type.
	for _, ct := range []string{"", "application/x-www-form-urlencoded", "text/plain"} {
		status, body := send(t, c, "POST", base+"/api/v1/auth/login", ct, `{"username":"ann","password":"s3cret-pass"}`)
		if status != 415 || errorCode(body) != "unsupported_media_type" {
			t.Errorf("content type %q: %d %v", ct, status, body)
		}
	}
}

func TestAPIUnknownEndpointIsJSON(t *testing.T) {
	c, base := apiClient(t)
	if status, body := send(t, c, "GET", base+"/api/v1/nothing", "", ""); status != 404 || errorCode(body) != "not_found" {
		t.Fatalf("%d %v", status, body)
	}
}

func TestAPIServicesNeedSignIn(t *testing.T) {
	c, base := apiClient(t)
	for _, path := range []string{"/api/v1/services", "/api/v1/services/web"} {
		if status, body := send(t, c, "GET", base+path, "", ""); status != 401 || errorCode(body) != "unauthorized" {
			t.Errorf("%s: %d %v", path, status, body)
		}
	}
}

func TestToAPIService(t *testing.T) {
	sv := docker.Service{
		ID: "abc", Name: "web", Image: "nginx:1", Mode: "replicated",
		Running: 2, Desired: 2, Labels: map[string]string{"a": "b"},
	}
	list := toAPIService(sv, false)
	if !list.Healthy || list.Labels != nil {
		t.Errorf("list entry: %+v", list)
	}
	if detail := toAPIService(sv, true); detail.Labels["a"] != "b" {
		t.Errorf("detail lost its labels: %+v", detail)
	}
	sv.Running = 1
	if toAPIService(sv, false).Healthy {
		t.Error("1 of 2 replicas must not be healthy")
	}
}

// api/openapi.json is generated from the handlers and committed, so a change to
// the API shows up in review. Regenerate with UPDATE_OPENAPI=1 go test ./internal/web.
func TestOpenAPIIsCommitted(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	if rec.Code != 200 {
		t.Fatalf("spec: %d", rec.Code)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte('\n')

	const path = "../../api/openapi.json"
	if os.Getenv("UPDATE_OPENAPI") != "" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), buf.Bytes()) {
		t.Fatal("api/openapi.json is out of date: UPDATE_OPENAPI=1 go test ./internal/web")
	}
}

func signIn(t *testing.T, c *http.Client, base string) {
	t.Helper()
	status, body := send(t, c, "POST", base+"/api/v1/auth/login", "application/json", `{"username":"ann","password":"s3cret-pass"}`)
	if status != 200 {
		t.Fatalf("sign in: %d %v", status, body)
	}
}

func TestSummarizeStacks(t *testing.T) {
	svcs := []docker.Service{
		{Name: "web", Stack: "shop", Running: 2, Desired: 2},
		{Name: "db", Stack: "shop", Running: 0, Desired: 1},
		{Name: "api", Stack: "blog", Running: 1, Desired: 1},
		{Name: "loner", Running: 1, Desired: 1},
	}
	got := summarizeStacks(svcs)
	if len(got) != 2 || got[0].Name != "blog" || got[1].Name != "shop" {
		t.Fatalf("want blog and shop, sorted: %+v", got)
	}
	if !got[0].Healthy || got[1].Healthy {
		t.Errorf("one failing service makes the stack unhealthy: %+v", got)
	}
	if shop := got[1]; shop.ServiceCount != 2 || shop.Running != 2 || shop.Desired != 3 {
		t.Errorf("shop totals: %+v", shop)
	}
}

func TestAPIDeploys(t *testing.T) {
	c, base, db := apiServer(t)
	finish, _ := db.StartDeploy(store.DeployEvent{ServiceName: "web", Source: "ui", TriggeredBy: "ann"})
	finish("sha256:old", "sha256:new", nil)

	if status, _ := send(t, c, "GET", base+"/api/v1/deploys", "", ""); status != 401 {
		t.Fatalf("anonymous: %d", status)
	}
	signIn(t, c, base)

	resp, err := c.Get(base + "/api/v1/deploys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(list) != 1 || list[0]["service"] != "web" || list[0]["status"] != "success" || list[0]["new_digest"] != "sha256:new" {
		t.Fatalf("%d %v", resp.StatusCode, list)
	}
	if status, body := send(t, c, "GET", base+"/api/v1/deploys?limit=0", "", ""); status != 422 {
		t.Fatalf("limit=0 should fail validation: %d %v", status, body)
	}
}

func TestAPIClusterRoutesAreGated(t *testing.T) {
	c, base := apiClient(t)
	for _, path := range []string{"/api/v1/stats", "/api/v1/nodes", "/api/v1/nodes/join"} {
		if status, _ := send(t, c, "GET", base+path, "", ""); status != 401 {
			t.Errorf("%s anonymous: %d", path, status)
		}
	}
	signIn(t, c, base)
	// ann is a viewer: the node list and join tokens are for admins.
	for _, path := range []string{"/api/v1/nodes", "/api/v1/nodes/join"} {
		if status, body := send(t, c, "GET", base+path, "", ""); status != 403 || errorCode(body) != "forbidden" {
			t.Errorf("%s as viewer: %d %v", path, status, body)
		}
	}
	if status, body := send(t, c, "GET", base+"/api/v1/nodes/join?role=owner", "", ""); status != 403 && status != 422 {
		t.Errorf("an unknown role must not reach Docker: %d %v", status, body)
	}
}

func TestToAPITaskStatsKeepsNodeErrors(t *testing.T) {
	got := toAPITaskStats([]docker.TaskStats{
		{Node: "n1", Service: "web", Slot: 1, CPU: 12.5, Mem: 100, MemLimit: 200},
		{Node: "n2", Err: "agent unreachable"},
	})
	if len(got) != 2 || got[0].CPU != 12.5 || got[0].MemLimit != 200 || got[1].Error != "agent unreachable" {
		t.Fatalf("%+v", got)
	}
}

func TestAPIWebhooksAreAdminOnly(t *testing.T) {
	c, base, db := apiServer(t)
	if _, err := db.CreateUser("root", "root-pass-1", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	hook, err := db.CreateWebhook(store.Webhook{ServiceName: "web", ActionType: "force_update"})
	if err != nil {
		t.Fatal(err)
	}

	if status, _ := send(t, c, "GET", base+"/api/v1/webhooks", "", ""); status != 401 {
		t.Fatalf("anonymous: %d", status)
	}
	signIn(t, c, base) // ann, a viewer
	if status, body := send(t, c, "GET", base+"/api/v1/webhooks", "", ""); status != 403 || errorCode(body) != "forbidden" {
		t.Fatalf("viewer: %d %v", status, body)
	}

	if status, body := send(t, c, "POST", base+"/api/v1/auth/login", "application/json", `{"username":"root","password":"root-pass-1"}`); status != 200 {
		t.Fatalf("admin sign in: %d %v", status, body)
	}
	resp, err := c.Get(base + "/api/v1/webhooks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(list) != 1 || list[0]["path"] != "/hook/"+hook.SecretToken || list[0]["action"] != "force_update" {
		t.Fatalf("%d %v", resp.StatusCode, list)
	}
}

func TestToAPIModules(t *testing.T) {
	got := toAPIModules([]modules.Module{
		{Name: "metrics", Version: "0.1.0", Service: "metrics_module", Manifest: modules.Manifest{
			NavEntries:  []modules.NavEntry{{Title: "Metrics", Path: "/"}},
			Permissions: []string{"stats:read"},
		}},
		{Name: "broken", Err: "connection refused"},
	})
	if len(got) != 2 || !got[0].OK || got[0].NavEntries[0].Title != "Metrics" || got[0].Permissions[0] != "stats:read" {
		t.Fatalf("working module: %+v", got)
	}
	if got[1].OK || got[1].Error != "connection refused" || got[1].NavEntries == nil || got[1].Permissions == nil {
		t.Fatalf("broken module must keep its error and still list empty arrays, not null: %+v", got[1])
	}
}

func TestAPIUsersAndTokensNeverExposeSecrets(t *testing.T) {
	c, base, db := apiServer(t)
	root, err := db.CreateUser("root", "root-pass-1", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := db.CreateToken(root.ID, "ci", store.RoleOperator, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/v1/users", "/api/v1/tokens"} {
		if status, _ := send(t, c, "GET", base+path, "", ""); status != 401 {
			t.Errorf("%s anonymous: %d", path, status)
		}
	}
	signIn(t, c, base) // ann, a viewer
	for _, path := range []string{"/api/v1/users", "/api/v1/tokens"} {
		if status, body := send(t, c, "GET", base+path, "", ""); status != 403 || errorCode(body) != "forbidden" {
			t.Errorf("%s as viewer: %d %v", path, status, body)
		}
	}

	if status, body := send(t, c, "POST", base+"/api/v1/auth/login", "application/json", `{"username":"root","password":"root-pass-1"}`); status != 200 {
		t.Fatalf("admin sign in: %d %v", status, body)
	}
	get := func(path string) string {
		t.Helper()
		resp, err := c.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
		}
		return string(b)
	}

	users := get("/api/v1/users")
	if !strings.Contains(users, `"ann"`) || !strings.Contains(users, `"root"`) || strings.Contains(users, "pass") {
		t.Errorf("users: %s", users)
	}
	tokens := get("/api/v1/tokens")
	if !strings.Contains(tokens, `"ci"`) || !strings.Contains(tokens, `"operator"`) {
		t.Errorf("tokens: %s", tokens)
	}
	if strings.Contains(tokens, secret) {
		t.Errorf("the token value must never be listed: %s", tokens)
	}
}
