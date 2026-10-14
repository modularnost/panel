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
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

const (
	LabelEnabled = "panel.module"
	LabelName    = "panel.module.name"
	LabelVersion = "panel.module.version"
	LabelPort    = "panel.module.port"
)

// ContractVersion is the manifest format this core understands.
const ContractVersion = 1

// The places a module may put something in. A slot the core does not know is
// ignored, so a newer module does not break an older core.
const (
	SlotNav            = "nav"
	SlotServiceTab     = "service.tab"
	SlotServicesAction = "services.action"
)

type Manifest struct {
	Contract      int            `json:"contract"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Contributions []Contribution `json:"contributions"`
	// Permissions is what the module may ask the core for, e.g. "service:read".
	Permissions []string `json:"permissions"`
}

// Contribution is one thing a module adds to the panel.
type Contribution struct {
	Slot  string `json:"slot"`
	Title string `json:"title"`
	Path  string `json:"path"` // below /modules/<name>
	// Role is the lowest role that may see this and reach its path.
	Role string `json:"role,omitempty"`
	// Match narrows a service.tab to services that carry all these labels.
	Match map[string]string `json:"match,omitempty"`
}

// validate checks the manifest and drops what this core does not know. A
// contribution is refused rather than guessed at: its role is a security
// decision.
func (m *Manifest) validate() error {
	switch {
	case m.Contract == 0:
		return fmt.Errorf(`the manifest has no "contract": %d, see docs/modules.md`, ContractVersion)
	case m.Contract != ContractVersion:
		return fmt.Errorf("the manifest is contract %d, this panel understands %d", m.Contract, ContractVersion)
	}
	known := m.Contributions[:0]
	for i, c := range m.Contributions {
		switch c.Slot {
		case SlotNav, SlotServiceTab, SlotServicesAction:
		default:
			continue
		}
		if strings.TrimSpace(c.Title) == "" {
			return fmt.Errorf("contribution %d (%s): no title", i, c.Slot)
		}
		if !strings.HasPrefix(c.Path, "/") || slices.Contains(strings.Split(c.Path, "/"), "..") {
			return fmt.Errorf("contribution %d (%s): path %q must start with / and not climb", i, c.Slot, c.Path)
		}
		if !slices.Contains([]string{"", store.RoleViewer, store.RoleOperator, store.RoleAdmin}, c.Role) {
			return fmt.Errorf("contribution %d (%s): unknown role %q", i, c.Slot, c.Role)
		}
		if c.Role == "" {
			c.Role = store.RoleOperator
		}
		known = append(known, c)
	}
	m.Contributions = known
	return nil
}

// Contributions of one slot, in the order the module declared them.
func (m Module) In(slot string) []Contribution {
	var out []Contribution
	for _, c := range m.Manifest.Contributions {
		if c.Slot == slot {
			out = append(out, c)
		}
	}
	return out
}

// RoleFor is the lowest role that may reach path below the module's root: the
// longest declared path that is a prefix at a segment boundary wins, and what
// no contribution covers needs operator, as every module did before.
func (m Module) RoleFor(p string) string {
	p = path.Clean("/" + p)
	role, best := store.RoleOperator, -1
	for _, c := range m.Manifest.Contributions {
		root := strings.TrimSuffix(path.Clean(c.Path), "/")
		if (root == "" || p == root || strings.HasPrefix(p, root+"/")) && len(root) > best {
			role, best = c.Role, len(root)
		}
	}
	return role
}

// SignUser is how a module learns who is looking at its page without trusting
// a header anybody could add: an HMAC, keyed with the module's own token, over
// the user, the role, the time and the module's name.
func SignUser(token, module, user, role string, unix int64) string {
	mac := hmac.New(sha256.New, []byte(token))
	fmt.Fprintf(mac, "v1\n%s\n%s\n%d\n%s", user, role, unix, module)
	return hex.EncodeToString(mac.Sum(nil))
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
	if err := json.NewDecoder(resp.Body).Decode(&m.Manifest); err != nil {
		return err
	}
	return m.Manifest.validate()
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
