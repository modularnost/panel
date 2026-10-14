// Package modules discovers module services and their manifests.
//
// A module is a separate service in Swarm, marked with labels:
//
//	panel.module=true
//	panel.module.name=<name>
//	panel.module.version=<semver>
//	panel.module.port=<port>   (optional, defaults to 8080)
//
// The core talks to it over HTTP inside the overlay network; a module has no
// docker.sock and asks the core to act.
package modules

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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

// What a module that does not answer is doing: a service that has not come up
// is not the same as one that came up and serves no manifest.
const (
	StateReady    = "ready"
	StateStarting = "starting" // fewer replicas running than wanted, nothing has failed
	StateFailed   = "failed"   // its tasks are rejected or fail: Err is Swarm's reason
	StateError    = "error"    // it runs, and the manifest could not be fetched
)

type Module struct {
	Name     string // from the label, not the manifest: labels are the routing truth
	Version  string
	Service  string // swarm service name
	BaseURL  string
	Manifest Manifest
	State    string
	Err      string // why it is not ready
}

func (m Module) OK() bool { return m.Err == "" }

// classify says why a module's manifest could not be fetched. failure is what
// Swarm reports for its tasks, and only matters while not all replicas run.
func classify(running, desired uint64, failure string, fetch error) (state, msg string) {
	switch {
	case running >= desired:
		return StateError, fetch.Error()
	case failure != "":
		return StateFailed, failure
	default:
		return StateStarting, fmt.Sprintf("starting (%d/%d)", running, desired)
	}
}

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
			BaseURL: "http://" + sv.Name + ":" + port,
		}
		if err := r.fetchManifest(ctx, &m); err != nil {
			var failure string
			if sv.Running < sv.Desired {
				failure, _ = r.dk.LastFailure(ctx, sv.ID)
			}
			m.State, m.Err = classify(sv.Running, sv.Desired, failure, err)
		} else {
			m.State = StateReady
			if m.Version == "" {
				// The version label is optional; the manifest always has one.
				m.Version = m.Manifest.Version
			}
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
	// Discovery also hands the module its token, before any user request and
	// again on every refresh, so a restarted core repairs itself.
	req.Header.Set("X-Panel-Module", m.Name)
	req.Header.Set("X-Panel-Token", r.Token(m.Name))
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

// Refresh keeps discovery running with nobody watching, so scheduled modules
// get their token before the first page view.
func (r *Registry) Refresh(ctx context.Context) {
	for {
		if _, err := r.List(ctx); err != nil {
			log.Printf("module discovery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ttl):
		}
	}
}

// Forget drops the cache; call it after installing or removing a module.
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
