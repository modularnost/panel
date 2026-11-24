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
	pending int
	fails   int
	until   time.Time // no attempt before this
	last    time.Time
}

const webhookRequestsPerMinute = 60

type webhookLimiter struct {
	mu     sync.Mutex
	reset  time.Time
	counts map[string]int
}

func (l *webhookLimiter) take(ip string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.reset) {
		l.reset = now.Truncate(time.Minute).Add(time.Minute)
		l.counts = make(map[string]int)
	}
	count := l.counts[ip]
	if count >= webhookRequestsPerMinute || (count == 0 && len(l.counts) >= maxTracked) {
		return l.reset.Sub(now)
	}
	l.counts[ip] = count + 1
	return 0
}

// loginLimiter slows password guessing down with a pause that doubles per
// failure. Never a lockout: a pause an attacker can trigger for a name is one
// short enough not to matter to its owner. State is in memory; a restart
// forgets it, which costs the attacker nothing it did not already have.
type loginLimiter struct {
	mu        sync.Mutex
	m         map[string]*attempts
	now       func() time.Time // a test sets it
	nextPrune time.Time
}

func (l *loginLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *loginLimiter) reserve(ip, user string) (finish func(failed, succeeded bool), wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if !now.Before(l.nextPrune) {
		l.prune(now)
		l.nextPrune = now.Add(time.Second)
	}
	keys := [2]string{ip, user}
	missing := 0
	for _, k := range keys {
		a := l.m[k]
		if a == nil {
			missing++
			continue
		}
		if a.pending == 0 && now.Sub(a.last) > attemptWindow {
			a.fails = 0
		}
		if a.until.After(now) {
			wait = max(wait, a.until.Sub(now))
		} else if a.pending >= max(1, freeAttempts-a.fails) {
			wait = max(wait, time.Second)
		}
	}
	if len(l.m)+missing > maxTracked {
		wait = max(wait, time.Second)
	}
	if wait > 0 {
		return nil, wait
	}
	if l.m == nil {
		l.m = make(map[string]*attempts)
	}
	for _, k := range keys {
		if l.m[k] == nil {
			l.m[k] = &attempts{}
		}
		l.m[k].pending++
	}
	return func(failed, succeeded bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		now := l.clock()
		for _, k := range keys {
			a := l.m[k]
			a.pending--
			switch {
			case failed:
				a.fails++
				a.last = now
				if a.fails >= freeAttempts {
					pause := time.Second << min(a.fails-freeAttempts, 6)
					a.until = now.Add(min(pause, maxPause))
				}
			case succeeded && k == user:
				a.fails = 0
				a.until = time.Time{}
			}
			if a.pending == 0 && a.fails == 0 {
				delete(l.m, k)
			}
		}
	}, 0
}

func (l *loginLimiter) prune(now time.Time) {
	for k, a := range l.m {
		if a.pending == 0 && now.Sub(a.last) > attemptWindow && !a.until.After(now) {
			delete(l.m, k)
		}
	}
}

// The user key is lower-cased and applies to names that do not exist too:
// a different answer for an unknown name would tell an attacker which exist.
func loginKeys(r *http.Request, trusted []netip.Prefix, username string) (ip, user string) {
	return "ip:" + clientIP(r, trusted), "user:" + strings.ToLower(strings.TrimSpace(username))
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
