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
func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/stacks?x=1":     "/stacks?x=1",
		"":                "/",
		"//evil.com":      "/",
		"/\\evil.com":     "/",
		"https://evil.io": "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoutesRegister(t *testing.T) {
	(&Server{}).Routes()
}
