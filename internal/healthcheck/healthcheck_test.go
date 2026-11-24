package healthcheck

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheckModes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mode, addr string
		ok               bool
	}{
		{"panel on a custom port", "", ":" + port, true},
		{"IPv4 wildcard", "", "0.0.0.0:" + port, true},
		{"agent uses its own endpoint", "agent", ":" + port, false},
		{"firewall has no HTTP listener", "firewall", "invalid", true},
		{"clear has no HTTP listener", "firewall-clear", "invalid", true},
		{"invalid panel address", "", "invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Check(tc.mode, tc.addr); (err == nil) != tc.ok {
				t.Fatalf("Check: %v, want success %v", err, tc.ok)
			}
		})
	}
}
