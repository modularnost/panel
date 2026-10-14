package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"
)

// A ui page (ADR 8) is a document of blocks the module answers with and the
// panel draws. Its text is only text; what the panel keeps in check here is the
// size of it, not what it says.
const (
	UIVersion        = 1
	MaxDocumentBytes = 1 << 20
	MaxBlocks        = 200
	MaxSeries        = 8
	MaxPoints        = 2000
)

// Document is what a ui contribution's path answers. A block the panel does not
// know is passed on: the renderer shows it as unsupported.
type Document struct {
	UI     int              `json:"ui"`
	Blocks []map[string]any `json:"blocks"`
}

// ParseDocument reads and checks a document. It is the panel's guard against a
// module that sends more than a page can hold.
func ParseDocument(r io.Reader) (Document, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil {
		return Document{}, err
	}
	if len(body) > MaxDocumentBytes {
		return Document{}, fmt.Errorf("the document is over %d bytes", MaxDocumentBytes)
	}
	var d Document
	if err := json.Unmarshal(body, &d); err != nil {
		return Document{}, fmt.Errorf("the document is not valid: %w", err)
	}
	switch {
	case d.UI == 0:
		return Document{}, fmt.Errorf(`the document has no "ui": %d`, UIVersion)
	case d.UI != UIVersion:
		return Document{}, fmt.Errorf("the document is ui %d, this panel understands %d", d.UI, UIVersion)
	case len(d.Blocks) > MaxBlocks:
		return Document{}, fmt.Errorf("the document has %d blocks, at most %d", len(d.Blocks), MaxBlocks)
	}
	if d.Blocks == nil {
		d.Blocks = []map[string]any{}
	}
	for i, b := range d.Blocks {
		if t, _ := b["type"].(string); t == "" {
			return Document{}, fmt.Errorf("block %d has no type", i)
		}
		if err := checkChart(b); err != nil {
			return Document{}, fmt.Errorf("block %d: %w", i, err)
		}
	}
	return d, nil
}

func checkChart(b map[string]any) error {
	if b["type"] != "chart" {
		return nil
	}
	series, _ := b["series"].([]any)
	if len(series) > MaxSeries {
		return fmt.Errorf("a chart has %d series, at most %d", len(series), MaxSeries)
	}
	for _, s := range series {
		m, _ := s.(map[string]any)
		if points, _ := m["points"].([]any); len(points) > MaxPoints {
			return fmt.Errorf("a series has %d points, at most %d", len(points), MaxPoints)
		}
	}
	return nil
}

// UIPage is the ui contribution whose page is p: only a page the manifest
// declared is fetched or notified about.
func (m Module) UIPage(p string) (Contribution, bool) {
	p = path.Clean("/" + p)
	for _, c := range m.Manifest.Contributions {
		if c.Render == RenderUI && path.Clean(c.Path) == p {
			return c, true
		}
	}
	return Contribution{}, false
}

// Viewer is who is looking at a page, as the module is told.
type Viewer struct{ Username, Role string }

// FetchPage asks a module for the document of one of its pages. The request is
// the panel's own: nothing the browser sent, cookies included, goes with it.
func (r *Registry) FetchPage(ctx context.Context, m Module, p, rawQuery string, v Viewer) (Document, error) {
	u := m.BaseURL + path.Clean("/"+p)
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Document{}, err
	}
	token, now := r.Token(m.Name), time.Now().Unix()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Panel-Module", m.Name)
	req.Header.Set("X-Panel-Token", token)
	req.Header.Set("X-Panel-User", v.Username)
	req.Header.Set("X-Panel-Role", v.Role)
	req.Header.Set("X-Panel-Time", strconv.FormatInt(now, 10))
	req.Header.Set("X-Panel-User-Sig", SignUser(token, m.Name, v.Username, v.Role, now))
	resp, err := r.http.Do(req)
	if err != nil {
		return Document{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Document{}, fmt.Errorf("the page returned %s", resp.Status)
	}
	return ParseDocument(resp.Body)
}
