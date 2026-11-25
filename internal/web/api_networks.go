package web

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type networkListOutput struct {
	Body []docker.Network `nullable:"false"`
}
type networkOutput struct{ Body docker.Network }
type networkCreated struct {
	Body struct {
		ID string `json:"id"`
	}
}
type networkPath struct {
	ID string `path:"id"`
}
type serviceNetworkOutput struct{ Body docker.ServiceNetworkState }
type networkChanges struct {
	Version string   `json:"version" minLength:"1"`
	Add     []string `json:"add" maxItems:"64" nullable:"false"`
	Remove  []string `json:"remove" maxItems:"64" nullable:"false"`
	Preview bool     `json:"preview,omitempty"`
}
type networkChangeResult struct {
	Body struct {
		Compose string `json:"compose,omitempty"`
	}
}

func networkProblem(err error) error {
	switch {
	case errdefs.IsUnavailable(err):
		return problem(http.StatusServiceUnavailable, "node_unavailable", "the selected node agent is unavailable")
	case errdefs.IsNotFound(err):
		return problem(http.StatusNotFound, "not_found", "network or service not found")
	case errors.Is(err, docker.ErrProtectedNetwork):
		return problem(http.StatusConflict, "network_protected", err.Error())
	case errors.Is(err, docker.ErrNetworkConflict), errdefs.IsConflict(err), errdefs.IsFailedPrecondition(err):
		return problem(http.StatusConflict, "network_conflict", "the network is in use or the service changed. Refresh and try again.")
	case errors.Is(err, docker.ErrInvalidNetwork), errdefs.IsInvalidArgument(err):
		return problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
	default:
		return backendProblem(http.StatusBadGateway, "docker_unavailable", err)
	}
}

func (s *Server) registerNetworks(api huma.API, viewer huma.Middlewares) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	s.registerLocalNetworks(api, viewer, admin)
	huma.Register(api, huma.Operation{OperationID: "list-networks", Method: http.MethodGet, Path: "/api/v1/networks", Summary: "Networks visible to the Swarm manager", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, _ *struct{}) (*networkListOutput, error) {
		networks, err := s.Docker.Networks(ctx)
		if err != nil {
			return nil, networkProblem(err)
		}
		return &networkListOutput{Body: networks}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-network", Method: http.MethodGet, Path: "/api/v1/networks/{id}", Summary: "Network details and attached workloads", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, in *networkPath) (*networkOutput, error) {
		n, err := s.Docker.Network(ctx, in.ID)
		if err != nil {
			return nil, networkProblem(err)
		}
		return &networkOutput{Body: n}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-network", Method: http.MethodPost, Path: "/api/v1/networks", DefaultStatus: http.StatusCreated, Summary: "Create an overlay network", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *struct{ Body docker.NewNetwork }) (*networkCreated, error) {
		id, err := s.Docker.CreateNetwork(ctx, in.Body)
		if err != nil {
			return nil, networkProblem(err)
		}
		out := &networkCreated{}
		out.Body.ID = id
		return out, nil
	})
	huma.Register(api, huma.Operation{OperationID: "remove-network", Method: http.MethodDelete, Path: "/api/v1/networks/{id}", Summary: "Remove an unused overlay network", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *networkPath) (*struct{}, error) {
		if err := s.Docker.RemoveNetwork(ctx, in.ID); err != nil {
			return nil, networkProblem(err)
		}
		return nil, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-service-networks", Method: http.MethodGet, Path: "/api/v1/services/{id}/networks", Summary: "Network attachments for a Swarm service", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, in *networkPath) (*serviceNetworkOutput, error) {
		state, err := s.serviceNetworkState(ctx, in.ID)
		if err != nil {
			return nil, networkProblem(err)
		}
		return &serviceNetworkOutput{Body: state}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "change-service-networks", Method: http.MethodPost, Path: "/api/v1/services/{id}/networks", Summary: "Preview or apply changes to service network attachments", Middlewares: admin, Security: signedIn}, s.changeServiceNetworks)
}

func (s *Server) serviceNetworkState(ctx context.Context, id string) (docker.ServiceNetworkState, error) {
	state, err := s.Docker.ServiceNetworks(ctx, id)
	if err != nil {
		return state, err
	}
	self, _ := s.Docker.Self(ctx)
	if state.Name == self.Service || (state.Stack != "" && state.Stack == self.Stack) {
		state.Source = "system"
	} else if state.Stack != "" {
		state.Source = "external"
		if stored, ok := s.DB.Stack(state.Stack); ok {
			state.Version = fmt.Sprintf("%s-%x", state.Version, sha256.Sum256([]byte(stored.Compose+"\x00"+stored.Environment+"\x00"+stored.RegistryHost)))
			state.Source = "compose"
		}
	}
	return state, nil
}

func (s *Server) changeServiceNetworks(ctx context.Context, in *struct {
	ID   string `path:"id"`
	Body networkChanges
}) (*networkChangeResult, error) {
	state, err := s.serviceNetworkState(ctx, in.ID)
	if err != nil {
		return nil, networkProblem(err)
	}
	if state.Stack != "" {
		defer s.lockStack(state.Stack)()
		state, err = s.serviceNetworkState(ctx, state.ID)
		if err != nil {
			return nil, networkProblem(err)
		}
	}
	if state.Source != "service" && state.Source != "compose" {
		return nil, networkProblem(docker.ErrProtectedNetwork)
	}
	b := in.Body
	if state.Version != b.Version {
		return nil, networkProblem(docker.ErrNetworkConflict)
	}
	networks, err := s.Docker.Networks(ctx)
	if err != nil {
		return nil, networkProblem(err)
	}
	add, remove, err := validateNetworkChanges(state, networks, b)
	if err != nil {
		return nil, networkProblem(err)
	}
	out := &networkChangeResult{}
	if len(add)+len(remove) == 0 {
		return out, nil
	}
	if state.Source == "service" {
		if b.Preview {
			return out, nil
		}
		if err := s.Docker.UpdateServiceNetworks(ctx, state.ID, state.Version, b.Add, b.Remove); err != nil {
			return nil, networkProblem(err)
		}
		return out, nil
	}
	stored, ok := s.DB.Stack(state.Stack)
	if !ok {
		return nil, networkProblem(docker.ErrNetworkConflict)
	}
	service := strings.TrimPrefix(state.Name, state.Stack+"_")
	compose, err := docker.EditStackNetworks(stored.Compose, state.Stack, service, add, remove)
	if err != nil {
		return nil, problem(http.StatusUnprocessableEntity, "compose_networks", err.Error())
	}
	if b.Preview {
		out.Body.Compose = compose
		return out, nil
	}
	values, err := docker.ParseStackEnvironment(stored.Environment)
	if err != nil {
		return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
	}
	auth, err := s.registryCredentials(stored.RegistryHost)
	if err != nil {
		return nil, err
	}
	me, _ := ctx.Value(ctxKey{}).(store.User)
	finish, _ := s.DB.StartDeploy(store.DeployEvent{StackName: state.Stack, Source: "api", TriggeredBy: me.Username})
	_, err = s.Docker.DeployStack(ctx, stored.Name, compose, values, auth)
	finish("", "", err)
	if err != nil {
		return nil, backendProblem(http.StatusBadGateway, "deploy_failed", err)
	}
	stored.Compose, stored.UpdatedBy = compose, me.Username
	if err := s.DB.SaveStack(stored); err != nil {
		return nil, backendProblem(http.StatusInternalServerError, "internal", err)
	}
	return out, nil
}

func validateNetworkChanges(state docker.ServiceNetworkState, networks []docker.Network, b networkChanges) (add, remove []docker.Network, err error) {
	available := make(map[string]docker.Network, len(networks))
	seen := make(map[string]bool)
	for _, n := range networks {
		available[n.ID] = n
	}
	for _, group := range []struct {
		ids []string
		add bool
	}{{b.Add, true}, {b.Remove, false}} {
		for _, id := range group.ids {
			n, ok := available[id]
			attached := slices.ContainsFunc(state.Attachments, func(a docker.NetworkAttachment) bool { return a.ID == id })
			automatic := slices.ContainsFunc(state.Attachments, func(a docker.NetworkAttachment) bool { return a.ID == id && a.Automatic })
			if automatic || seen[id] || !ok || n.Scope != "swarm" || n.Protected || n.ConfigOnly || attached == group.add ||
				(group.add && n.Stack != "" && n.Stack != state.Stack) {
				return nil, nil, docker.ErrInvalidNetwork
			}
			seen[id] = true
			if group.add {
				add = append(add, n)
			} else {
				remove = append(remove, n)
			}
		}
	}
	return add, remove, nil
}
