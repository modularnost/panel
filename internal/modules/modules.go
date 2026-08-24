// Package modules discovers module services and their manifests.
//
// A module is a separate service in Swarm, marked with labels:
//
//	panel.module=true
//	panel.module.name=<name>
//	panel.module.version=<semver>
//	panel.module.port=<port>   (optional, defaults to 8080)
//
// The core talks to it over HTTP inside the overlay network. A module gets no
// direct access to docker.sock — it asks the core to act on Swarm instead (see
// the internal API).
package modules

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"modularnost/internal/docker"
)

const (
	LabelEnabled = "panel.module"
	LabelName    = "panel.module.name"
	LabelVersion = "panel.module.version"
	LabelPort    = "panel.module.port"
)

type Manifest struct {
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	NavEntries []NavEntry `json:"nav_entries"`
	// Permissions is what the module may ask the core for, e.g. "service:read".
	Permissions []string `json:"permissions"`
}

type NavEntry struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

type Module struct {
	Name     string // from the label, not the manifest: labels are the routing truth
	Version  string
	Service  string // swarm service name
	BaseURL  string
	Manifest Manifest
	Err      string // manifest could not be fetched: shown as broken
}

func (m Module) OK() bool { return m.Err == "" }

func (m Module) Allowed(perm string) bool {
	return slices.Contains(m.Manifest.Permissions, perm)
}

type Registry struct {
	dk     *docker.Client
	secret []byte
	http   *http.Client

	mu     sync.Mutex
	cached []Module
	at     time.Time
}

// ponytail: TTL is hardcoded. Move it to an env var if anyone needs a different one.
const ttl = 15 * time.Second

func NewRegistry(dk *docker.Client, secret string) *Registry {
	return &Registry{
		dk:     dk,
		secret: []byte(secret),
		http:   &http.Client{Timeout: 5 * time.Second},
	}
}

// Token is how a module proves to the core that it is itself: an HMAC of its
// name, so the core stores nothing and has nothing to keep in sync.
func (r *Registry) Token(module string) string {
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(module))
	return hex.EncodeToString(mac.Sum(nil))
}

func (r *Registry) ValidToken(module, token string) bool {
	return hmac.Equal([]byte(r.Token(module)), []byte(token))
}

// List returns modules with their manifests. Cached in memory for a short TTL,
// never in the database (ADR 2).
func (r *Registry) List(ctx context.Context) ([]Module, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.at) < ttl && r.cached != nil {
		return r.cached, nil
	}
	svcs, err := r.dk.Services(ctx)
	if err != nil {
		return nil, err
	}
	var mods []Module
	for _, sv := range svcs {
		if sv.Labels[LabelEnabled] != "true" {
			continue
		}
		name := sv.Labels[LabelName]
		if name == "" {
			name = sv.Name
		}
		port := sv.Labels[LabelPort]
		if port == "" {
			port = "8080"
		}
		m := Module{
			Name:    name,
			Version: sv.Labels[LabelVersion],
			Service: sv.Name,
			// Resolved by Swarm's DNS inside the overlay network.
			BaseURL: "http://" + sv.Name + ":" + port,
		}
		if err := r.fetchManifest(ctx, &m); err != nil {
			m.Err = err.Error()
		} else if m.Version == "" {
			// The version label is optional; the manifest always has one.
			m.Version = m.Manifest.Version
		}
		mods = append(mods, m)
	}
	slices.SortFunc(mods, func(a, b Module) int { return strings.Compare(a.Name, b.Name) })
	r.cached, r.at = mods, time.Now()
	return mods, nil
}

func (r *Registry) fetchManifest(ctx context.Context, m *Module) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.BaseURL+"/manifest", nil)
	if err != nil {
		return err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/manifest returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(&m.Manifest)
}

// Forget drops the cache. Call it after installing or removing a module:
// otherwise the list keeps showing the old answer for up to a TTL, which reads
// as "the button did nothing".
func (r *Registry) Forget() {
	r.mu.Lock()
	r.at = time.Time{}
	r.mu.Unlock()
}

func (r *Registry) Get(ctx context.Context, name string) (Module, bool) {
	mods, err := r.List(ctx)
	if err != nil {
		return Module{}, false
	}
	for _, m := range mods {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}
