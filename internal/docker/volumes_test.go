package docker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalVolumesSkipsAnonymousVolumes(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.51")
		case r.Method == http.MethodGet && r.URL.Path == "/v1.51/volumes":
			_, _ = io.WriteString(w, `{"Volumes":[
				{"Name":"panel_panel-data","Driver":"local"},
				{"Name":"619d2561273cde7f9a15a5c0c4e36fc3ef1df48a080f12cf03f3c50de53215d7","Driver":"local","Labels":{"com.docker.volume.anonymous":""}}
			]}`)
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
	dk, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()

	got, err := dk.LocalVolumes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "panel_panel-data" {
		t.Fatalf("expected only named volumes, got %+v", got)
	}
}
