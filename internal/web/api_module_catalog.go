package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/modules"
	"modularnost/internal/store"
)

type catalogScreenshot struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

type catalogEntry struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	Summary         string              `json:"summary"`
	Maintainer      string              `json:"maintainer"`
	SourceURL       string              `json:"source_url"`
	DocsURL         string              `json:"docs_url,omitempty"`
	Screenshots     []catalogScreenshot `json:"screenshots" nullable:"false"`
	Permissions     []string            `json:"permissions" nullable:"false"`
	Version         string              `json:"version,omitempty"`
	Revision        string              `json:"revision"`
	Incompatibility string              `json:"incompatibility,omitempty"`
	InstructionsURL string              `json:"instructions_url,omitempty"`
	MountPath       string              `json:"mount_path,omitempty"`
}

type moduleCatalogOutput struct {
	Body struct {
		Modules   []catalogEntry `json:"modules" nullable:"false"`
		CheckedAt *time.Time     `json:"checked_at,omitempty"`
		State     string         `json:"state" enum:"ready,stale,unavailable"`
	}
}

type apiCatalogInstall struct {
	Secrets  map[string]catalogSecretInput     `json:"secrets,omitempty" doc:"Bindings for declared secret aliases"`
	Volume   *catalogVolumeSelection           `json:"volume,omitempty"`
	Volumes  map[string]catalogVolumeSelection `json:"volumes,omitempty"`
	Version  string                            `json:"version" minLength:"1"`
	Revision string                            `json:"revision" pattern:"^[a-f0-9]{64}$"`
}

type catalogVolumeSelection struct {
	Node string `json:"node" pattern:"^[a-z0-9]{1,64}$"`
	Name string `json:"name,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$" doc:"Existing volume name. Omit to create the catalog default on this node"`
}

type catalogInstallInput struct {
	ID   string `path:"id" maxLength:"63"`
	Body apiCatalogInstall
}

type catalogPreflightOutput struct {
	Body struct {
		PanelNetwork string `json:"panel_network"`
	}
}

func (s *Server) catalogOutput() *moduleCatalogOutput {
	snapshot := s.Catalog.Status()
	out := &moduleCatalogOutput{}
	out.Body.Modules = make([]catalogEntry, 0, len(snapshot.Entries))
	out.Body.State = "unavailable"
	if !snapshot.CheckedAt.IsZero() {
		out.Body.CheckedAt = &snapshot.CheckedAt
		out.Body.State = "stale"
		if snapshot.Fresh(time.Now()) {
			out.Body.State = "ready"
		}
	}
	for _, entry := range snapshot.Entries {
		card := catalogEntry{
			ID: entry.ID, Title: entry.Title, Summary: entry.Summary, Maintainer: entry.Maintainer,
			SourceURL: entry.SourceURL, DocsURL: entry.DocsURL, Permissions: entry.Permissions,
			Version: entry.Install.Version, Revision: entry.Revision(), Incompatibility: entry.Incompatibility(Version),
			InstructionsURL: entry.Install.InstructionsURL, Screenshots: []catalogScreenshot{},
		}
		if entry.Install.DataVolume != nil {
			card.MountPath = entry.Install.DataVolume.MountPath
		}
		for _, shot := range entry.Screenshots {
			card.Screenshots = append(card.Screenshots, catalogScreenshot{shot.URL(), shot.Alt})
		}
		out.Body.Modules = append(out.Body.Modules, card)
	}
	return out
}

func (s *Server) catalogEntryForInstall(id string) (modules.CatalogEntry, error) {
	snapshot := s.Catalog.Status()
	if !snapshot.Fresh(time.Now()) {
		return modules.CatalogEntry{}, problem(http.StatusConflict, "catalog_stale", "Refresh the catalog before installing")
	}
	for _, entry := range snapshot.Entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return modules.CatalogEntry{}, problem(http.StatusConflict, "catalog_changed", "This module is no longer in the catalog")
}

func (in apiCatalogInstall) confirm(entry modules.CatalogEntry) error {
	if entry.Install.Version != in.Version || entry.Revision() != in.Revision {
		return problem(http.StatusConflict, "catalog_changed", "The catalog entry changed. Review it before installing")
	}
	if reason := entry.Incompatibility(Version); reason != "" {
		return problem(http.StatusConflict, "module_incompatible", reason)
	}
	return nil
}

func (s *Server) registerModuleCatalog(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	s.registerManagedRegistry(api, admin)
	huma.Register(api, huma.Operation{
		OperationID: "get-module-catalog", Method: http.MethodGet, Path: "/api/v1/module-catalog",
		Summary: "Curated modules and catalog freshness", Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, _ *struct{}) (*moduleCatalogOutput, error) {
		return s.catalogOutput(), nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "preflight-catalog-module", Method: http.MethodPost, Path: "/api/v1/module-catalog/{id}/preflight",
		Summary: "Verify module stack prerequisites and read the approved deployment bundle", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *catalogInstallInput) (*catalogPreflightOutput, error) {
		entry, err := s.catalogEntryForInstall(in.ID)
		if err != nil {
			return nil, err
		}
		if entry.Install.Kind != modules.CatalogSwarmStack {
			return nil, problem(http.StatusUnprocessableEntity, "preflight_unavailable", "Preflight is available for Swarm stack entries")
		}
		if err := in.Body.confirm(entry); err != nil {
			return nil, err
		}
		_, _, network, err := s.prepareCatalogStack(ctx, entry, nil, false)
		if err != nil {
			return nil, err
		}
		out := &catalogPreflightOutput{}
		out.Body.PanelNetwork = network
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "refresh-module-catalog", Method: http.MethodPost, Path: "/api/v1/module-catalog/refresh",
		Summary: "Refresh the curated module catalog", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*moduleCatalogOutput, error) {
		if s.Catalog != nil {
			if err := s.Catalog.Refresh(ctx); err != nil && ctx.Err() == nil {
				log.Printf("refreshing module catalog: %v", err)
			}
		}
		return s.catalogOutput(), nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "install-catalog-module", Method: http.MethodPost, Path: "/api/v1/module-catalog/{id}/install",
		Summary: "Install the approved module version", DefaultStatus: http.StatusCreated, MaxBodyBytes: 4 << 20, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *catalogInstallInput) (*moduleInstalledOutput, error) {
		entry, err := s.catalogEntryForInstall(in.ID)
		if err != nil {
			return nil, err
		}
		if entry.Install.Kind == "manual" {
			return nil, problem(http.StatusUnprocessableEntity, "manual_install", "Follow this module's installation guide")
		}
		if err := in.Body.confirm(entry); err != nil {
			return nil, err
		}
		if entry.Install.Kind == modules.CatalogSwarmStack {
			if in.Body.Volume != nil {
				return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "Use the volumes mapping for a catalog stack")
			}
			service, err := s.installCatalogStack(ctx, entry, in.Body.Secrets, in.Body.Volumes)
			if err != nil {
				return nil, err
			}
			return &moduleInstalledOutput{Body: apiModuleInstalled{Name: entry.ID, Service: service}}, nil
		}
		if len(in.Body.Secrets) > 0 {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "Secret aliases require a catalog stack")
		}
		m := moduleInstall{Name: entry.ID, Image: entry.Install.Image}
		if entry.Install.Port != 0 {
			m.Port = strconv.Itoa(entry.Install.Port)
		}
		if len(in.Body.Volumes) > 0 || in.Body.Volume != nil && entry.Install.DataVolume == nil {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "This entry does not declare the selected volumes")
		}
		service, err := s.deployModule(ctx, m, "", "", &entry, in.Body.Volume)
		if errors.Is(err, errModuleExists) || errors.Is(err, errModuleStorage) {
			return nil, problem(http.StatusConflict, "module_unavailable", err.Error())
		}
		if err != nil {
			return nil, backendProblem(http.StatusBadGateway, "docker_error", err)
		}
		return &moduleInstalledOutput{Body: apiModuleInstalled{Name: m.Name, Service: service}}, nil
	})
}
