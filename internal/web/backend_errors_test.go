package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
)

func TestBackendErrorsDoNotExposeDetails(t *testing.T) {
	const detail = "private-daemon-details"
	const jsonType = "application/json"
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.48")
			return
		}
		w.Header().Set("Content-Type", jsonType)
		http.Error(w, `{"message":"`+detail+`"}`, http.StatusInternalServerError)
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
	c, base, db := apiServerWith(t, func(s *Server) { s.Docker = dk })

	signIn(t, c, base)
	status, body := send(t, c, "GET", base+"/api/v1/services", "", "")
	if status != 502 || errorCode(body) != "docker_unavailable" {
		t.Fatalf("services: %d %v", status, body)
	}
	if body["error"].(map[string]any)["message"] != "Docker is unavailable" {
		t.Fatal("services must return a generic Docker error")
	}

	req := httptest.NewRequest("GET", "/stats", nil)
	req.Header.Set("X-Panel-Token", agent.Token("agent-test"))
	rec := httptest.NewRecorder()
	agent.Routes("agent-test", "worker", dk).ServeHTTP(rec, req)
	if rec.Code != 502 || strings.TrimSpace(rec.Body.String()) != "could not load metrics" {
		t.Fatalf("agent stats: %d %s", rec.Code, rec.Body.String())
	}

	if _, err := db.Exec("DROP TABLE api_tokens"); err != nil {
		t.Fatal(err)
	}
	status, body = send(t, c, "POST", base+"/api/v1/auth/login", jsonType, `{"username":"ann","password":"s3cret-pass"}`)
	if status != 500 || errorCode(body) != "internal" || body["error"].(map[string]any)["message"] != "internal server error" {
		t.Fatalf("login database failure: %d %v", status, body)
	}
}
