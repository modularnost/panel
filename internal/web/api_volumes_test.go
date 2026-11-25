package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

func TestNodeVolumePathBindsNodeForInspectCreateAndDelete(t *testing.T) {
	const nodeID = "node1abc"
	const volName = "unused-vol"
	present := false

	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/info":
			_, _ = io.WriteString(w, `{"Swarm":{"NodeID":"`+nodeID+`"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/nodes":
			_, _ = io.WriteString(w, `[{"ID":"`+nodeID+`","Description":{"Hostname":"mgr"},"Spec":{"Role":"manager","Availability":"active"},"Status":{"State":"ready"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/services":
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/volumes/"+volName:
			if !present {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"no such volume"}`)
				return
			}
			_, _ = io.WriteString(w, `{"Name":"`+volName+`","Driver":"local"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.51/volumes/create":
			present = true
			_, _ = io.WriteString(w, `{"Name":"`+volName+`","Driver":"local"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.51/volumes/"+volName:
			present = false
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
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

	db, err := store.Open(t.TempDir() + "/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	admin, err := db.CreateUser("admin", "admin-password", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := db.CreateToken(admin.ID, "test-admin", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{DB: db, Docker: dk}).Routes()

	callAPI := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	if w := callAPI(http.MethodPut, "/api/v1/nodes/"+nodeID+"/volumes/"+volName); w.Code != http.StatusOK {
		t.Fatalf("PUT volume got %d: %s", w.Code, w.Body.String())
	}
	if w := callAPI(http.MethodGet, "/api/v1/nodes/"+nodeID+"/volumes/"+volName); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"node":"`+nodeID+`"`) {
		t.Fatalf("GET volume got %d: %s", w.Code, w.Body.String())
	}
	if w := callAPI(http.MethodDelete, "/api/v1/nodes/"+nodeID+"/volumes/"+volName); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE volume got %d: %s", w.Code, w.Body.String())
	}

	specResp := callAPI(http.MethodGet, "/api/v1/openapi.json")
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(specResp.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"get", "put", "delete"} {
		params := spec.Paths["/api/v1/nodes/{node}/volumes/{name}"][method].Parameters
		if len(params) != 2 || params[0].Name != "node" && params[1].Name != "node" {
			t.Fatalf("%s /api/v1/nodes/{node}/volumes/{name} missing node parameter: %+v", method, params)
		}
	}
}

func TestListServiceVolumesReturnsMounts(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/services/svc1":
			_, _ = io.WriteString(w, `{"ID":"svc1","Spec":{"Name":"demo-api","TaskTemplate":{"Placement":{"Constraints":["node.id==node1abc"]},"ContainerSpec":{"Image":"alpine:3.22","Mounts":[{"Type":"volume","Source":"demo-data","Target":"/data","ReadOnly":true}]}}}}`)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
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

	db, err := store.Open(t.TempDir() + "/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	admin, err := db.CreateUser("admin", "admin-password", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := db.CreateToken(admin.ID, "test-admin", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{DB: db, Docker: dk}).Routes()

	r := httptest.NewRequest(http.MethodGet, "/api/v1/services/svc1/volumes", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"target":"/data"`) || !strings.Contains(w.Body.String(), `"read_only":true`) {
		t.Fatalf("GET service volumes got %d: %s", w.Code, w.Body.String())
	}
}
