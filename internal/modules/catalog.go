package modules

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/distribution/reference"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

const catalogBase = "https://raw.githubusercontent.com/modularnost/module-catalog/main/"
const catalogInterval = time.Hour
const maxCatalogBytes = 1 << 20

type CatalogScreenshot struct {
	Path string `json:"path"`
	Alt  string `json:"alt"`
}

func (s CatalogScreenshot) URL() string { return catalogBase + s.Path }

type CatalogRequirement struct {
	Contract        int    `json:"contract"`
	MinPanelVersion string `json:"min_panel_version,omitempty"`
}

type CatalogVolume struct {
	MountPath string `json:"mount_path"`
}

type CatalogInstall struct {
	Kind            string         `json:"kind"`
	Version         string         `json:"version,omitempty"`
	Image           string         `json:"image,omitempty"`
	Port            int            `json:"port,omitempty"`
	DataVolume      *CatalogVolume `json:"data_volume,omitempty"`
	InstructionsURL string         `json:"instructions_url,omitempty"`
}

func (i *CatalogInstall) UnmarshalJSON(data []byte) error {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	*i = CatalogInstall{Kind: header.Kind}
	if !i.supported() {
		return nil
	}
	type install CatalogInstall
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*install)(i))
}

func (i CatalogInstall) supported() bool {
	return i.Kind == "manual" || i.Kind == "swarm_service"
}

type CatalogEntry struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Summary     string              `json:"summary"`
	Maintainer  string              `json:"maintainer"`
	SourceURL   string              `json:"source_url"`
	DocsURL     string              `json:"docs_url,omitempty"`
	Screenshots []CatalogScreenshot `json:"screenshots,omitempty"`
	Requires    CatalogRequirement  `json:"requires"`
	Permissions []string            `json:"permissions"`
	Install     CatalogInstall      `json:"install"`
}

func (e CatalogEntry) Revision() string {
	body, _ := json.Marshal(e)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (e CatalogEntry) Incompatibility(running string) string {
	if e.Requires.Contract != ContractVersion {
		return fmt.Sprintf("Requires module contract %d", e.Requires.Contract)
	}
	if min := e.Requires.MinPanelVersion; min != "" {
		running = strings.TrimPrefix(running, "v")
		if !catalogVersion.MatchString(running) || compareVersions(running, min) < 0 {
			return "Requires panel " + min + " or newer"
		}
	}
	return ""
}

type CatalogSnapshot struct {
	Entries   []CatalogEntry
	CheckedAt time.Time
	Failed    bool
}

func (s CatalogSnapshot) Fresh(now time.Time) bool {
	return !s.Failed && !s.CheckedAt.IsZero() && now.Sub(s.CheckedAt) < catalogInterval
}

type Catalog struct {
	db       *store.DB
	client   *http.Client
	url      string
	mu       sync.Mutex
	snapshot atomic.Pointer[CatalogSnapshot]
}

func NewCatalog(ctx context.Context, db *store.DB) *Catalog {
	c := &Catalog{
		db: db, url: catalogBase + "catalog.json",
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	body, checkedAt, err := db.LoadModuleCatalog(ctx)
	if err == nil && len(body) != 0 {
		var entries []CatalogEntry
		entries, err = decodeCatalog(body)
		if err == nil {
			c.snapshot.Store(&CatalogSnapshot{Entries: entries, CheckedAt: checkedAt, Failed: true})
		}
	}
	if err != nil {
		log.Printf("loading module catalog cache: %v", err)
	}
	return c
}

func (c *Catalog) Status() CatalogSnapshot {
	if c != nil {
		if snapshot := c.snapshot.Load(); snapshot != nil {
			return *snapshot
		}
	}
	return CatalogSnapshot{}
}

func (c *Catalog) Run(ctx context.Context) {
	ticker := time.NewTicker(catalogInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := c.Refresh(ctx); err != nil && ctx.Err() == nil {
			log.Printf("refreshing module catalog: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Catalog) Refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.refresh(ctx); err != nil {
		if ctx.Err() == nil {
			previous := c.Status()
			previous.Failed = true
			c.snapshot.Store(&previous)
		}
		return err
	}
	return nil
}

func (c *Catalog) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return err
	}
	entries, err := decodeCatalog(body)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := c.db.SaveModuleCatalog(ctx, body, now); err != nil {
		return err
	}
	c.snapshot.Store(&CatalogSnapshot{Entries: entries, CheckedAt: now})
	return nil
}

var (
	catalogVersion     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	catalogImage       = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(?::[0-9]+)?/(?:[a-z0-9]+(?:[._-]+[a-z0-9]+)*/)*[a-z0-9]+(?:[._-]+[a-z0-9]+)*@sha256:[a-f0-9]{64}$`)
	catalogMount       = regexp.MustCompile(`^/[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)*$`)
	catalogShot        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*\.(png|jpg|jpeg|webp)$`)
	catalogPerm        = regexp.MustCompile(`^[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*$`)
	catalogInstallKind = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func decodeCatalog(body []byte) ([]CatalogEntry, error) {
	if len(body) > maxCatalogBytes {
		return nil, errors.New("catalog exceeds size limit")
	}
	var document struct {
		Version int            `json:"schema_version"`
		Entries []CatalogEntry `json:"modules"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing data after catalog")
	}
	if document.Version != 1 || document.Entries == nil {
		return nil, errors.New("expected module catalog schema version 1")
	}
	seen := make(map[string]bool)
	for _, entry := range document.Entries {
		id := strings.ToLower(entry.ID)
		if seen[id] || !entry.valid() {
			return nil, fmt.Errorf("invalid or duplicate catalog entry %q", entry.ID)
		}
		seen[id] = true
	}
	return slices.DeleteFunc(document.Entries, func(entry CatalogEntry) bool {
		return !entry.Install.supported()
	}), nil
}

func (e CatalogEntry) valid() bool {
	if !docker.ValidName(e.ID) || strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Summary) == "" ||
		strings.TrimSpace(e.Maintainer) == "" || !catalogHTTPS(e.SourceURL) || (e.DocsURL != "" && !catalogHTTPS(e.DocsURL)) ||
		e.Requires.Contract < 1 || (e.Requires.MinPanelVersion != "" && !catalogVersion.MatchString(e.Requires.MinPanelVersion)) ||
		e.Permissions == nil {
		return false
	}
	permissions := make(map[string]bool, len(e.Permissions))
	for _, permission := range e.Permissions {
		if !catalogPerm.MatchString(permission) || permissions[permission] {
			return false
		}
		permissions[permission] = true
	}
	for _, shot := range e.Screenshots {
		file, ok := strings.CutPrefix(shot.Path, "screenshots/"+e.ID+"/")
		if !ok || !catalogShot.MatchString(file) || strings.TrimSpace(shot.Alt) == "" {
			return false
		}
	}
	i := e.Install
	switch i.Kind {
	case "manual":
		return catalogHTTPS(i.InstructionsURL) && i.Image == "" && i.Version == "" && i.Port == 0 && i.DataVolume == nil
	case "swarm_service":
		_, err := reference.Parse(i.Image)
		return err == nil && catalogImage.MatchString(i.Image) && catalogVersion.MatchString(i.Version) &&
			i.Port >= 0 && i.Port <= 65535 && i.InstructionsURL == "" &&
			(i.DataVolume == nil || catalogMount.MatchString(i.DataVolume.MountPath))
	default:
		return catalogInstallKind.MatchString(i.Kind)
	}
}

func catalogHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

func compareVersions(a, b string) int {
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	a, aPre, _ := strings.Cut(a, "-")
	b, bPre, _ := strings.Cut(b, "-")
	numeric := func(s string) bool { return strings.Trim(s, "0123456789") == "" }
	compare := func(a, b string) int {
		if numeric(a) && numeric(b) && len(a) != len(b) {
			return cmp.Compare(len(a), len(b))
		}
		if numeric(a) != numeric(b) {
			if numeric(a) {
				return -1
			}
			return 1
		}
		return cmp.Compare(a, b)
	}
	if order := slices.CompareFunc(strings.Split(a, "."), strings.Split(b, "."), compare); order != 0 {
		return order
	}
	if aPre == "" || bPre == "" {
		return cmp.Compare(bPre, aPre)
	}
	return slices.CompareFunc(strings.Split(aPre, "."), strings.Split(bPre, "."), compare)
}
