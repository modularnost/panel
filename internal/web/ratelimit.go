package web

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	freeAttempts  = 5                // wrong passwords before the pauses start
	attemptWindow = 10 * time.Minute // a quiet key starts again from zero
	maxPause      = time.Minute
	maxTracked    = 10000
)

type attempts struct {
	fails int
	until time.Time // no attempt before this
	last  time.Time
}

// loginLimiter slows password guessing down with a pause that doubles per
// failure. Never a lockout: a pause an attacker can trigger for a name is one
// short enough not to matter to its owner. State is in memory; a restart
// forgets it, which costs the attacker nothing it did not already have.
type loginLimiter struct {
	mu  sync.Mutex
	m   map[string]*attempts
	now func() time.Time // a test sets it
}

func (l *loginLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// wait is how long the longest-paused key still has to wait; zero means go on.
func (l *loginLimiter) wait(keys ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	var longest time.Duration
	for _, k := range keys {
		if a := l.m[k]; a != nil && a.until.After(now) {
			longest = max(longest, a.until.Sub(now))
		}
	}
	return longest
}

func (l *loginLimiter) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if l.m == nil {
		l.m = map[string]*attempts{}
	}
	if len(l.m) >= maxTracked {
		l.prune(now)
	}
	for _, k := range keys {
		a := l.m[k]
		if a == nil {
			// ponytail: past the cap an unseen key goes untracked, the per-IP keys still hold; add eviction if it shows.
			if len(l.m) >= maxTracked {
				continue
			}
			a = &attempts{}
			l.m[k] = a
		}
		if now.Sub(a.last) > attemptWindow {
			a.fails = 0
		}
		a.fails++
		a.last = now
		if a.fails >= freeAttempts {
			pause := time.Second << min(a.fails-freeAttempts, 6)
			a.until = now.Add(min(pause, maxPause))
		}
	}
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

func (l *loginLimiter) prune(now time.Time) {
	for k, a := range l.m {
		if now.Sub(a.last) > attemptWindow && !a.until.After(now) {
			delete(l.m, k)
		}
	}
}

// The user key is lower-cased and applies to names that do not exist too:
// a different answer for an unknown name would tell an attacker which exist.
func loginKeys(r *http.Request, trusted []netip.Prefix, username string) (ip, user string) {
	return "ip:" + clientIP(r, trusted), "user:" + strings.ToLower(strings.TrimSpace(username))
}

// loginBlocked is how long this caller must wait before a password is tried.
func (s *Server) loginBlocked(r *http.Request, username string) time.Duration {
	ip, user := loginKeys(r, s.TrustedProxies, username)
	return s.logins.wait(ip, user)
}

func (s *Server) loginFailed(r *http.Request, username string) {
	ip, user := loginKeys(r, s.TrustedProxies, username)
	s.logins.fail(ip, user)
}

// loginSucceeded clears the name only: a signed-in attacker must not be able
// to wipe the counter of the address it guesses from.
func (s *Server) loginSucceeded(r *http.Request, username string) {
	_, user := loginKeys(r, s.TrustedProxies, username)
	s.logins.reset(user)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func retryAfterSeconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// clientIP is the address the request really came from. X-Forwarded-For is
// read only when the peer is a configured proxy, and from the right: each
// proxy appends the address it saw, so everything left of the first untrusted
// hop is whatever the client wrote. IPv6 clients are counted by their /64,
// the smallest block one subscriber gets.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := peerAddr(r.RemoteAddr)
	addr := peer
	if isTrusted(peer, trusted) {
		hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			a = a.Unmap()
			addr = a
			if !isTrusted(a, trusted) {
				break
			}
		}
	}
	if addr.Is6() {
		p, _ := addr.Prefix(64)
		return p.String()
	}
	return addr.String()
}

func peerAddr(remote string) netip.Addr {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return ap.Addr().Unmap()
	}
	return netip.Addr{}
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ParseProxies reads PANEL_TRUSTED_PROXIES: addresses or CIDR ranges, comma
// separated. Empty means no proxy is trusted.
func ParseProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: not an address or a CIDR range", part)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}
