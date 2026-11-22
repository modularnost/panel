package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/api/pkg/authconfig"

	"modularnost/internal/store"
)

func TestRegistryCredentialsAreWriteOnlyAndScoped(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	encryption, err := store.RegistryCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, RegistryEncryption: encryption}
	handler := s.Routes()
	admin, err := db.CreateUser("admin", "admin-password", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := db.CreateToken(admin.ID, "test-admin", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := db.CreateToken(admin.ID, "test-viewer", store.RoleViewer, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/registries"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "secret-pull-token") {
			t.Fatal("registry token was returned in the response")
		}
		return w
	}
	const host = "registry.example.net:5000"
	const credentials = `{"username":"reader","token":"secret-pull-token"}`
	if w := request(http.MethodPut, "/"+host, viewerToken, credentials); w.Code != http.StatusForbidden {
		t.Fatalf("viewer save got %d", w.Code)
	}
	if w := request(http.MethodPut, "/"+host, adminToken, credentials); w.Code != http.StatusNoContent {
		t.Fatalf("admin save got %d: %s", w.Code, w.Body)
	}
	if w := request(http.MethodGet, "", adminToken, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), host) {
		t.Fatalf("registry missing from list: %s", w.Body)
	}
	var raw []byte
	if err := db.QueryRow(`SELECT encrypted_token FROM registries WHERE host=?`, host).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-pull-token")) {
		t.Fatal("registry token was stored in plaintext")
	}
	if err := db.SaveStack("test", "services: {}", "admin", host); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{"com.docker.stack.namespace": "test"}
	encoded, err := s.serviceRegistryAuth(host+"/app:1", labels)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := authconfig.Decode(encoded)
	if err != nil || auth.Password != "secret-pull-token" || auth.ServerAddress != host {
		t.Fatal("expected saved credentials for the matching image")
	}
	if encoded, err := s.serviceRegistryAuth("registry.example.net/app:1", labels); err != nil || encoded != "" {
		t.Fatal("different registry received credentials")
	}
	labels[labelOwner] = "module"
	if encoded, err := s.serviceRegistryAuth(host+"/app:1", labels); err != nil || encoded != "" {
		t.Fatal("module-created service received credentials")
	}
	delete(labels, labelOwner)
	if w := request(http.MethodDelete, "/"+host, adminToken, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete got %d", w.Code)
	}
	if _, err := s.serviceRegistryAuth(host+"/app:1", labels); err == nil {
		t.Fatal("redeploy should report missing credentials after deletion")
	}
}
