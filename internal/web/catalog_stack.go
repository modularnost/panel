package web

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
)

const catalogStackLabel = "panel.module.managed-stack"

func catalogStackName(id string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(id)))
	return fmt.Sprintf("modularnost-module-%x", hash[:8])
}

type catalogStackPlan struct {
	compose, network string
	count            int
	configs          []docker.BundleConfig
	configNames      map[string]string
}

func (s *Server) prepareCatalogStack(ctx context.Context, entry modules.CatalogEntry, selected map[string]catalogVolumeSelection, install bool) (catalogStackPlan, error) {
	self, err := s.Docker.Self(ctx)
	if err != nil {
		return catalogStackPlan{}, backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	networks, err := s.Docker.OverlayNetworks(ctx)
	if err != nil {
		return catalogStackPlan{}, backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	if self.Service == "" || !slices.Contains(networks, self.Network) {
		return catalogStackPlan{}, problem(http.StatusConflict, "module_unavailable", "Module stacks require a panel service on a Swarm overlay network")
	}
	bundle, err := s.Docker.ReadModuleBundle(ctx, entry.Install.Image)
	if err != nil {
		log.Printf("reading module deployment bundle: %v", err)
		return catalogStackPlan{}, problem(http.StatusBadGateway, "module_bundle_unavailable", "Could not read the approved module deployment bundle. Check that the image can be pulled on this node")
	}
	if bundle.RegistryCredentials {
		return catalogStackPlan{}, problem(http.StatusUnprocessableEntity, "unsupported_stack", "Managed registry setup is unavailable for this module stack")
	}
	spec := docker.CatalogStack{
		Image:        entry.Install.Image,
		PanelNetwork: self.Network,
		CoreURL:      "http://" + net.JoinHostPort(self.Service, corePort(s.Addr)),
		Labels: map[string]string{
			modules.LabelEnabled:         "true",
			modules.LabelName:            entry.ID,
			modules.LabelPort:            "8080",
			modules.LabelVersion:         entry.Install.Version,
			modules.LabelCatalogApproval: entry.ApprovalLabel(),
			catalogStackLabel:            catalogStackName(entry.ID),
		},
	}
	configs, err := bundle.CatalogConfigFiles()
	if err != nil {
		return catalogStackPlan{}, problem(http.StatusUnprocessableEntity, "unsupported_stack", err.Error())
	}
	spec.ConfigNames = make(map[string]string, len(configs))
	for _, config := range configs {
		spec.ConfigNames[config.Alias] = catalogStackName(entry.ID) + "-" + rand.Text()
	}
	aliases, err := bundle.CatalogVolumeAliases()
	if err != nil {
		return catalogStackPlan{}, problem(http.StatusUnprocessableEntity, "unsupported_stack", err.Error())
	}
	spec.Volumes = make(map[string]docker.CatalogVolume, len(aliases))
	for alias := range selected {
		if !slices.Contains(aliases, alias) {
			return catalogStackPlan{}, problem(http.StatusUnprocessableEntity, "invalid_input", "Select volumes only for declared aliases")
		}
	}
	for _, alias := range aliases {
		key := catalogStackName(entry.ID) + "-" + alias
		spec.Volumes[alias] = docker.CatalogVolume{Name: moduleVolumeName(key)}
	}
	if install && len(aliases) > 0 {
		for _, alias := range aliases {
			var selection *catalogVolumeSelection
			if value, ok := selected[alias]; ok {
				selection = &value
			}
			key := catalogStackName(entry.ID) + "-" + alias
			node, name, err := s.ensureModuleVolume(ctx, key, spec.Volumes[alias].Name, selection)
			if errors.Is(err, errModuleStorage) {
				return catalogStackPlan{}, problem(http.StatusConflict, "module_unavailable", err.Error())
			}
			if err != nil {
				return catalogStackPlan{}, volumeProblem(err)
			}
			spec.Volumes[alias] = docker.CatalogVolume{Name: name, Node: node}
		}
	}
	compose, count, err := docker.RenderCatalogStack(ctx, bundle, spec)
	if err != nil {
		return catalogStackPlan{}, problem(http.StatusUnprocessableEntity, "unsupported_stack", err.Error())
	}
	return catalogStackPlan{compose: compose, network: self.Network, count: count, configs: configs, configNames: spec.ConfigNames}, nil
}

func (s *Server) installCatalogStack(ctx context.Context, entry modules.CatalogEntry, secrets map[string]catalogSecretInput, volumes map[string]catalogVolumeSelection) (_ string, err error) {
	name := catalogStackName(entry.ID)
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	defer s.lockStack(name)()
	services, err := s.Docker.Services(ctx)
	if err != nil {
		return "", backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	for _, svc := range services {
		if svc.Stack == name || strings.HasPrefix(svc.Name, name+"_") ||
			svc.Labels[modules.LabelEnabled] == "true" && strings.EqualFold(cmp.Or(svc.Labels[modules.LabelName], svc.Name), entry.ID) {
			return "", problem(http.StatusConflict, "module_unavailable", "A module or stack with this name is already installed")
		}
	}
	networks, err := s.Docker.OverlayNetworks(ctx)
	if err != nil {
		return "", backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	if slices.Contains(networks, name+"_default") {
		return "", problem(http.StatusConflict, "module_unavailable", "An overlay network with this stack name already exists")
	}
	s.volumeMu.Lock()
	defer s.volumeMu.Unlock()
	plan, err := s.prepareCatalogStack(ctx, entry, volumes, true)
	if err != nil {
		return "", err
	}
	compose, created, err := s.bindCatalogSecrets(ctx, plan.compose, secrets)
	defer func() {
		if err != nil && len(created) != 0 {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			s.removeCatalogSecrets(cleanup, name, created)
		}
	}()
	if err != nil {
		return "", err
	}
	var configIDs []string
	defer func() {
		if err == nil || len(configIDs) == 0 {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, id := range configIDs {
			if err := s.Docker.RemoveConfig(cleanup, id); err != nil && !errdefs.IsNotFound(err) {
				log.Printf("rolling back module stack %s config %s: %v", name, id, err)
			}
		}
	}()
	for _, config := range plan.configs {
		if err := ctx.Err(); err != nil {
			return "", resourceProblem(err)
		}
		createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		id, err := s.Docker.CreateConfig(createCtx, plan.configNames[config.Alias], config.Data)
		cancel()
		if err != nil {
			return "", resourceProblem(err)
		}
		configIDs = append(configIDs, id)
	}
	// Remove only this new stack if deployment stops before readiness.
	rollback := func(cause error) (string, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		defer s.Modules.Forget()
		if err := s.Docker.RemoveCatalogStack(cleanup, name); err != nil {
			log.Printf("rolling back module stack %s: %v", name, err)
			return "", problem(http.StatusBadGateway, "module_rollback_failed", "Could not clean up module stack "+name)
		}
		return "", cause
	}
	if _, err := s.Docker.DeployStack(ctx, name, compose, nil, nil); err != nil {
		log.Printf("deploying module stack %s: %v", name, err)
		return rollback(backendProblem(http.StatusBadGateway, "docker_error", err))
	}
	if err := s.waitForCatalogStack(ctx, entry.ID, name, plan.count); err != nil {
		return rollback(err)
	}
	return name + "_module", nil
}

func (s *Server) waitForCatalogStack(ctx context.Context, id, stack string, expected int) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		services, err := s.Docker.Services(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return backendProblem(http.StatusBadGateway, "docker_error", err)
		}
		ready := 0
		for _, service := range services {
			if service.Stack == stack && service.Healthy() {
				ready++
			}
		}
		if ready == expected {
			s.Modules.Forget()
			if mod, ok := s.Modules.Get(ctx, id); ok && mod.Service == stack+"_module" {
				if mod.CatalogError != "" {
					return problem(http.StatusBadGateway, "module_manifest_mismatch", mod.CatalogError)
				}
				if mod.State == modules.StateReady {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return problem(http.StatusGatewayTimeout, "module_not_ready", "The stack services and the approved module manifest did not become ready")
}
