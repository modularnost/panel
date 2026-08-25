// An EXAMPLE module, not a feature: the panel shows the same numbers on its own
// Resources page. This exists to demonstrate the module contract end to end —
// a manifest, a permission (stats:read), and a call back into the core — and to
// be copied as a skeleton.
//
// The module has no docker.sock access of its own. It gets the numbers from the
// core via GET /internal/stats, identifying itself with the headers the core
// sent it in the proxied request.
//
// A metrics module worth installing would do what the core deliberately does
// not: keep history, draw graphs, alert on thresholds. This one keeps nothing.
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"sync/atomic"
)

var (
	core  = or(os.Getenv("PANEL_CORE_URL"), "http://panel:8080")
	creds atomic.Pointer[[2]string] // [module, token] from the core's latest request
)

func or(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

const manifest = `{
  "name": "metrics",
  "version": "0.1.0",
  "nav_entries": [{"title": "Metrics", "path": "/"}],
  "permissions": ["stats:read"]
}`

type stat struct {
	Node     string  `json:"node"`
	Service  string  `json:"service"`
	Stack    string  `json:"stack"`
	Slot     int     `json:"slot"`
	CPU      float64 `json:"cpu"`
	Mem      uint64  `json:"mem"`
	MemLimit uint64  `json:"mem_limit"`
	Err      string  `json:"err"`
}

// CPUBar is the bar width in percent, clamped to 100.
func (s stat) CPUBar() int { return clamp(s.CPU) }

func (s stat) MemBar() int {
	if s.MemLimit == 0 {
		return 0
	}
	return clamp(float64(s.Mem) / float64(s.MemLimit) * 100)
}

func clamp(v float64) int {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return int(v)
	}
}

func human(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

var page = template.Must(template.New("page").Funcs(template.FuncMap{"human": human}).Parse(`
<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Metrics</title>
<!-- The panel's stylesheet: same origin, so the module matches the surrounding
     UI and its theme. Only the bars below are the module's own. -->
<link rel="stylesheet" href="/static/panel.css">
<style>
body { margin: 1rem; }
.bar { height: .7rem; background: #8882; border-radius: 3px; overflow: hidden; min-width: 6rem; }
.bar > i { display: block; height: 100%; background: var(--accent, #4a90d9); }
.hot > i { background: var(--bad, #d33); }
.num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
</style></head><body>
<h1>Metrics</h1>
<p><small>Live snapshot, refreshed every 5 s. No history &mdash; that is Grafana's job.</small></p>
<table>
<tr><th>Node</th><th>Service</th><th>Replica</th><th class=num>CPU</th><th></th><th class=num>Memory</th><th></th></tr>
{{range .}}
<tr>
  <td class=muted>{{.Node}}</td>
  <td>{{.Service}}</td>
  <td>{{.Slot}}</td>
  {{if .Err}}<td colspan=4 class=bad>{{.Err}}</td>{{else}}
  <td class=num>{{printf "%.1f" .CPU}} %</td>
  <td><div class="bar {{if gt .CPUBar 80}}hot{{end}}"><i style="width:{{.CPUBar}}%"></i></div></td>
  <td class=num>{{human .Mem}}{{if .MemLimit}} / {{human .MemLimit}}{{end}}</td>
  <td>{{if .MemLimit}}<div class="bar {{if gt .MemBar 80}}hot{{end}}"><i style="width:{{.MemBar}}%"></i></div>{{end}}</td>
  {{end}}
</tr>
{{else}}
<tr><td colspan=7>No running replicas in sight.</td></tr>
{{end}}
</table>
<script>setTimeout(() => location.reload(), 5000);</script>
`))

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
		stats, err := fetchStats()
		if err != nil {
			http.Error(w, "the core did not return metrics: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page.Execute(w, stats)
	})

	addr := or(os.Getenv("ADDR"), ":8080")
	log.Printf("metrics-module listening on %s, core: %s", addr, core)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// remember stores the credentials the core arrived with; we answer with them.
func remember(r *http.Request) {
	if m := r.Header.Get("X-Panel-Module"); m != "" {
		creds.Store(&[2]string{m, r.Header.Get("X-Panel-Token")})
	}
}

func fetchStats() ([]stat, error) {
	c := creds.Load()
	if c == nil {
		return nil, fmt.Errorf("the core has not called yet, so there is no token")
	}
	req, err := http.NewRequest(http.MethodGet, core+"/internal/stats", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Panel-Module", c[0])
	req.Header.Set("X-Panel-Token", c[1])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, body)
	}
	var stats []stat
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}
	// Hungriest first: that is what people open this page for.
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].CPU > stats[j].CPU })
	return stats, nil
}
