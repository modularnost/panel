package web

import (
	"cmp"
	"context"
	"crypto/hmac"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/firewall"
	"modularnost/internal/store"
)

// FirewallConfig is what the installer told the panel about the host the
// cluster started on: the SSH ports to keep, and the listeners that were there
// before the install and stay open (copied to the database once, see Seed).
type FirewallConfig struct {
	SSH  []int
	Keep []firewall.Port
}

// peerLife is how long the address of a server about to join is allowed when
// it never shows up in the swarm.
const peerLife = 24 * time.Hour

// staleAfter is how long a node may be silent before the panel stops claiming
// that its firewall is in force; the services ask every 30 seconds.
const staleAfter = 3 * time.Minute

type firewallStates struct {
	mu sync.Mutex
	by map[string]firewallState
}

type firewallState struct {
	firewall.Status
	At time.Time
}

func (f *firewallStates) set(st firewall.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.by == nil {
		f.by = map[string]firewallState{}
	}
	f.by[st.Node] = firewallState{st, time.Now()}
}

// state is "applied", "failed" or "unknown": a node that has not reported
// lately is not known to be protected, whatever it reported last.
func (f *firewallStates) state(node string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.by[node]
	switch {
	case !ok || time.Since(st.At) > staleAfter:
		return "unknown"
	case st.OK:
		return "applied"
	default:
		return "failed"
	}
}

// apiNodes is toAPINodes with what each node's firewall says about itself.
func (s *Server) apiNodes(in []docker.Node) []apiNode {
	out := toAPINodes(in)
	for i := range out {
		out[i].Firewall = s.fw.state(out[i].Hostname)
	}
	return out
}

func (s *Server) firewallAuthorized(r *http.Request) bool {
	if s.Secret == "" {
		return false
	}
	return hmac.Equal([]byte(r.Header.Get("X-Panel-Token")), []byte(firewall.Token(s.Secret)))
}

// hostOf is the address in "10.0.0.2:2377" or "10.0.0.2".
func hostOf(addr string) (string, bool) {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return "", false
	}
	return a.Unmap().String(), true
}

// firewallSpec is what a node's firewall service asks for: SSH and what is
// allowed come from the installer, the peers from Swarm itself and from the
// servers that were allowed to join.
func (s *Server) firewallSpec(w http.ResponseWriter, r *http.Request) {
	if !s.firewallAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	spec, err := s.currentFirewallSpec(r.Context())
	if err != nil {
		// The node keeps what it has: an error is not an empty list.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, spec)
}

func (s *Server) currentFirewallSpec(ctx context.Context) (firewall.Spec, error) {
	nodes, err := s.Docker.Nodes(ctx)
	if err != nil {
		return firewall.Spec{}, err
	}
	known := map[string]bool{}
	for _, n := range nodes {
		if h, ok := hostOf(n.Address); ok {
			known[h] = true
		}
	}
	if err := s.DB.PruneFirewallPeers(peerLife, known); err != nil {
		return firewall.Spec{}, err
	}
	pending, err := s.DB.FirewallPeers()
	if err != nil {
		return firewall.Spec{}, err
	}
	var addrs []string
	for _, p := range pending {
		addrs = append(addrs, p.Address)
	}
	ports, err := s.DB.FirewallPorts()
	if err != nil {
		return firewall.Spec{}, err
	}
	return buildFirewallSpec(s.Firewall, nodes, addrs, ports), nil
}

// buildFirewallSpec: the panel and the web are always allowed, so are the ports
// of the Ports page, and the swarm's ports are for the nodes Swarm knows and the
// servers allowed to join.
func buildFirewallSpec(cfg FirewallConfig, nodes []docker.Node, pending []string, ports []store.FirewallPort) firewall.Spec {
	var peers []string
	for _, n := range nodes {
		if h, ok := hostOf(n.Address); ok {
			peers = append(peers, h)
		}
	}
	peers = append(peers, pending...)
	allow := []firewall.Port{{Port: 80, Proto: "tcp"}, {Port: 443, Proto: "tcp"}}
	for _, p := range ports {
		allow = append(allow, firewall.Port{Port: p.Port, Proto: p.Proto})
	}
	return firewall.Spec{SSH: cfg.SSH, Allow: allow, Peers: peers}
}
func (s *Server) firewallApplied(w http.ResponseWriter, r *http.Request) {
	if !s.firewallAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var st firewall.Status
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&st); err != nil || st.Node == "" || len(st.Node) > 253 {
		http.Error(w, "send a status", http.StatusBadRequest)
		return
	}
	if len(st.Error) > 1000 {
		st.Error = st.Error[:1000]
	}
	s.fw.set(st)
	w.WriteHeader(http.StatusNoContent)
}

type apiFirewallPeer struct {
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

type apiFirewallPeerAddress struct {
	Address string `json:"address" minLength:"2" maxLength:"45" doc:"The public address of the server that is going to join"`
}

type firewallPeerInput struct{ Body apiFirewallPeerAddress }

type firewallPeersOutput struct {
	Body []apiFirewallPeer `nullable:"false"`
}

func (s *Server) registerFirewall(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	s.registerFirewallPorts(api, admin)

	huma.Register(api, huma.Operation{
		OperationID: "list-firewall-peers", Method: http.MethodGet, Path: "/api/v1/firewall/peers",
		Summary: "Servers allowed to join that are not in the swarm yet", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*firewallPeersOutput, error) {
		peers, err := s.DB.FirewallPeers()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		out := make([]apiFirewallPeer, 0, len(peers))
		for _, p := range peers {
			out = append(out, apiFirewallPeer{p.Address, p.CreatedAt})
		}
		return &firewallPeersOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "allow-firewall-peer", Method: http.MethodPost, Path: "/api/v1/firewall/peers",
		Summary:       "Open the swarm's ports to a server that is going to join, for a day or until it has",
		Middlewares:   admin,
		Security:      signedIn,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *firewallPeerInput) (*struct{ Body apiFirewallPeer }, error) {
		a, err := netip.ParseAddr(in.Body.Address)
		if err != nil {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "that is not an IP address")
		}
		a = a.Unmap()
		if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "that address cannot be another server")
		}
		if err := s.DB.AddFirewallPeer(a.String()); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &struct{ Body apiFirewallPeer }{apiFirewallPeer{a.String(), time.Now()}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revoke-firewall-peer", Method: http.MethodDelete, Path: "/api/v1/firewall/peers",
		Summary: "Close the swarm's ports to a server again", Middlewares: admin, Security: signedIn,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *struct {
		Address string `query:"address" required:"true"`
	}) (*struct{}, error) {
		if err := s.DB.DeleteFirewallPeer(in.Address); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})
}

// Seed copies the listeners that were there before the install into the
// database, the first time only.
func (c FirewallConfig) Seed(db *store.DB) error {
	ports := make([]store.FirewallPort, len(c.Keep))
	for i, p := range c.Keep {
		ports[i] = store.FirewallPort{Port: p.Port, Proto: p.Proto, Note: "there before the install"}
	}
	return db.SeedFirewallPorts(ports)
}

type apiFirewallPort struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
	Note  string `json:"note"`
	// Fixed ports (SSH, 80, 443) cannot be removed.
	Fixed bool `json:"fixed"`
}

type apiPublishedPort struct {
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
	Service string `json:"service"`
	Open    bool   `json:"open" doc:"Reachable from the internet"`
}

type apiFirewallPorts struct {
	Allowed   []apiFirewallPort  `json:"allowed" nullable:"false"`
	Published []apiPublishedPort `json:"published" nullable:"false"`
}

type firewallPortsOutput struct{ Body apiFirewallPorts }

type apiNewFirewallPort struct {
	Port  int    `json:"port" minimum:"1" maximum:"65535"`
	Proto string `json:"proto" enum:"tcp,udp"`
	Note  string `json:"note,omitempty" maxLength:"200"`
}

type firewallPortInput struct{ Body apiNewFirewallPort }

func (s *Server) firewallPorts(ctx context.Context) (apiFirewallPorts, error) {
	stored, err := s.DB.FirewallPorts()
	if err != nil {
		return apiFirewallPorts{}, err
	}
	out := apiFirewallPorts{Allowed: []apiFirewallPort{}, Published: []apiPublishedPort{}}
	open := map[firewall.Port]bool{}
	for _, p := range []int{80, 443} {
		out.Allowed = append(out.Allowed, apiFirewallPort{Port: p, Proto: "tcp", Note: "the panel and Traefik", Fixed: true})
		open[firewall.Port{Port: p, Proto: "tcp"}] = true
	}
	for _, p := range stored {
		out.Allowed = append(out.Allowed, apiFirewallPort{Port: p.Port, Proto: p.Proto, Note: p.Note})
		open[firewall.Port{Port: p.Port, Proto: p.Proto}] = true
	}
	svcs, err := s.Docker.Services(ctx)
	if err != nil {
		return out, nil // the allowed list is still worth showing
	}
	for _, sv := range svcs {
		for _, p := range sv.Ports {
			out.Published = append(out.Published, apiPublishedPort{p.Port, p.Proto, sv.Name, open[firewall.Port{Port: p.Port, Proto: p.Proto}]})
		}
	}
	slices.SortFunc(out.Published, func(a, b apiPublishedPort) int { return cmp.Or(a.Port-b.Port, strings.Compare(a.Proto, b.Proto)) })
	return out, nil
}

func (s *Server) registerFirewallPorts(api huma.API, admin huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "list-firewall-ports", Method: http.MethodGet, Path: "/api/v1/firewall/ports",
		Summary: "Ports open to everyone, and the ports services publish", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*firewallPortsOutput, error) {
		out, err := s.firewallPorts(ctx)
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &firewallPortsOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "allow-firewall-port", Method: http.MethodPost, Path: "/api/v1/firewall/ports",
		Summary: "Open a port to everyone, on every node", Middlewares: admin, Security: signedIn,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *firewallPortInput) (*struct{ Body apiFirewallPort }, error) {
		b := in.Body
		if swarmOwned(b.Port, b.Proto) {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "that port belongs to the swarm and is open to the cluster's nodes already")
		}
		if err := s.DB.AddFirewallPort(store.FirewallPort{Port: b.Port, Proto: b.Proto, Note: b.Note}); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &struct{ Body apiFirewallPort }{apiFirewallPort{Port: b.Port, Proto: b.Proto, Note: b.Note}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "close-firewall-port", Method: http.MethodDelete, Path: "/api/v1/firewall/ports",
		Summary: "Close a port again", Middlewares: admin, Security: signedIn,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *struct {
		Port  int    `query:"port" required:"true"`
		Proto string `query:"proto" required:"true" enum:"tcp,udp"`
	}) (*struct{}, error) {
		if err := s.DB.DeleteFirewallPort(in.Port, in.Proto); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})
}

// swarmOwned: opening these to everyone would undo the cluster's own rule.
func swarmOwned(port int, proto string) bool {
	return (proto == "tcp" && (port == 2377 || port == 7946)) || (proto == "udp" && (port == 7946 || port == 4789))
}
