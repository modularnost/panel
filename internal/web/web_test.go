package web

import "testing"

func TestHostFromRule(t *testing.T) {
	rule := "Host(" + backtick + "app.example.com" + backtick + ")"
	if got := hostFromRule(rule); got != "app.example.com" {
		t.Fatalf("got %q", got)
	}
	if got := hostFromRule(""); got != "" {
		t.Fatalf("empty rule: %q", got)
	}
}

// Routes panics on a pattern conflict, and that happens at startup — which is
// far too late to find out. One call catches it here instead.
func TestRoutesRegister(t *testing.T) {
	(&Server{}).Routes()
}
