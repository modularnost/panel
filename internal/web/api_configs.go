package web

import (
	"context"
	"net/http"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type configValue struct {
	Name string `json:"name" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$"`
	Data []byte `json:"data" format:"byte" doc:"Base64-encoded configuration file"`
}

type configDetail struct {
	docker.Config
	Data []byte `json:"data" format:"byte"`
}

func (s *Server) registerConfigs(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	huma.Register(api, huma.Operation{OperationID: "list-configs", Method: http.MethodGet, Path: "/api/v1/configs", Summary: "List configs and service attachments", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, _ *struct{}) (*struct {
			Body []docker.Config `nullable:"false"`
		}, error) {
			list, err := s.Docker.Configs(ctx)
			return &struct {
				Body []docker.Config `nullable:"false"`
			}{list}, configProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "inspect-config", Method: http.MethodGet, Path: "/api/v1/configs/{id}", Summary: "Read config metadata, contents and service attachments", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *configIDInput) (*struct{ Body configDetail }, error) {
			row, data, err := s.Docker.InspectConfig(ctx, in.ID)
			return &struct{ Body configDetail }{configDetail{row, data}}, configProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "create-config", Method: http.MethodPost, Path: "/api/v1/configs", Summary: "Create an immutable config version", DefaultStatus: http.StatusCreated, MaxBodyBytes: 700 << 10, Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *struct{ Body configValue }) (*configCreatedOutput, error) {
			id, err := s.Docker.CreateConfig(ctx, in.Body.Name, in.Body.Data)
			return configCreated(id), configProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "remove-config", Method: http.MethodDelete, Path: "/api/v1/configs/{id}", Summary: "Remove an unused config", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *configIDInput) (*struct{}, error) {
			return nil, configProblem(s.Docker.RemoveConfig(ctx, in.ID))
		})
	huma.Register(api, huma.Operation{OperationID: "set-service-config", Method: http.MethodPut, Path: "/api/v1/services/{id}/configs", Summary: "Attach, replace or detach the config at a container path", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *configAttachmentInput) (*struct{}, error) {
			return nil, configProblem(s.Docker.SetServiceConfig(ctx, in.ID, in.Body.Target, in.Body.Resource))
		})
}

type configIDInput struct {
	ID string `path:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$"`
}
type configCreatedOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

func configCreated(id string) *configCreatedOutput {
	out := &configCreatedOutput{}
	out.Body.ID = id
	return out
}

type configAttachmentInput struct {
	ID   string `path:"id"`
	Body struct {
		Target   string `json:"target" minLength:"1" maxLength:"4096"`
		Resource string `json:"resource,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$" doc:"Resource name or ID. Omit to detach the resource at this target"`
	}
}

func configProblem(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errdefs.IsInvalidArgument(err):
		return problem(http.StatusUnprocessableEntity, "invalid_input", "Invalid resource parameters")
	case errdefs.IsNotFound(err):
		return problem(http.StatusNotFound, "not_found", "Resource, node or service not found")
	case errdefs.IsConflict(err), errdefs.IsFailedPrecondition(err):
		return problem(http.StatusConflict, "resource_conflict", "Resource already exists, is in use, or changed. Refresh and try again")
	case errdefs.IsUnavailable(err):
		return problem(http.StatusServiceUnavailable, "resource_unavailable", "The resource or its node is unavailable")
	default:
		return problem(http.StatusBadGateway, "docker_error", "Docker could not complete the resource operation")
	}
}
