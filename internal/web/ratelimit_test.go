package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func req(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	trusted, err := ParseProxies("10.0.0.0/24, 192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		r    *http.Request
		want string
	}{
		"no proxy configured":              {req("203.0.113.7:5000", "6.6.6.6"), "203.0.113.7"},
		"an untrusted peer forging":        {req("203.0.113.7:5000", "6.6.6.6"), "203.0.113.7"},
		"a proxy states the client":        {req("10.0.0.5:5000", "198.51.100.2"), "198.51.100.2"},
		"the client wrote left of it":      {req("10.0.0.5:5000", "6.6.6.6, 198.51.100.2"), "198.51.100.2"},
		"two proxies in the chain":         {req("10.0.0.5:5000", "198.51.100.2, 10.0.0.9"), "198.51.100.2"},
		"two header lines":                 {req("10.0.0.5:5000", "6.6.6.6", "198.51.100.2"), "198.51.100.2"},
		"a single trusted address":         {req("192.168.1.1:80", "198.51.100.2"), "198.51.100.2"},
		"a proxy with no header":           {req("10.0.0.5:5000"), "10.0.0.5"},
		"garbage in the header":            {req("10.0.0.5:5000", "not-an-ip"), "10.0.0.5"},
		"v6 client counted by its /64":     {req("10.0.0.5:5000", "2001:db8:1:2:aaaa:bbbb:cccc:dddd"), "2001:db8:1:2::/64"},
		"v6 neighbours share the same key": {req("10.0.0.5:5000", "2001:db8:1:2::1"), "2001:db8:1:2::/64"},
		"v4-mapped peer":                   {req("[::ffff:203.0.113.7]:5000"), "203.0.113.7"},
	} {
		tr := trusted
		if name == "no proxy configured" {
			tr = nil
		}
		if got := clientIP(c.r, tr); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestParseProxiesRefusesGarbage(t *testing.T) {
	if _, err := ParseProxies("10.0.0.0/24, nope"); err == nil {
		t.Fatal("want an error")
	}
	if p, err := ParseProxies(""); err != nil || len(p) != 0 {
		t.Fatalf("empty must mean none: %v %v", p, err)
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func TestLimiterPausesDoubleAndNeverLock(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	l := &loginLimiter{now: clock.now}
	for range freeAttempts - 1 {
		l.fail("k")
		if w := l.wait("k"); w != 0 {
			t.Fatalf("paused before the free attempts were used: %v", w)
		}
	}
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		l.fail("k")
		if got := l.wait("k"); got != want {
			t.Fatalf("failure %d: pause %v, want %v", freeAttempts+i, got, want)
		}
		clock.t = clock.t.Add(want) // wait it out
	}
	for range 20 {
		l.fail("k")
		clock.t = clock.t.Add(maxPause)
	}
	l.fail("k")
	if got := l.wait("k"); got != maxPause {
		t.Fatalf("pause %v must stop growing at %v", got, maxPause)
	}
	clock.t = clock.t.Add(attemptWindow + maxPause)
	if got := l.wait("k"); got != 0 {
		t.Fatalf("a quiet key must be free again, still %v", got)
	}
	l.fail("k")
	if got := l.wait("k"); got != 0 {
		t.Fatalf("a long quiet key starts again from one failure, got pause %v", got)
	}
}

func TestLimiterPerAddressAndPerName(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	s := &Server{logins: loginLimiter{now: clock.now}}
	attacker := req("203.0.113.7:1")
	other := req("198.51.100.9:1")

	// One address guessing against many different names.
	for i := range freeAttempts {
		s.loginFailed(attacker, "user"+string(rune('a'+i)))
	}
	if s.loginBlocked(attacker, "fresh") == 0 {
		t.Error("the address must be paused whatever name it tries")
	}
	if s.loginBlocked(other, "fresh") != 0 {
		t.Error("another address must not feel it")
	}

	// Many addresses guessing against one name, spelled differently.
	for i := range freeAttempts {
		s.loginFailed(req("192.0.2."+string(rune('1'+i))+":1"), "  Ann ")
	}
	if s.loginBlocked(other, "ann") == 0 {
		t.Error("the name must be paused for everyone, in any case and spacing")
	}

	// Success clears the name, but not the address.
	s.loginSucceeded(other, "ann")
	if s.loginBlocked(other, "ann") != 0 {
		t.Error("a successful sign-in clears the name")
	}
	if s.loginBlocked(attacker, "fresh") == 0 {
		t.Error("a success elsewhere must not clear the attacker's address")
	}
}

func TestLoginIsThrottledWithRetryAfter(t *testing.T) {
	c, base, db := apiServerWith(t, func(s *Server) { s.logins.now = (&fakeClock{t: time.Unix(1_000_000, 0)}).now })
	_ = db
	const j = "application/json"
	bad := `{"username":"ann","password":"wrong-password"}`
	for i := range freeAttempts {
		if status, _ := send(t, c, "POST", base+"/api/v1/auth/login", j, bad); status != 401 {
			t.Fatalf("attempt %d: %d", i+1, status)
		}
	}
	resp, err := c.Post(base+"/api/v1/auth/login", j, strings.NewReader(`{"username":"ann","password":"s3cret-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "1" {
		t.Fatalf("the right password during a pause: %d Retry-After=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestBasicAuthGuessingIsThrottledToo(t *testing.T) {
	c, base, _ := apiServerWith(t, func(s *Server) { s.logins.now = (&fakeClock{t: time.Unix(1_000_000, 0)}).now })
	for range freeAttempts {
		req, _ := http.NewRequest("GET", base+"/api/v1/me", nil)
		req.SetBasicAuth("ann", "wrong-password")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("a wrong password: %d", resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", base+"/api/v1/me", nil)
	req.SetBasicAuth("ann", "s3cret-pass")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("the right password while paused must be refused: %d", resp.StatusCode)
	}
}
