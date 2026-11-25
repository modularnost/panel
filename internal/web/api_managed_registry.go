package web

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

type ManagedRegistryPlan struct {
	Version        string `json:"version" minLength:"1"`
	Revision       string `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Endpoint       string `json:"endpoint" minLength:"1" maxLength:"255"`
	BuildNetwork   string `json:"build_network" minLength:"1" maxLength:"63"`
	PullNetwork    string `json:"pull_network" minLength:"1" maxLength:"63"`
	PullEntrypoint string `json:"pull_entrypoint,omitempty" maxLength:"63" doc:"Traefik Swarm private TCP entry point; omit for an external passthrough router"`
}

type managedRegistryProvisionBody struct {
	ManagedRegistryPlan
	Secrets map[string]string `json:"secrets,omitempty" writeOnly:"true" doc:"PEM and bcrypt-user file contents keyed by their Builder Swarm secret names"`
}

type managedRegistryProvision struct {
	ID   string `path:"id" maxLength:"63"`
	Body managedRegistryProvisionBody
}

type ManagedRegistryVerifyBody struct {
	ManagedRegistryPlan
	Worker     string `json:"worker" pattern:"^[a-z0-9]+$" maxLength:"64"`
	ProbeImage string `json:"probe_image" minLength:"1" maxLength:"512"`
	Username   string `json:"username"`
	Token      string `json:"token" writeOnly:"true"`
}

type managedRegistryVerify struct {
	ID   string `path:"id" maxLength:"63"`
	Body ManagedRegistryVerifyBody
}

func (s *Server) managedRegistryInputs(ctx context.Context, id string, in ManagedRegistryPlan, provision bool) (modules.CatalogEntry, docker.ManagedRegistry, error) {
	entry, err := s.catalogEntryForInstall(id)
	if err != nil {
		return entry, docker.ManagedRegistry{}, err
	}
	if id != "builder" || entry.Install.Kind != modules.CatalogSwarmStack {
		return entry, docker.ManagedRegistry{}, problem(http.StatusUnprocessableEntity, "registry_unavailable", "Managed registry provisioning requires an approved Builder stack")
	}
	confirmation := apiCatalogInstall{Version: in.Version, Revision: in.Revision}
	if err := confirmation.confirm(entry); err != nil {
		return entry, docker.ManagedRegistry{}, err
	}
	if s.RegistryEncryption == nil {
		return entry, docker.ManagedRegistry{}, problem(http.StatusServiceUnavailable, "registry_not_configured", "Configure encrypted registry credentials first")
	}
	endpoint, err := url.Parse("https://" + in.Endpoint)
	if err != nil || !validRegistryHost(in.Endpoint) || endpoint.Host != in.Endpoint ||
		!strings.Contains(endpoint.Hostname(), ".") || net.ParseIP(endpoint.Hostname()) != nil || endpoint.Hostname() == "docker.io" {
		return entry, docker.ManagedRegistry{}, problem(http.StatusUnprocessableEntity, "invalid_input", "Enter a registry DNS name with an optional port")
	}
	if port := endpoint.Port(); port != "" {
		if value, err := strconv.ParseUint(port, 10, 16); err != nil || value == 0 {
			return entry, docker.ManagedRegistry{}, problem(http.StatusUnprocessableEntity, "invalid_input", "Enter a valid endpoint port")
		}
	}
	if in.BuildNetwork == in.PullNetwork || !docker.ValidName(in.BuildNetwork) || !docker.ValidName(in.PullNetwork) {
		return entry, docker.ManagedRegistry{}, problem(http.StatusUnprocessableEntity, "invalid_input", "Select separate build and private routing overlays")
	}
	if in.PullEntrypoint != "" && !docker.ValidName(in.PullEntrypoint) {
		return entry, docker.ManagedRegistry{}, problem(http.StatusUnprocessableEntity, "invalid_input", "Traefik routing requires a named private entry point and registry DNS name")
	}
	nets, err := s.Docker.Networks(ctx)
	if err != nil {
		return entry, docker.ManagedRegistry{}, backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	for _, name := range []string{in.BuildNetwork, in.PullNetwork} {
		isBuild := name == in.BuildNetwork
		found := slices.ContainsFunc(nets, func(n docker.Network) bool { return n.Name == name })
		matches := slices.ContainsFunc(nets, func(n docker.Network) bool {
			return n.Name == name && n.Driver == "overlay" && !n.Protected && (!isBuild || n.Internal)
		})
		if matches {
			continue
		}
		if !provision || found {
			return entry, docker.ManagedRegistry{}, problem(http.StatusConflict, "registry_network_unavailable", "Select an internal attachable build overlay and a separate private routing overlay")
		}
		if _, err := s.Docker.CreateNetwork(ctx, docker.NewNetwork{Name: name, Internal: true, Attachable: true}); err != nil {
			return entry, docker.ManagedRegistry{}, backendProblem(http.StatusBadGateway, "registry_network_unavailable", err)
		}
	}
	node, err := s.Docker.LocalSwarmNode(ctx)
	if err != nil || node == "" {
		return entry, docker.ManagedRegistry{}, problem(http.StatusServiceUnavailable, "registry_node_unavailable", "The panel's Swarm manager is unavailable")
	}
	return entry, docker.ManagedRegistry{Endpoint: in.Endpoint, Hostname: endpoint.Hostname(), BuildNetwork: in.BuildNetwork, PullNetwork: in.PullNetwork, PullEntrypoint: in.PullEntrypoint, Node: node}, nil
}

func (s *Server) registerManagedRegistry(api huma.API, admin huma.Middlewares) {
	const path = "/api/v1/module-catalog/{id}/registry"
	huma.Register(api, huma.Operation{OperationID: "provision-module-registry", Method: http.MethodPut, Path: path, Summary: "Provision the registry and private pull proxy from the approved Builder bundle", MaxBodyBytes: 768 << 10, DefaultStatus: http.StatusNoContent, Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *managedRegistryProvision) (*struct{}, error) {
		s.moduleMu.Lock()
		defer s.moduleMu.Unlock()
		entry, plan, err := s.managedRegistryInputs(ctx, in.ID, in.Body.ManagedRegistryPlan, true)
		if err != nil {
			return nil, err
		}
		node, err := s.DB.ModuleStorageNode(ctx, "registry:"+in.ID, plan.Node)
		if err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		if node != plan.Node {
			return nil, problem(http.StatusConflict, "registry_node_unavailable", "The original registry storage node is unavailable from this panel")
		}
		if err := s.Docker.EnsureBuilderSecrets(ctx, in.Body.Secrets); err != nil {
			if errdefs.IsInvalidArgument(err) || errdefs.IsConflict(err) {
				return nil, problem(http.StatusUnprocessableEntity, "builder_secret_conflict", err.Error())
			}
			return nil, problem(http.StatusBadGateway, "docker_error", "Docker could not provision the Builder secrets")
		}
		if err := s.Docker.ProvisionManagedRegistry(ctx, entry.Install.Image, plan); err != nil {
			if errdefs.IsConflict(err) {
				return nil, problem(http.StatusConflict, "registry_conflict", "A conflicting registry service or storage volume exists")
			}
			return nil, backendProblem(http.StatusBadGateway, "registry_provision_failed", err)
		}
		return nil, nil
	})
	huma.Register(api, huma.Operation{OperationID: "verify-module-registry", Method: http.MethodPost, Path: path + "/verify", Summary: "Verify a worker pull and save credentials for the exact endpoint", DefaultStatus: http.StatusNoContent, Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *managedRegistryVerify) (*struct{}, error) {
		s.moduleMu.Lock()
		defer s.moduleMu.Unlock()
		return s.verifyManagedRegistry(ctx, in)
	})
}

func (s *Server) verifyManagedRegistry(ctx context.Context, in *managedRegistryVerify) (*struct{}, error) {
	b := in.Body
	b.Username = strings.TrimSpace(b.Username)
	if !docker.ValidRegistryProbe(b.ProbeImage, b.Endpoint) || b.Username == "" || len(b.Username) > 255 ||
		strings.ContainsAny(b.Username, ":\x00\r\n") || strings.TrimSpace(b.Token) == "" ||
		len(b.Token) > 4096 || strings.ContainsAny(b.Token, "\x00\r\n") {
		return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "Enter a digest-pinned probe from the exact endpoint and valid pull credentials")
	}
	entry, plan, err := s.managedRegistryInputs(ctx, in.ID, b.ManagedRegistryPlan, false)
	if err != nil {
		return nil, err
	}
	installed, err := s.Docker.ManagedRegistryExists(ctx, entry.Install.Image, plan)
	if err != nil || !installed {
		return nil, problem(http.StatusConflict, "registry_unavailable", "Provision the matching registry before verifying a pull")
	}
	nodes, err := s.Docker.Nodes(ctx)
	if err != nil {
		return nil, backendProblem(http.StatusBadGateway, "docker_error", err)
	}
	addrs, err := s.Docker.AgentNetworkAddresses(ctx, s.AgentService)
	if err != nil || s.Metrics == nil || !addrs[b.Worker].IsValid() || !slices.ContainsFunc(nodes, func(n docker.Node) bool {
		return n.ID == b.Worker && n.Role == "worker" && n.Healthy()
	}) {
		return nil, problem(http.StatusServiceUnavailable, "node_unavailable", "Select an active Swarm worker with a running panel agent")
	}
	encoded, err := authconfig.Encode(registry.AuthConfig{ServerAddress: b.Endpoint, Username: b.Username, Password: b.Token})
	if err != nil {
		return nil, problem(http.StatusInternalServerError, "internal", "Could not encode registry credentials")
	}
	result, err := s.Metrics.VerifyRegistry(ctx, addrs[b.Worker], b.Endpoint, b.ProbeImage, encoded)
	if err != nil {
		return nil, problem(http.StatusServiceUnavailable, "node_unavailable", "The worker agent did not complete the registry probe")
	}
	switch result {
	case "verified":
	case "anonymous_pull_allowed":
		return nil, problem(http.StatusConflict, result, "The worker could pull without credentials")
	case "authorized_pull_failed":
		return nil, problem(http.StatusConflict, result, "Anonymous pulls are denied, but the worker could not pull the probe with these credentials")
	default:
		return nil, problem(http.StatusConflict, "registry_unverified", "The worker could not confirm that the TLS endpoint rejects anonymous pulls. Check routing, trust and the registry response")
	}
	if err := s.DB.SaveRegistry(store.Registry{Host: b.Endpoint, Username: b.Username}, b.Token, s.RegistryEncryption); err != nil {
		return nil, problem(http.StatusInternalServerError, "internal", "Could not save the verified registry credentials")
	}
	return nil, nil
}
