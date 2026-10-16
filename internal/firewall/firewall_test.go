package firewall

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func lines(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }

func index(rules []string, want string) int {
	return slices.IndexFunc(rules, func(l string) bool { return strings.Contains(l, want) })
}

func TestRulesAreAChainThatEndsInDropAndAlwaysKeepsSSH(t *testing.T) {
	// The spec has no SSH at all, as a panel that could not read sshd would send.
	r := lines(Rules(Spec{Allow: []Port{{443, "tcp"}, {80, "tcp"}, {10050, "tcp"}}}, false))

	if r[0] != "*filter" || r[len(r)-1] != "COMMIT" || !strings.HasSuffix(r[len(r)-2], "-j DROP") {
		t.Fatalf("a chain is declared, rebuilt and ends in DROP:\n%s", strings.Join(r, "\n"))
	}
	ssh := index(r, "--dport 22 ")
	if ssh < 0 {
		t.Fatal("port 22 is allowed whatever the spec says: a host nobody can reach cannot be repaired from the panel")
	}
	// Established connections first, so that a rule change never cuts the one it runs over.
	if est, drop := index(r, "RELATED,ESTABLISHED"), index(r, "-j DROP"); est > ssh || ssh > drop {
		t.Errorf("order: established %d, ssh %d, drop %d", est, ssh, drop)
	}
	for _, port := range []string{"80", "443", "10050"} {
		if index(r, "-p tcp --dport "+port+" ") < 0 {
			t.Errorf("port %s is allowed in the spec but not in the rules", port)
		}
	}
	// The swarm's ports are open to peers only: with no peer, nothing mentions them.
	for _, l := range r {
		if strings.Contains(l, "2377") || strings.Contains(l, "4789") {
			t.Errorf("a swarm port with no peer: %s", l)
		}
	}
}

func TestRulesOpenTheSwarmPortsToPeersOfTheSameFamily(t *testing.T) {
	spec := Spec{SSH: []int{2222}, Peers: []string{"10.0.0.5", "2001:db8::5", "::ffff:10.0.0.6", "10.0.0.5"}}
	v4, v6 := Rules(spec, false), Rules(spec, true)

	for _, want := range []string{
		"-A MODULARNOST -s 10.0.0.5 -p tcp -m multiport --dports 2377,7946 -j ACCEPT",
		"-A MODULARNOST -s 10.0.0.5 -p udp -m multiport --dports 7946,4789 -j ACCEPT",
		"-A MODULARNOST -s 10.0.0.5 -p esp -j ACCEPT",
		"-A MODULARNOST -s 10.0.0.6 -p esp -j ACCEPT", // a v4-mapped address is the v4 address
		"--dport 2222 ",
	} {
		if !strings.Contains(v4, want) {
			t.Errorf("v4 rules lack %q", want)
		}
	}
	if strings.Contains(v4, "2001:db8") || strings.Contains(v6, "10.0.0.5") {
		t.Error("an address of one family is in the rules of the other")
	}
	if !strings.Contains(v6, "-s 2001:db8::5 ") || !strings.Contains(v6, "-p ipv6-icmp -j ACCEPT") || strings.Contains(v6, "-p icmp ") {
		t.Errorf("v6 rules:\n%s", v6)
	}
	if strings.Count(v4, "-s 10.0.0.5 -p esp") != 1 {
		t.Error("a peer given twice is one rule")
	}
}

func TestRulesAreTheSameForTheSameSpec(t *testing.T) {
	a := Spec{SSH: []int{22, 2222}, Allow: []Port{{443, "tcp"}, {80, "tcp"}}, Peers: []string{"10.0.0.2", "10.0.0.1"}}
	b := Spec{SSH: []int{2222, 22}, Allow: []Port{{80, "tcp"}, {443, "tcp"}}, Peers: []string{"10.0.0.1", "10.0.0.2"}}
	if Rules(a, false) != Rules(b, false) || a.Sig() != b.Sig() {
		t.Error("the order of a list must not change the rules: the node would reapply for nothing")
	}
	if a.Sig() == (Spec{SSH: []int{22}}).Sig() {
		t.Error("another spec, the same signature")
	}
}

func TestValidateRefusesWhatIsNotAPortOrAnAddress(t *testing.T) {
	for name, s := range map[string]Spec{
		"a port that is zero":       {SSH: []int{0}},
		"a port above the range":    {Allow: []Port{{70000, "tcp"}}},
		"a protocol that is a rule": {Allow: []Port{{80, "tcp -j ACCEPT"}}},
		"a peer that is a rule":     {Peers: []string{"1.2.3.4 -j ACCEPT"}},
		"a peer that is a name":     {Peers: []string{"example.com"}},
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := (Spec{SSH: []int{22}, Allow: []Port{{53, "udp"}}, Peers: []string{"10.0.0.1", "fe80::1"}}).Validate(); err != nil {
		t.Errorf("a plain spec: %v", err)
	}
}

func TestParsePorts(t *testing.T) {
	got, err := ParsePorts("10050/tcp, 53/udp 8080")
	if err != nil || !slices.Equal(got, []Port{{10050, "tcp"}, {53, "udp"}, {8080, "tcp"}}) {
		t.Fatalf("%v, %v", got, err)
	}
	for _, bad := range []string{"x", "0", "70000/tcp", "80/icmp", "80/tcp/x"} {
		if _, err := ParsePorts(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if ssh, err := ParseInts("22,2222"); err != nil || !slices.Equal(ssh, []int{22, 2222}) {
		t.Errorf("%v, %v", ssh, err)
	}
	if p, err := ParsePorts(""); err != nil || len(p) != 0 {
		t.Errorf("nothing is nothing: %v, %v", p, err)
	}
}

type call struct{ stdin, name string }

func recorder(failing map[string]bool) (runner, *[]call) {
	var calls []call
	return func(_ context.Context, stdin, name string, args ...string) error {
		key := name + " " + strings.Join(args, " ")
		calls = append(calls, call{stdin, key})
		if failing[key] || failing[name] {
			return errors.New("failed")
		}
		return nil
	}, &calls
}

func TestApplyFillsTheChainBeforeTheJumpIsInserted(t *testing.T) {
	run, calls := recorder(map[string]bool{
		"iptables -C INPUT -j MODULARNOST": true, "ip6tables -C INPUT -j MODULARNOST": true,
	})
	if err := apply(context.Background(), Spec{SSH: []int{22}}, run); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, c := range *calls {
		seq = append(seq, c.name)
	}
	want := []string{
		"iptables-restore --noflush", "iptables -C INPUT -j MODULARNOST", "iptables -I INPUT 1 -j MODULARNOST",
		"ip6tables-restore --noflush", "ip6tables -C INPUT -j MODULARNOST", "ip6tables -I INPUT 1 -j MODULARNOST",
	}
	if !slices.Equal(seq, want) {
		t.Errorf("commands:\n%s", strings.Join(seq, "\n"))
	}
	if !strings.Contains((*calls)[0].stdin, "*filter") {
		t.Error("the rules go in on stdin")
	}
}

func TestApplyDoesNotInsertASecondJump(t *testing.T) {
	run, calls := recorder(nil) // -C succeeds: the jump is there
	if err := apply(context.Background(), Spec{}, run); err != nil {
		t.Fatal(err)
	}
	for _, c := range *calls {
		if strings.Contains(c.name, " -I ") {
			t.Errorf("a jump was inserted again: %s", c.name)
		}
	}
}

func TestApplyFailsOnIPv4AndToleratesNoIPv6(t *testing.T) {
	run, _ := recorder(map[string]bool{"ip6tables-restore": true})
	if err := apply(context.Background(), Spec{}, run); err != nil {
		t.Errorf("a kernel without IPv6 is not an error: %v", err)
	}
	run, calls := recorder(map[string]bool{"iptables-restore": true})
	if err := apply(context.Background(), Spec{}, run); err == nil {
		t.Error("IPv4 not applied must be an error")
	}
	for _, c := range *calls {
		if strings.Contains(c.name, "-I INPUT") {
			t.Error("a chain that did not load must not be jumped to")
		}
	}
	if err := apply(context.Background(), Spec{Peers: []string{"nope"}}, func(context.Context, string, string, ...string) error { return nil }); err == nil {
		t.Error("a spec that is not valid reached the kernel")
	}
}

func TestClearTakesTheChainAway(t *testing.T) {
	run, calls := recorder(nil)
	// -D "succeeds" forever with this runner: the loop has to be bounded.
	if err := clearWith(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(*calls) > 40 {
		t.Errorf("%d commands: removing the jump never ends", len(*calls))
	}
	var tail []string
	for _, c := range *calls {
		if strings.Contains(c.name, " -X ") {
			tail = append(tail, c.name)
		}
	}
	if len(tail) != 2 {
		t.Errorf("the chain is deleted for both families: %v", tail)
	}
}

func TestTokenIsStableAndKeyedByTheSecret(t *testing.T) {
	first, again := Token("a"), Token("a")
	if first != again || first == Token("b") || len(first) != 64 {
		t.Error("token")
	}
}
