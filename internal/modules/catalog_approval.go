package modules

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const LabelCatalogApproval = "panel.module.catalog"

type catalogApproval struct {
	Image       string   `json:"image"`
	Version     string   `json:"version"`
	Permissions []string `json:"permissions"`
}

func (e CatalogEntry) ApprovalLabel() string {
	body, _ := json.Marshal(catalogApproval{e.Install.Image, e.Install.Version, e.Permissions})
	return string(body)
}

func (m *Module) loadApproval(raw, image string) error {
	var approval catalogApproval
	if err := json.Unmarshal([]byte(raw), &approval); err != nil || approval.Permissions == nil ||
		!catalogVersion.MatchString(approval.Version) || !catalogImage.MatchString(approval.Image) {
		return errors.New("invalid catalog approval")
	}
	if image != approval.Image {
		return errors.New("running image differs from the approved catalog image")
	}
	m.approval = &approval
	return nil
}

func (m Module) verifyApprovedManifest() error {
	a := m.approval
	if a == nil {
		return nil
	}
	if m.Manifest.Name != m.Name || m.Manifest.Version != a.Version {
		return errors.New("manifest name or version differs from the catalog")
	}
	if !slices.Equal(slices.Sorted(slices.Values(m.Manifest.Permissions)), slices.Sorted(slices.Values(a.Permissions))) {
		return errors.New("manifest permissions differ from the catalog")
	}
	return nil
}

func (r *Registry) moduleToken(m Module) string {
	if m.approval == nil {
		return r.Token(m.Name)
	}
	mac := hmac.New(sha256.New, r.secret)
	fmt.Fprintf(mac, "catalog-module:%s:%s:%s", m.Name, m.serviceID, m.approval.Image)
	return hex.EncodeToString(mac.Sum(nil))
}

func (r *Registry) Authenticate(ctx context.Context, name, token string) (Module, bool) {
	if !r.ValidToken(name, token) {
		r.mu.Lock()
		cached := slices.ContainsFunc(r.cached, func(m Module) bool {
			return m.Name == name && m.approval != nil && hmac.Equal([]byte(r.moduleToken(m)), []byte(token))
		})
		r.mu.Unlock()
		if !cached {
			return Module{}, false
		}
	}
	m, ok := r.Get(ctx, name)
	return m, ok && m.OK() && hmac.Equal([]byte(r.moduleToken(m)), []byte(token))
}
