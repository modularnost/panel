package web

import (
	"context"
	"net/http"

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
			}{list}, resourceProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "inspect-config", Method: http.MethodGet, Path: "/api/v1/configs/{id}", Summary: "Read config metadata, contents and service attachments", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *resourceIDInput) (*struct{ Body configDetail }, error) {
			row, data, err := s.Docker.InspectConfig(ctx, in.ID)
			return &struct{ Body configDetail }{configDetail{row, data}}, resourceProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "create-config", Method: http.MethodPost, Path: "/api/v1/configs", Summary: "Create an immutable config version", DefaultStatus: http.StatusCreated, MaxBodyBytes: 700 << 10, Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *struct{ Body configValue }) (*resourceCreatedOutput, error) {
			id, err := s.Docker.CreateConfig(ctx, in.Body.Name, in.Body.Data)
			return resourceCreated(id), resourceProblem(err)
		})
	huma.Register(api, huma.Operation{OperationID: "remove-config", Method: http.MethodDelete, Path: "/api/v1/configs/{id}", Summary: "Remove an unused config", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *resourceIDInput) (*struct{}, error) {
			return nil, resourceProblem(s.Docker.RemoveConfig(ctx, in.ID))
		})
	huma.Register(api, huma.Operation{OperationID: "set-service-config", Method: http.MethodPut, Path: "/api/v1/services/{id}/configs", Summary: "Attach, replace or detach the config at a container path", Middlewares: admin, Security: signedIn},
		func(ctx context.Context, in *resourceAttachmentInput) (*struct{}, error) {
			return nil, resourceProblem(s.Docker.SetServiceConfig(ctx, in.ID, in.Body.Target, in.Body.Resource))
		})
}
