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
	"modularnost/internal/store"
)

func apiClient(t *testing.T) (*http.Client, string) {
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
	return &http.Client{Jar: jar}, srv.URL
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
