package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiRedeploy struct {
	Service   string   `json:"service"`
	OldDigest string   `json:"old_digest,omitempty"`
	NewDigest string   `json:"new_digest,omitempty"`
	Warnings  []string `json:"warnings" nullable:"false"`
}

type apiTraefik struct {
	Host         string `json:"host" doc:"Host name; empty for none"`
	Port         int    `json:"port" doc:"Container port; 0 for none"`
	Entrypoint   string `json:"entrypoint" doc:"Letters, digits, hyphens and underscores"`
	CertResolver string `json:"cert_resolver" doc:"Empty serves plain HTTP"`
}

func toAPITraefik(sv docker.Service) apiTraefik {
	f := traefikFrom(sv)
	port, _ := strconv.Atoi(f.Port)
	return apiTraefik{Host: f.Host, Port: port, Entrypoint: f.Entrypoint, CertResolver: f.CertResolv}
}

type redeployOutput struct{ Body apiRedeploy }
type traefikOutput struct{ Body apiTraefik }

func (s *Server) apiFindService(ctx context.Context, id string) (docker.Service, error) {
	svcs, err := s.Docker.Services(ctx)
	if err != nil {
		return docker.Service{}, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
	}
	for _, sv := range svcs {
		if sv.ID == id || sv.Name == id {
			return sv, nil
		}
	}
	return docker.Service{}, problem(http.StatusNotFound, "not_found", "no such service")
}

func (s *Server) registerServiceActions(api huma.API) {
	operator := huma.Middlewares{s.humaAuth(api, store.RoleOperator)}
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "redeploy-service", Method: http.MethodPost, Path: "/api/v1/services/{id}/redeploy",
		Summary: "Pull the image again and replace the replicas", Middlewares: operator, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*redeployOutput, error) {
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		me, _ := ctx.Value(ctxKey{}).(store.User)
		finish, _ := s.DB.StartDeploy(store.DeployEvent{
			ServiceName: sv.Name, StackName: sv.Stack, Source: "api", TriggeredBy: me.Username})
		d, err := s.Docker.ForceUpdate(ctx, sv.ID)
		finish(d.OldDigest, d.NewDigest, err)
		logWarnings(d)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		out := apiRedeploy{Service: sv.Name, OldDigest: d.OldDigest, NewDigest: d.NewDigest, Warnings: []string{}}
		out.Warnings = append(out.Warnings, d.Warnings...)
		return &redeployOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-service", Method: http.MethodDelete, Path: "/api/v1/services/{id}",
		Summary: "Remove a service", DefaultStatus: http.StatusNoContent, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*struct{}, error) {
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if err := s.Docker.RemoveService(ctx, sv.ID); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-service-traefik", Method: http.MethodGet, Path: "/api/v1/services/{id}/traefik",
		Summary: "The Traefik routing the service's labels describe", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*traefikOutput, error) {
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		return &traefikOutput{Body: toAPITraefik(sv)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-service-traefik", Method: http.MethodPut, Path: "/api/v1/services/{id}/traefik",
		Summary:     "Write the Traefik labels; an empty host and port take the service off Traefik",
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Body apiTraefik
	}) (*traefikOutput, error) {
		port := ""
		if in.Body.Port != 0 {
			port = strconv.Itoa(in.Body.Port)
		}
		if err := validateTraefik(in.Body.Host, port, in.Body.Entrypoint, in.Body.CertResolver); err != nil {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
		}
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		labels := traefikLabels(sv.Name, in.Body.Host, port, in.Body.Entrypoint, in.Body.CertResolver)
		if err := s.Docker.SetLabels(ctx, sv.ID, labels); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		return &traefikOutput{Body: in.Body}, nil
	})
}
