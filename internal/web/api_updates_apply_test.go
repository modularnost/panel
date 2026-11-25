package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"modularnost/internal/docker"
)

func TestVerifyUpdateImages(t *testing.T) {
	const panelRepo = "ghcr.io/modularnost/panel"
	const otherRepo = "ghcr.io/modularnost/agent"
	panelDigest := "sha256:" + strings.Repeat("a", 64)
	agentDigest := "sha256:" + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, agentImage, wantAgentTag string
		wantAgentReads                 int32
	}{
		{"same repository", panelRepo + ":0.8.0", "0.10.0@" + panelDigest, 0},
		{"different repository", otherRepo + ":0.9.0", "0.10.0@" + agentDigest, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var panelReads, agentReads atomic.Int32
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/_ping":
					w.Header().Set("API-Version", "1.51")
				case "/v1.51/distribution/" + panelRepo + ":0.10.0/json":
					if panelReads.Add(1) > 1 {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `{"message":"net/http: TLS handshake timeout"}`)
						return
					}
					_, _ = fmt.Fprintf(w, `{"Descriptor":{"digest":%q}}`, panelDigest)
				case "/v1.51/distribution/" + otherRepo + ":0.10.0/json":
					agentReads.Add(1)
					_, _ = fmt.Fprintf(w, `{"Descriptor":{"digest":%q}}`, agentDigest)
				default:
					t.Errorf("unexpected Docker request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(daemon.Close)
			t.Setenv("DOCKER_HOST", daemon.URL)
			t.Setenv("DOCKER_API_VERSION", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			client, err := docker.New()
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			server := Server{Docker: client}
			panelTag, agentTag, err := server.verifyUpdateImages(t.Context(), "0.10.0", panelRepo+":0.9.0", tc.agentImage)
			if err != nil || panelTag != "0.10.0@"+panelDigest || agentTag != tc.wantAgentTag {
				t.Fatalf("panel tag %q, agent tag %q, error %v", panelTag, agentTag, err)
			}
			if panelReads.Load() != 1 || agentReads.Load() != tc.wantAgentReads {
				t.Fatalf("registry reads: panel %d, agent %d", panelReads.Load(), agentReads.Load())
			}
		})
	}
}
