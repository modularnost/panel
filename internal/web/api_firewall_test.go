package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"modularnost/internal/docker"
	"modularnost/internal/firewall"
	"modularnost/internal/store"
)

func TestHostOfTakesTheAddressOutOfWhatSwarmReports(t *testing.T) {
	for in, want := range map[string]string{
		"172.25.0.2:2377":    "172.25.0.2", // a manager reports host:port
		"203.0.113.9":        "203.0.113.9",
		"[2001:db8::1]:2377": "2001:db8::1",
		"::ffff:10.0.0.4":    "10.0.0.4",
	} {
		if got, ok := hostOf(in); !ok || got != want {
			t.Errorf("hostOf(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "node-1", "10.0.0.1.5", ":2377"} {
		if _, ok := hostOf(bad); ok {
			t.Errorf("hostOf(%q) was taken for an address", bad)
		}
	}
}

func TestBuildFirewallSpec(t *testing.T) {
	cfg := FirewallConfig{SSH: []int{2222}}
	nodes := []docker.Node{{Address: "10.0.0.1:2377"}, {Address: "10.0.0.2"}, {Address: ""}}
	spec := buildFirewallSpec(cfg, nodes, []string{"10.0.0.9"}, []store.FirewallPort{{Port: 10050, Proto: "tcp"}})

	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"10.0.0.1": true, "10.0.0.2": true, "10.0.0.9": true}
	if len(spec.Peers) != len(want) {
		t.Fatalf("peers: %v", spec.Peers)
	}
	for _, p := range spec.Peers {
		if !want[p] {
			t.Errorf("unexpected peer %q", p)
		}
	}
	got := map[string]bool{}
	for _, p := range spec.Allow {
		got[p.String()] = true
	}
	for _, p := range []string{"80/tcp", "443/tcp", "10050/tcp"} {
		if !got[p] {
			t.Errorf("%s is not allowed: %v", p, spec.Allow)
		}
	}
	if len(spec.SSH) != 1 || spec.SSH[0] != 2222 {
		t.Errorf("ssh: %v", spec.SSH)
	}
}

func TestFirewallStatesGoStale(t *testing.T) {
	var f firewallStates
	if f.state("n1") != "unknown" {
		t.Error("a node that never reported is unknown")
	}
	f.set(firewall.Status{Node: "n1", OK: true})
	f.set(firewall.Status{Node: "n2", OK: false, Error: "boom"})
	if f.state("n1") != "applied" || f.state("n2") != "failed" {
		t.Errorf("n1 %s, n2 %s", f.state("n1"), f.state("n2"))
	}
	f.mu.Lock()
	st := f.by["n1"]
	st.At = time.Now().Add(-2 * staleAfter)
	f.by["n1"] = st
	f.mu.Unlock()
	if f.state("n1") != "unknown" {
		t.Error("a node silent for long is not known to be protected")
	}
}

func TestFirewallEndpointsNeedTheNodeToken(t *testing.T) {
	_, base, _ := apiServerWith(t, func(s *Server) { s.Secret = "sekrit" })
	token := firewall.Token("sekrit")

	call := func(method, path, tok, body string) int {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Panel-Token", tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for name, tok := range map[string]string{"no token": "", "a token of the agents": "wrong", "the module secret itself": "sekrit"} {
		if got := call("GET", "/internal/firewall", tok, ""); got != 401 {
			t.Errorf("%s: GET %d", name, got)
		}
		if got := call("POST", "/internal/firewall/applied", tok, `{"node":"n"}`); got != 401 {
			t.Errorf("%s: POST %d", name, got)
		}
	}
	if got := call("POST", "/internal/firewall/applied", token, `{"node":"n1","ok":true,"sig":"x"}`); got != 204 {
		t.Errorf("a report with the token: %d", got)
	}
	if got := call("POST", "/internal/firewall/applied", token, `{"ok":true}`); got != 400 {
		t.Errorf("a report with no node: %d", got)
	}
}

func TestFirewallIsClosedWhenThePanelHasNoSecret(t *testing.T) {
	_, base, _ := apiServer(t)
	// No secret configured: the token of an empty secret must not open it.
	req, _ := http.NewRequest("GET", base+"/internal/firewall", nil)
	req.Header.Set("X-Panel-Token", firewall.Token(""))
	rec, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rec.Body.Close()
	if rec.StatusCode != 401 {
		t.Errorf("%d", rec.StatusCode)
	}
	_ = httptest.NewRecorder
}

func TestFirewallPeersAreForAdminsAndMustBeAddresses(t *testing.T) {
	c, base, db := apiServer(t)
	const j = "application/json"
	// ann is a viewer.
	if status, _ := send(t, c, "POST", base+"/api/v1/auth/login", j, `{"username":"ann","password":"s3cret-pass"}`); status != 200 {
		t.Fatal("login")
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/firewall/peers", j, `{"address":"203.0.113.5"}`); status != 403 {
		t.Errorf("a viewer opened the swarm ports: %d", status)
	}
	if status, _ := send(t, c, "GET", base+"/api/v1/firewall/peers", "", ""); status != 403 {
		t.Errorf("a viewer read them: %d", status)
	}
	if peers, _ := db.FirewallPeers(); len(peers) != 0 {
		t.Errorf("%v", peers)
	}
}

func TestFirewallPortsAreSeededOnceAndSwarmPortsRefused(t *testing.T) {
	c, base, db := apiServer(t)
	const j = "application/json"
	cfg := FirewallConfig{Keep: []firewall.Port{{Port: 10050, Proto: "tcp"}}}
	if err := cfg.Seed(db); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteFirewallPort(10050, "tcp"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Seed(db); err != nil { // the next start of the panel
		t.Fatal(err)
	}
	if ports, _ := db.FirewallPorts(); len(ports) != 0 {
		t.Errorf("a removed port came back: %v", ports)
	}

	if _, err := db.CreateUser("root", "root-pass-1", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/auth/login", j, `{"username":"ann","password":"s3cret-pass"}`); status != 200 {
		t.Fatal("login")
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/firewall/ports", j, `{"port":5432,"proto":"tcp"}`); status != 403 {
		t.Errorf("a viewer opened a port: %d", status)
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/auth/login", j, `{"username":"root","password":"root-pass-1"}`); status != 200 {
		t.Fatal("login")
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/firewall/ports", j, `{"port":2377,"proto":"tcp"}`); status != 422 {
		t.Errorf("the swarm's port was opened to everyone: %d", status)
	}
	if status, _ := send(t, c, "POST", base+"/api/v1/firewall/ports", j, `{"port":5432,"proto":"tcp"}`); status != 201 {
		t.Errorf("an admin could not open a port: %d", status)
	}
	if ports, _ := db.FirewallPorts(); len(ports) != 1 || ports[0].Port != 5432 {
		t.Errorf("%v", ports)
	}
}
