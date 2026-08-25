// A demo panel module. One file: its own HTTP server, a manifest, and one
// action that asks the core to redeploy a service.
//
// The module has NO access to docker.sock. Everything it needs from Swarm it
// asks the core for at PANEL_CORE_URL, identifying itself with the
// X-Panel-Module / X-Panel-Token headers the core sends with every request.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

// docHead makes every response a real document. A module is rendered inside a
// frame in the panel, and a page without a doctype lands in quirks mode, where
// the panel cannot measure its height correctly.
//
// Linking the panel's own stylesheet is optional but free: same origin, and the
// module then matches the surrounding UI including its dark theme.
const docHead = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>hello-module</title>
<link rel="stylesheet" href="/static/panel.css">
<style>body{margin:1rem}</style></head><body>
`

const manifest = `{
  "name": "hello",
  "version": "0.1.0",
  "nav_entries": [{"title": "Hello", "path": "/"}],
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		names, err := services()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, docHead)
		if err != nil {
			fmt.Fprintf(w, "<p>core unreachable: %v</p>", err)
			return
		}
		io.WriteString(w, "<h1>hello-module</h1><p>Services the core can see:</p><ul>")
		for _, n := range names {
			fmt.Fprintf(w, `<li>%s <form method=post action="/modules/hello/action/redeploy">`+
				`<input type=hidden name=service value="%s"><button>redeploy</button></form></li>`, n, n)
		}
		io.WriteString(w, "</ul>")
	})

	http.HandleFunc("POST /action/redeploy", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		body, err := call(http.MethodPost, "/internal/services/"+r.FormValue("service")+"/update")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, docHead)
		fmt.Fprintf(w, "<p>done: %s</p><a href='/modules/hello/'>back</a>", body)
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
