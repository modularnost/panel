// Package firewall is the host firewall of a node (ADR 9). The panel's binary
// runs as a global service with the host's network and NET_ADMIN, listens on
// nothing, and keeps one chain of its own in sync with what the panel says:
// SSH, the ports that are allowed, and the addresses of the cluster's nodes for
// the swarm's own ports. Everything else that reaches the host is dropped.
package firewall

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Chain is the one chain this package owns, hooked into INPUT.
const Chain = "MODULARNOST"

// The swarm's own ports: management and gossip, and the overlay's VXLAN. They
// are open to the nodes of the cluster and to nobody else.
var (
	swarmTCP = []int{2377, 7946}
	swarmUDP = []int{7946, 4789}
)

// Port is a port of one protocol, "tcp" or "udp".
type Port struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
}

func (p Port) String() string { return fmt.Sprintf("%d/%s", p.Port, p.Proto) }

// Spec is what a node's firewall is told: nothing in it is trusted as text, it
// is checked field by field before it becomes a rule.
type Spec struct {
	SSH   []int    `json:"ssh"`
	Allow []Port   `json:"allow"`
	Peers []string `json:"peers"`
}

// Token is what a firewall service presents to the panel, and what the panel checks.
func Token(secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("firewall"))
	return hex.EncodeToString(mac.Sum(nil))
}

// ParsePorts reads "10050/tcp,53/udp"; a bare number is tcp.
func ParsePorts(s string) ([]Port, error) {
	var out []Port
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		num, proto, _ := strings.Cut(f, "/")
		if proto == "" {
			proto = "tcp"
		}
		n, err := strconv.Atoi(num)
		if err != nil || n < 1 || n > 65535 || (proto != "tcp" && proto != "udp") {
			return nil, fmt.Errorf("%q is not a port like 10050/tcp", f)
		}
		out = append(out, Port{n, proto})
	}
	return out, nil
}

// ParseInts reads a comma separated list of port numbers.
func ParseInts(s string) ([]int, error) {
	ports, err := ParsePorts(s)
	if err != nil {
		return nil, err
	}
	out := make([]int, len(ports))
	for i, p := range ports {
		out[i] = p.Port
	}
	return out, nil
}

// Validate refuses a spec that would not be a plain list of ports and addresses.
func (s Spec) Validate() error {
	for _, p := range s.SSH {
		if p < 1 || p > 65535 {
			return fmt.Errorf("ssh port %d", p)
		}
	}
	for _, p := range s.Allow {
		if p.Port < 1 || p.Port > 65535 || (p.Proto != "tcp" && p.Proto != "udp") {
			return fmt.Errorf("allowed port %v", p)
		}
	}
	for _, a := range s.Peers {
		if _, err := netip.ParseAddr(a); err != nil {
			return fmt.Errorf("peer address %q", a)
		}
	}
	return nil
}

// Rules is the input of iptables-restore --noflush for the filter table, for
// IPv4 or IPv6. The chain is rebuilt whole, atomically, so a node is never
// between two rule sets. SSH is always allowed, whatever the spec says: the one
// mistake that cannot be undone from the panel is a host nobody can reach.
func Rules(s Spec, v6 bool) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	acc := func(proto string, port int) { w("-A %s -p %s --dport %d -j ACCEPT", Chain, proto, port) }

	w("*filter")
	w(":%s - [0:0]", Chain)
	w("-F %s", Chain)
	w("-A %s -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT", Chain)
	w("-A %s -i lo -j ACCEPT", Chain)
	// What a container may ask of its host (a DNS cache, a registry mirror).
	for _, ifc := range []string{"docker0", "docker_gwbridge", "br-+"} {
		w("-A %s -i %s -j ACCEPT", Chain, ifc)
	}
	if v6 {
		// Neighbour discovery and router advertisements are how the host is
		// reachable at all; the DHCPv6 client listens on 546.
		w("-A %s -p ipv6-icmp -j ACCEPT", Chain)
		acc("udp", 546)
	} else {
		w("-A %s -p icmp -j ACCEPT", Chain)
		acc("udp", 68)
	}

	ssh := slices.Clone(s.SSH)
	ssh = append(ssh, 22)
	slices.Sort(ssh)
	for _, p := range slices.Compact(ssh) {
		acc("tcp", p)
	}

	allow := slices.Clone(s.Allow)
	slices.SortFunc(allow, func(a, b Port) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Proto, b.Proto)
	})
	for _, p := range slices.Compact(allow) {
		acc(p.Proto, p.Port)
	}

	for _, a := range sortedPeers(s.Peers, v6) {
		w("-A %s -s %s -p tcp -m multiport --dports %s -j ACCEPT", Chain, a, joinInts(swarmTCP))
		w("-A %s -s %s -p udp -m multiport --dports %s -j ACCEPT", Chain, a, joinInts(swarmUDP))
		w("-A %s -s %s -p esp -j ACCEPT", Chain, a)
	}

	w("-A %s -j DROP", Chain)
	w("COMMIT")
	return b.String()
}

func joinInts(in []int) string {
	s := make([]string, len(in))
	for i, n := range in {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}

// sortedPeers are the addresses of one family, without a duplicate. A v4-mapped
// v6 address is the v4 address.
func sortedPeers(in []string, v6 bool) []string {
	set := map[string]bool{}
	for _, a := range in {
		addr, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		addr = addr.Unmap()
		if addr.Is6() == v6 {
			set[addr.String()] = true
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// runner is the command line, so that a test does not need a kernel.
type runner func(ctx context.Context, stdin string, name string, args ...string) error

func execRun(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return nil
}

// Apply puts the spec in force on this node, for both address families. IPv6 is
// best effort: a kernel without it is not a reason to leave IPv4 open.
func Apply(ctx context.Context, s Spec) error {
	return apply(ctx, s, execRun)
}

func apply(ctx context.Context, s Spec, run runner) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := family(ctx, run, "iptables", false, s); err != nil {
		return err
	}
	if err := family(ctx, run, "ip6tables", true, s); err != nil {
		log.Printf("firewall: IPv6 not applied: %v", err)
	}
	return nil
}

func family(ctx context.Context, run runner, bin string, v6 bool, s Spec) error {
	if err := run(ctx, Rules(s, v6), bin+"-restore", "--noflush"); err != nil {
		return err
	}
	// The jump goes in after the chain is full, never before.
	if run(ctx, "", bin, "-C", "INPUT", "-j", Chain) != nil {
		return run(ctx, "", bin, "-I", "INPUT", "1", "-j", Chain)
	}
	return nil
}

// Clear takes the chain away, for an uninstall.
func Clear(ctx context.Context) error {
	return clearWith(ctx, execRun)
}

func clearWith(ctx context.Context, run runner) error {
	var errs []error
	for _, bin := range []string{"iptables", "ip6tables"} {
		// A jump may have been inserted more than once by hand.
		for range 8 {
			if run(ctx, "", bin, "-D", "INPUT", "-j", Chain) != nil {
				break
			}
		}
		_ = run(ctx, "", bin, "-F", Chain)
		if err := run(ctx, "", bin, "-X", Chain); err != nil && !strings.Contains(err.Error(), "No chain") && !strings.Contains(err.Error(), "does not exist") {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Status is what a node reports back after it applied a spec.
type Status struct {
	Node  string `json:"node"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Sig   string `json:"sig"`
}

// Sig is a short fingerprint of a spec, to tell "same as before" cheaply.
func (s Spec) Sig() string {
	sum := sha256.Sum256([]byte(Rules(s, false) + Rules(s, true)))
	return hex.EncodeToString(sum[:6])
}

// Run is the firewall service: ask the panel what to allow, apply it, say what
// happened, and do it again. The panel being away leaves the last rules in
// place, which is the safe side of that.
func Run(ctx context.Context, coreURL, secret, node string, every time.Duration) {
	c := &http.Client{Timeout: 15 * time.Second}
	var lastSig, lastErr string
	for {
		spec, err := fetch(ctx, c, coreURL, secret, node)
		if err == nil {
			err = Apply(ctx, spec)
		}
		st := Status{Node: node, OK: err == nil, Sig: spec.Sig()}
		if err != nil {
			st.Error = err.Error()
		}
		switch {
		case err != nil && err.Error() != lastErr:
			log.Printf("firewall: %v", err)
		case err == nil && st.Sig != lastSig:
			log.Printf("firewall: applied %d allowed, %d ssh, %d peers (%s)", len(spec.Allow), len(spec.SSH), len(spec.Peers), st.Sig)
		}
		lastSig, lastErr = st.Sig, st.Error
		report(ctx, c, coreURL, secret, st)

		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func fetch(ctx context.Context, c *http.Client, coreURL, secret, node string) (Spec, error) {
	u := strings.TrimRight(coreURL, "/") + "/internal/firewall?node=" + url.QueryEscape(node)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Spec{}, err
	}
	req.Header.Set("X-Panel-Token", Token(secret))
	resp, err := c.Do(req)
	if err != nil {
		return Spec{}, fmt.Errorf("asking the panel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Spec{}, fmt.Errorf("the panel answered %s", resp.Status)
	}
	var s Spec
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&s); err != nil {
		return Spec{}, fmt.Errorf("reading the answer: %w", err)
	}
	return s, nil
}

func report(ctx context.Context, c *http.Client, coreURL, secret string, st Status) {
	body, _ := json.Marshal(st)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(coreURL, "/")+"/internal/firewall/applied", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("X-Panel-Token", Token(secret))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
}
