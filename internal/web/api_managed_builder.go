package web

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/moby/moby/api/types/mount"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
)

type managedBuilderInstallBody struct {
	ManagedRegistryPlan
	Worker      string             `json:"worker" pattern:"^[a-z0-9]+$" maxLength:"64"`
	Username    string             `json:"username"`
	Token       string             `json:"token" writeOnly:"true"`
	BuildKitTLS docker.BuildKitTLS `json:"buildkit_tls" writeOnly:"true"`
}

type managedBuilderInstall struct {
	ID   string `path:"id" maxLength:"63"`
	Body managedBuilderInstallBody
}

func (s *Server) registerManagedBuilder(api huma.API, admin huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "install-managed-builder", Method: http.MethodPost, Path: "/api/v1/module-catalog/{id}/builder",
		Summary:       "Verify the managed registry and install Builder and BuildKit from the approved recipe",
		DefaultStatus: http.StatusCreated, MaxBodyBytes: 256 << 10, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *managedBuilderInstall) (*moduleInstalledOutput, error) {
		service, err := s.installManagedBuilder(ctx, in)
		if err != nil {
			return nil, err
		}
		return &moduleInstalledOutput{Body: apiModuleInstalled{Name: in.ID, Service: service}}, nil
	})
}

func (s *Server) installManagedBuilder(ctx context.Context, in *managedBuilderInstall) (_ string, err error) {
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	entry, plan, err := s.managedRegistryInputs(ctx, in.ID, in.Body.ManagedRegistryPlan, false)
	if err != nil {
		return "", err
	}
	installed, err := s.Docker.ManagedRegistryExists(ctx, entry.Install.Image, plan)
	if err != nil || !installed {
		return "", problem(http.StatusConflict, "registry_unavailable", "Prepare the matching registry before installing Builder")
	}
	stack := catalogStackName(entry.ID)
	defer s.lockStack(stack)()
	name := stack + "_module"
	services, err := s.Docker.Services(ctx)
	if err != nil {
		return "", backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	for _, service := range services {
		if service.Stack != stack && service.Name != name && !strings.EqualFold(service.Labels[modules.LabelName], entry.ID) {
			continue
		}
		if service.Name == name && service.Labels[modules.LabelCatalogApproval] == entry.ApprovalLabel() &&
			service.Labels[modules.LabelPullRegistry] == plan.Endpoint {
			if err := s.waitForCatalogStack(ctx, entry.ID, stack, 1); err != nil {
				return "", err
			}
			if err := s.Docker.VerifyBuilderConnection(ctx, name); err != nil {
				return "", backendProblem(http.StatusBadGateway, "buildkit_unavailable", err)
			}
			return name, nil
		}
		return "", problem(http.StatusConflict, "module_unavailable", "A different Builder installation already exists")
	}
	if err := in.Body.BuildKitTLS.Validate(); err != nil {
		return "", problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
	}
	self, err := s.Docker.Self(ctx)
	if err != nil || self.Service == "" || self.Network == "" {
		return "", problem(http.StatusConflict, "module_unavailable", "Builder requires the panel to run on a Swarm overlay")
	}
	refs, err := s.Docker.BuilderSecrets(ctx)
	if err != nil {
		return "", problem(http.StatusConflict, "builder_tls_unavailable", "Create the Builder module’s three BuildKit client secrets first")
	}
	worker, err := s.Docker.ProvisionBuildKit(ctx, plan.BuildNetwork, in.Body.BuildKitTLS)
	if err != nil {
		if errdefs.IsInvalidArgument(err) {
			return "", problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
		}
		if errdefs.IsConflict(err) {
			return "", resourceProblem(err)
		}
		return "", backendProblem(http.StatusBadGateway, "buildkit_unavailable", err)
	}
	var serviceID string
	const attemptLabel = "panel.builder.install_attempt"
	attempt := rand.Text()
	defer func() {
		if err == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if serviceID == "" {
			svc, found, lookupErr := s.Docker.Service(cleanup, name)
			if lookupErr == nil && found && svc.Labels[attemptLabel] == attempt {
				serviceID = svc.ID
			}
		}
		if serviceID != "" {
			if e := s.Docker.RemoveService(cleanup, serviceID); e != nil && !errdefs.IsNotFound(e) {
				log.Printf("rolling back Builder: %v", e)
				return
			}
		}
		if worker != "" {
			if e := s.Docker.RemoveBuildKit(cleanup, worker); e != nil {
				log.Printf("rolling back BuildKit: %v", e)
			}
		}
		s.Modules.Forget()
	}()
	probe, err := s.Docker.SeedBuilderProbe(ctx, plan.Endpoint)
	if err != nil {
		return "", backendProblem(http.StatusBadGateway, "registry_probe_failed", err)
	}
	if _, err := s.verifyManagedRegistry(ctx, &managedRegistryVerify{ID: in.ID, Body: ManagedRegistryVerifyBody{
		ManagedRegistryPlan: in.Body.ManagedRegistryPlan, Worker: in.Body.Worker, ProbeImage: probe,
		Username: in.Body.Username, Token: in.Body.Token,
	}}); err != nil {
		return "", err
	}
	s.volumeMu.Lock()
	defer s.volumeMu.Unlock()
	dataNode, data, err := s.ensureModuleVolume(ctx, "builder:module", "modularnost-builder-data", &catalogVolumeSelection{Node: plan.Node})
	if errors.Is(err, errModuleStorage) {
		return "", problem(http.StatusConflict, "module_unavailable", err.Error())
	}
	if err != nil {
		return "", volumeProblem(err)
	}
	serviceID, err = s.Docker.CreateService(ctx, docker.NewService{
		Name: name, Image: entry.Install.Image, NodeID: dataNode, Networks: []string{self.Network, plan.BuildNetwork}, Secrets: refs,
		Env:    []string{"PANEL_CORE_URL=http://" + net.JoinHostPort(self.Service, corePort(s.Addr)), "REGISTRY_PULL=" + plan.Endpoint},
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: data, Target: "/data"}},
		Labels: map[string]string{
			"com.docker.stack.namespace": stack, catalogStackLabel: stack, attemptLabel: attempt,
			modules.LabelEnabled: "true", modules.LabelName: entry.ID, modules.LabelVersion: entry.Install.Version,
			modules.LabelPort: "8080", modules.LabelCatalogApproval: entry.ApprovalLabel(), modules.LabelPullRegistry: plan.Endpoint,
		},
	})
	if err != nil {
		return "", backendProblem(http.StatusBadGateway, "builder_install_failed", err)
	}
	if err := s.waitForCatalogStack(ctx, entry.ID, stack, 1); err != nil {
		return "", err
	}
	if err := s.Docker.VerifyBuilderConnection(ctx, name); err != nil {
		return "", backendProblem(http.StatusBadGateway, "buildkit_unavailable", err)
	}
	return name, nil
}
