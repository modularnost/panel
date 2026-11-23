package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"modularnost/internal/modules"
	"modularnost/internal/store"
)

func TestModuleCatalogIsAdminOnlyAndStaleInstallsAreRefused(t *testing.T) {
	c, base, db := apiServerWith(t, func(s *Server) {
		if err := s.DB.SaveModuleCatalog(t.Context(), []byte(`{"schema_version":1,"modules":[]}`), time.Now()); err != nil {
			t.Fatal(err)
		}
		s.Catalog = modules.NewCatalog(t.Context(), s.DB)
	})
	catalog := base + "/api/v1/module-catalog"
	if status, _ := send(t, c, "GET", catalog, "", ""); status != http.StatusUnauthorized {
		t.Fatalf("signed-out access returned %d", status)
	}
	signIn(t, c, base)
	if status, _ := send(t, c, "GET", catalog, "", ""); status != http.StatusForbidden {
		t.Fatalf("viewer access returned %d", status)
	}
	if _, err := db.CreateUser("root", "catalog-admin-key", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if status, body := send(t, c, "POST", base+"/api/v1/auth/login", "application/json",
		`{"username":"root","password":"catalog-admin-key"}`); status != http.StatusOK {
		t.Fatalf("admin login: %d %v", status, body)
	}
	if status, body := send(t, c, "GET", catalog, "", ""); status != http.StatusOK || body["state"] != "stale" {
		t.Fatalf("cached catalog: %d %v", status, body)
	}
	if status, body := send(t, c, "POST", catalog+"/metrics/install", "application/json",
		`{"version":"0.4.0","revision":"`+strings.Repeat("a", 64)+`"}`); status != http.StatusConflict || errorCode(body) != "catalog_stale" {
		t.Fatalf("stale install: %d %v", status, body)
	}
}
