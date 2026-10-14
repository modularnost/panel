// Package updates asks GitHub which releases are newer than the running one.
// It only reads: applying an update is the caller's business.
package updates

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"
)

const feedURL = "https://api.github.com/repos/modularnost/panel/releases?per_page=30"

// Release is one published version with its notes.
type Release struct {
	Tag         string
	Body        string
	Prerelease  bool
	PublishedAt time.Time
	URL         string
}

// Status is what the last check found, relative to the running version.
type Status struct {
	Current      string
	ReleaseBuild bool // false for edge and dev builds, which have nothing to update to
	Enabled      bool
	CheckedAt    time.Time // zero before the first successful check
	Error        string    // the last failure; the previous answer is kept
	Newer        []Release // newest first
}

type Checker struct {
	current string
	enabled bool
	url     string
	client  *http.Client

	mu        sync.Mutex
	releases  []Release
	checkedAt time.Time
	lastErr   string
}

// New returns a checker for the running version; disabled makes it never
// touch the network.
func New(current string, enabled bool) *Checker {
	return &Checker{
		current: current, enabled: enabled, url: feedURL,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Run checks now and then every interval, until ctx ends.
func (c *Checker) Run(ctx context.Context, every time.Duration) {
	if !c.enabled {
		return
	}
	for {
		if err := c.Refresh(ctx); err != nil {
			log.Printf("update check: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// Refresh fetches the release list. A failure keeps the previous answer.
func (c *Checker) Refresh(ctx context.Context) error {
	if !c.enabled {
		return nil
	}
	list, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastErr = err.Error()
		return err
	}
	c.releases, c.checkedAt, c.lastErr = list, time.Now(), ""
	return nil
}

func (c *Checker) fetch(ctx context.Context) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "modularnost-panel")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github answered %s", resp.Status)
	}
	var raw []struct {
		Tag         string    `json:"tag_name"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		URL         string    `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("reading the release list: %w", err)
	}
	var out []Release
	for _, r := range raw {
		if r.Draft {
			continue
		}
		out = append(out, Release{r.Tag, r.Body, r.Prerelease, r.PublishedAt, r.URL})
	}
	return out, nil
}

// Status lists the releases newer than the running version.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, release := parse(c.current)
	st := Status{
		Current: c.current, ReleaseBuild: release, Enabled: c.enabled,
		CheckedAt: c.checkedAt, Error: c.lastErr,
	}
	if !release {
		return st
	}
	for _, r := range c.releases {
		// ponytail: tags that are not vX.Y.Z (rc, build metadata) are skipped, add when we publish them.
		if v, ok := parse(r.Tag); ok && compare(v, cur) > 0 {
			st.Newer = append(st.Newer, r)
		}
	}
	slices.SortFunc(st.Newer, func(a, b Release) int {
		va, _ := parse(a.Tag)
		vb, _ := parse(b.Tag)
		return compare(vb, va)
	})
	return st
}

// Has reports whether tag was one of the newer releases of the last check.
func (c *Checker) Has(tag string) bool {
	return slices.ContainsFunc(c.Status().Newer, func(r Release) bool { return r.Tag == tag })
}

type version [3]int

var versionRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// parse accepts only a release tag: an edge build (v0.1.0-4-gabc1234) or "dev"
// is not a version anyone can update from.
func parse(s string) (version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	var v version
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func compare(a, b version) int {
	for i := range a {
		if c := cmp.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}
