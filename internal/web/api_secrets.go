package web

import (
	"context"
	"net/http"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type resourceIDInput struct {
	ID string `path:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$"`
}
type resourceCreatedOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

func resourceCreated(id string) *resourceCreatedOutput {
	out := &resourceCreatedOutput{}
	out.Body.ID = id
	return out
}

type secretValue struct {
	Name string `json:"name" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$"`
	Data []byte `json:"data" format:"byte" writeOnly:"true" doc:"Base64-encoded secret payload"`
}

type resourceAttachmentInput struct {
	ID   string `path:"id"`
	Body struct {
		Target   string `json:"target" minLength:"1" maxLength:"4096"`
		Resource string `json:"resource,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$" doc:"Resource name or ID. Omit to detach the resource at this target"`
	}
}

func resourceProblem(err error) error {
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

func (s *Server) registerSecrets(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	huma.Register(api, huma.Operation{OperationID: "list-secrets", Method: http.MethodGet, Path: "/api/v1/secrets", Summary: "List secret metadata and service attachments", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, _ *struct{}) (*struct {
			Body []docker.Secret `nullable:"false"`
		}, error) {
			list, err := s.Docker.Secrets(ctx)
			return &struct {
				Body []docker.Secret `nullable:"false"`
			}{list}, resourceProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "create-secret", Method: http.MethodPost, Path: "/api/v1/secrets", Summary: "Create an immutable secret version", DefaultStatus: http.StatusCreated, MaxBodyBytes: 700 << 10, Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *struct{ Body secretValue }) (*resourceCreatedOutput, error) {
			id, err := s.Docker.CreateSecret(ctx, in.Body.Name, in.Body.Data)
			return resourceCreated(id), resourceProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "remove-secret", Method: http.MethodDelete, Path: "/api/v1/secrets/{id}", Summary: "Remove an unused secret", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *resourceIDInput) (*struct{}, error) {
			return nil, resourceProblem(s.Docker.RemoveSecret(ctx, in.ID))
		})
	huma.Register(api, huma.Operation{OperationID: "set-service-secret", Method: http.MethodPut, Path: "/api/v1/services/{id}/secrets", Summary: "Attach, rotate or detach the secret at a target filename", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *resourceAttachmentInput) (*struct{}, error) {
			return nil, resourceProblem(s.Docker.SetServiceSecret(ctx, in.ID, in.Body.Target, in.Body.Resource))
		})
}
