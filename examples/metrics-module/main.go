// An example module, not a feature: the panel already shows these numbers. It
// demonstrates a ui page (a document of blocks the panel draws), the stats:read
// permission, a call to GET /internal/stats and telling the panel a page changed.
// It keeps ten minutes of history in memory, which is all a chart needs here;
// a module that keeps more owns its own storage (docs/modules.md).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
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
  "contract": 3,
  "name": "metrics",
  "version": "0.2.0",
  "contributions": [
    {"slot": "nav", "title": "Metrics", "path": "/", "role": "viewer"},
    {"slot": "service.tab", "title": "Metrics", "path": "/service", "role": "viewer"}
  ],
  "permissions": ["stats:read"]
}`

const (
	interval = 5 * time.Second
	keep     = 120 // samples: ten minutes at 5 s
)

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

// sample is one service at one moment, summed over its replicas.
type sample struct {
	T        int64
	CPU      float64
	Mem      uint64
	MemLimit uint64
	Replicas int
}

var (
	mu      sync.Mutex
	latest  []stat
	history = map[string][]sample{}
)

// record keeps the snapshot and appends one sample per service to its history.
func record(stats []stat, now time.Time) {
	mu.Lock()
	defer mu.Unlock()
	latest = stats
	sums := map[string]*sample{}
	for _, s := range stats {
		if s.Err != "" {
			continue
		}
		a := sums[s.Service]
		if a == nil {
			a = &sample{T: now.Unix()}
			sums[s.Service] = a
		}
		a.CPU += s.CPU
		a.Mem += s.Mem
		a.MemLimit += s.MemLimit
		a.Replicas++
	}
	for name, a := range sums {
		h := append(history[name], *a)
		if len(h) > keep {
			h = h[len(h)-keep:]
		}
		history[name] = h
	}
}

type block map[string]any

type document struct {
	UI     int     `json:"ui"`
	Blocks []block `json:"blocks"`
}

func overview(stats []stat) document {
	var cpu float64
	var mem uint64
	running := 0
	rows := []block{}
	for _, s := range stats {
		row := block{
			"node":    s.Node,
			"service": block{"text": s.Service, "href": block{"path": "/service?service=" + url.QueryEscape(s.Service)}},
			"slot":    s.Slot,
		}
		if s.Err != "" {
			// A node that cannot be reached is shown, not dropped: it would look idle.
			row["cpu"] = block{"text": s.Err, "tone": "bad"}
		} else {
			cpu, mem, running = cpu+s.CPU, mem+s.Mem, running+1
			row["cpu"], row["mem"] = s.CPU, s.Mem
		}
		rows = append(rows, row)
	}
	return document{UI: 1, Blocks: []block{
		{"type": "text", "tone": "muted", "text": "A live snapshot; a service's page keeps ten minutes of history."},
		{"type": "stat", "label": "Replicas", "value": running},
		{"type": "stat", "label": "CPU", "value": cpu, "unit": "percent"},
		{"type": "stat", "label": "Memory", "value": mem, "unit": "bytes"},
		{"type": "table", "empty": "Nothing sampled yet.", "rows": rows, "columns": []block{
			{"key": "node", "title": "Node"},
			{"key": "service", "title": "Service"},
			{"key": "slot", "title": "Replica", "type": "number"},
			{"key": "cpu", "title": "CPU", "type": "percent"},
			{"key": "mem", "title": "Memory", "type": "bytes"},
		}},
	}}
}

var ranges = []struct {
	Value string
	D     time.Duration
}{{"1m", time.Minute}, {"5m", 5 * time.Minute}, {"10m", 10 * time.Minute}}

func rangeOf(s string) (string, time.Duration) {
	for _, r := range ranges {
		if r.Value == s {
			return r.Value, r.D
		}
	}
	return "5m", 5 * time.Minute
}

func serviceDoc(service, rng string, h []sample, now time.Time) document {
	if service == "" {
		return document{UI: 1, Blocks: []block{{"type": "banner", "tone": "warning", "text": "Open a service to see its metrics."}}}
	}
	label, d := rangeOf(rng)
	from := now.Add(-d).Unix()
	options := make([]block, len(ranges))
	for i, r := range ranges {
		options[i] = block{"value": r.Value, "label": r.Value}
	}
	var cpu, mem [][2]float64
	var last *sample
	for i, s := range h {
		if s.T >= from {
			cpu = append(cpu, [2]float64{float64(s.T), s.CPU})
			mem = append(mem, [2]float64{float64(s.T), float64(s.Mem)})
			last = &h[i]
		}
	}
	chart := func(title, unit string, points [][2]float64) block {
		return block{"type": "chart", "title": title, "unit": unit, "sync": "time", "from": from, "to": now.Unix(),
			"empty": "No samples in this range.", "series": []block{{"name": title, "points": points}}}
	}
	blocks := []block{
		{"type": "choice", "param": "range", "value": label, "options": options},
		{"type": "heading", "text": service},
	}
	if last != nil {
		blocks = append(blocks, block{"type": "meter", "label": "CPU", "value": last.CPU, "max": max(100, last.CPU), "unit": "percent"})
		if last.MemLimit > 0 {
			blocks = append(blocks, block{"type": "meter", "label": "Memory", "value": last.Mem, "max": last.MemLimit, "unit": "bytes",
				"tone": tone(float64(last.Mem) > 0.8*float64(last.MemLimit))})
		}
	}
	return document{UI: 1, Blocks: append(blocks, chart("CPU", "percent", cpu), chart("Memory", "bytes", mem))}
}

func tone(bad bool) string {
	if bad {
		return "bad"
	}
	return "ok"
}

func main() {
	http.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		remember(r) // discovery carries the token, so this is where it arrives first
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, manifest)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		mu.Lock()
		doc := overview(latest)
		mu.Unlock()
		write(w, doc)
	})
	http.HandleFunc("/service", func(w http.ResponseWriter, r *http.Request) {
		remember(r)
		name := r.URL.Query().Get("service")
		mu.Lock()
		doc := serviceDoc(name, r.URL.Query().Get("range"), history[name], time.Now())
		mu.Unlock()
		write(w, doc)
	})

	go poll()
	addr := or(os.Getenv("ADDR"), ":8080")
	log.Printf("metrics-module listening on %s, core: %s", addr, core)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func write(w http.ResponseWriter, d document) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

// poll samples the core on a timer, nobody needs to be watching, and tells the
// panel that the pages changed so the browsers that have them open ask again.
func poll() {
	for range time.Tick(interval) {
		stats, err := fetchStats()
		if err != nil {
			log.Printf("sampling: %v", err)
			continue
		}
		record(stats, time.Now())
		for _, page := range []string{"/", "/service"} {
			if err := changed(page); err != nil {
				log.Printf("telling the panel about %s: %v", page, err)
			}
		}
	}
}

// remember stores the credentials the core arrived with; we answer with them.
func remember(r *http.Request) {
	if m := r.Header.Get("X-Panel-Module"); m != "" {
		creds.Store(&[2]string{m, r.Header.Get("X-Panel-Token")})
	}
}

func coreRequest(method, path string, body io.Reader) (*http.Response, error) {
	c := creds.Load()
	if c == nil {
		return nil, fmt.Errorf("the core has not called yet, so there is no token")
	}
	req, err := http.NewRequest(method, core+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Panel-Module", c[0])
	req.Header.Set("X-Panel-Token", c[1])
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func changed(page string) error {
	resp, err := coreRequest(http.MethodPost, "/internal/ui/changed", bytes.NewBufferString(fmt.Sprintf(`{"path":%q}`, page)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s", resp.Status)
	}
	return nil
}

func fetchStats() ([]stat, error) {
	resp, err := coreRequest(http.MethodGet, "/internal/stats", nil)
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
