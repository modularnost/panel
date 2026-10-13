package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

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
