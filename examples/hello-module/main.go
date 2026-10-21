// A demo panel module: its own HTTP server, a manifest, one ui page with a form
// and one action that asks the core (PANEL_CORE_URL) to redeploy a service. It
// has no docker.sock; it identifies itself with the X-Panel-Module /
// X-Panel-Token headers. The other example, metrics-module, shows charts.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
)

var (
	core  = cmp(os.Getenv("PANEL_CORE_URL"), "http://panel:8080")
	creds atomic.Pointer[[2]string] // [module, token] from the core's latest request
)

func cmp(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

const manifest = `{
  "contract": 3,
  "name": "hello",
  "version": "0.2.0",
  "contributions": [{"slot": "nav", "title": "Hello", "path": "/", "role": "operator"}],
  "permissions": ["service:read", "service:update"]
}`

func main() {
	http.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		remember(r) // discovery carries the token, so this is where it arrives first
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, manifest)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		names, err := services()
		blocks := []any{map[string]any{"type": "heading", "level": 1, "text": "hello-module"}}
		if err != nil {
			blocks = append(blocks, map[string]any{"type": "banner", "tone": "error", "text": "core unreachable: " + err.Error()})
		} else {
			options := make([]map[string]string, 0, len(names))
			for _, n := range names {
				options = append(options, map[string]string{"value": n})
			}
			blocks = append(blocks,
				map[string]any{"type": "text", "text": "Services the core can see:"},
				map[string]any{
					"type": "form", "action": "/action/redeploy", "submit": "Redeploy",
					"confirm": "Redeploy this service?",
					"fields":  []any{map[string]any{"name": "service", "label": "Service", "type": "select", "options": options}},
				})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ui": 1, "blocks": blocks})
	})

	http.HandleFunc("POST /action/redeploy", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		var values map[string]string
		if err := json.NewDecoder(r.Body).Decode(&values); err != nil || values["service"] == "" {
			http.Error(w, `send {"service": "<name>"}`, http.StatusBadRequest)
			return
		}
		service := values["service"]
		if _, err := call(http.MethodPost, "/internal/services/"+url.PathEscape(service)+"/update"); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "redeployed " + service})
	})

	addr := cmp(os.Getenv("ADDR"), ":8080")
	log.Printf("hello-module listening on %s, core: %s", addr, core)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// remember stores the credentials the core arrived with; we answer with them.
func remember(r *http.Request) {
	if m := r.Header.Get("X-Panel-Module"); m != "" {
		creds.Store(&[2]string{m, r.Header.Get("X-Panel-Token")})
	}
}

func call(method, path string) (string, error) {
	c := creds.Load()
	if c == nil {
		return "", fmt.Errorf("the core has not called yet, so there is no token")
	}
	req, err := http.NewRequest(method, core+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Panel-Module", c[0])
	req.Header.Set("X-Panel-Token", c[1])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("core: %s: %s", resp.Status, body)
	}
	return string(body), nil
}

func services() ([]string, error) {
	body, err := call(http.MethodGet, "/internal/services")
	if err != nil {
		return nil, err
	}
	var svcs []struct{ Name string }
	if err := json.Unmarshal([]byte(body), &svcs); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(svcs))
	for _, s := range svcs {
		names = append(names, s.Name)
	}
	return names, nil
}
