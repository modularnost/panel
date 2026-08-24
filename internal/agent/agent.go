// Package agent collects container stats on nodes the panel itself does not run
// on.
//
// Docker has no cross-node stats API: a manager's socket only sees its own
// containers. So the same binary runs as a global Swarm service in agent mode,
// each replica reporting its own node, and the panel merges the answers.
//
// The agent is the panel's own code, not a module: docker.sock stays with us
// (ADR 3). It only ever reads.
package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"sync"
	"time"

	"modularnost/internal/docker"
)

// Token is what the panel presents to an agent, and what the agent checks.
func Token(secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("agent"))
	return hex.EncodeToString(mac.Sum(nil))
}

// Serve runs the agent side: one endpoint, stats for this node only.
func Serve(addr, secret, node string, dk *docker.Client) error {
	want := Token(secret)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.Header.Get("X-Panel-Token")), []byte(want)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		stats, err := dk.Stats(r.Context(), r.FormValue("service"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for i := range stats {
			stats[i].Node = node
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	return http.ListenAndServe(addr, mux)
}

// Collector gathers stats from every agent, falling back to the local socket
// when no agents are deployed (a single-node cluster does not need them).
type Collector struct {
	service string // swarm service name of the agent, e.g. "panel_agent"
	port    string
	secret  string
	local   *docker.Client
	http    *http.Client
}

func NewCollector(service, port, secret string, local *docker.Client) *Collector {
	return &Collector{
		service: service, port: port, secret: secret, local: local,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Stats returns stats for the whole cluster, or for one service when named.
func (c *Collector) Stats(ctx context.Context, service string) ([]docker.TaskStats, error) {
	addrs := c.agents(ctx)
	if len(addrs) == 0 {
		// No agents: this is the only node we can see anyway.
		stats, err := c.local.Stats(ctx, service)
		if err != nil {
			return nil, err
		}
		node, _ := c.local.NodeName(ctx)
		for i := range stats {
			stats[i].Node = node
		}
		return stats, nil
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []docker.TaskStats
	)
	for _, addr := range addrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			stats, err := c.fetch(ctx, addr, service)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// A node that cannot be reached is worth showing as a row, not
				// worth failing the whole page for.
				out = append(out, docker.TaskStats{Node: addr, Err: err.Error()})
				return
			}
			out = append(out, stats...)
		}(addr)
	}
	wg.Wait()
	docker.SortStats(out)
	return out, nil
}

// agents resolves the agent service to one address per running task. Swarm
// publishes those under tasks.<service>, unlike the load-balanced service name.
func (c *Collector) agents(ctx context.Context) []string {
	if c.service == "" {
		return nil
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, "tasks."+c.service)
	if err != nil {
		return nil
	}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.JoinHostPort(ip, c.port))
	}
	return addrs
}

func (c *Collector) fetch(ctx context.Context, addr, service string) ([]docker.TaskStats, error) {
	url := "http://" + addr + "/stats"
	if service != "" {
		url += "?service=" + neturl.QueryEscape(service)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Panel-Token", Token(c.secret))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent: %s", resp.Status)
	}
	var stats []docker.TaskStats
	return stats, json.NewDecoder(resp.Body).Decode(&stats)
}
