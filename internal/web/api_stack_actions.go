package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiStackResult struct {
	Name   string `json:"name"`
	Output string `json:"output,omitempty" doc:"What docker stack deploy printed"`
}

type apiStackCompose struct {
	Compose string `json:"compose" doc:"The compose file; services missing from it are removed"`
}

type stackResultOutput struct{ Body apiStackResult }

func (s *Server) registerStackActions(api huma.API) {
	operator := huma.Middlewares{s.humaAuth(api, store.RoleOperator)}
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "deploy-stack", Method: http.MethodPut, Path: "/api/v1/stacks/{name}",
		Summary:     "Create a stack or update it from a compose file, and keep the file",
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
		Body apiStackCompose
	}) (*stackResultOutput, error) {
		if !docker.ValidName(in.Name) {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "name: letters, digits, dot, dash and underscore only")
		}
		if strings.TrimSpace(in.Body.Compose) == "" {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "compose: the file is empty")
		}
		me, _ := ctx.Value(ctxKey{}).(store.User)
		finish, _ := s.DB.StartDeploy(store.DeployEvent{
			StackName: in.Name, Source: "api", TriggeredBy: me.Username})
		out, err := s.Docker.DeployStack(ctx, in.Name, in.Body.Compose)
		finish("", "", err)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "deploy_failed", err.Error())
		}
		// Only a file that actually deployed is kept.
		if err := s.DB.SaveStack(in.Name, in.Body.Compose, me.Username); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &stackResultOutput{Body: apiStackResult{Name: in.Name, Output: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "redeploy-stack", Method: http.MethodPost, Path: "/api/v1/stacks/{name}/redeploy",
		Summary:     "Deploy the stored compose file again, or force-update the services of an adopted stack",
		Middlewares: operator, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
	}) (*stackResultOutput, error) {
		me, _ := ctx.Value(ctxKey{}).(store.User)
		finish, _ := s.DB.StartDeploy(store.DeployEvent{
			StackName: in.Name, Source: "api", TriggeredBy: me.Username})

		var out string
		var err error
		if st, ok := s.DB.Stack(in.Name); ok {
			out, err = s.Docker.DeployStack(ctx, in.Name, st.Compose)
		} else {
			var done []docker.Deploy
			done, err = s.Docker.RedeployStack(ctx, in.Name)
			logWarnings(done...)
		}
		finish("", "", err)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "deploy_failed", err.Error())
		}
		return &stackResultOutput{Body: apiStackResult{Name: in.Name, Output: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-stack", Method: http.MethodDelete, Path: "/api/v1/stacks/{name}",
		Summary: "Remove every service of the stack and forget its compose file", DefaultStatus: http.StatusNoContent,
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
	}) (*struct{}, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		live := false
		for _, sv := range svcs {
			live = live || sv.Stack == in.Name
		}
		_, stored := s.DB.Stack(in.Name)
		if !live && !stored {
			return nil, problem(http.StatusNotFound, "not_found", "no such stack")
		}
		if live {
			if err := s.Docker.RemoveStack(ctx, in.Name); err != nil {
				return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
			}
		}
		if err := s.DB.DeleteStack(in.Name); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})
}
