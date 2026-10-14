package web

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/store"
)

type apiWebhookCreate struct {
	Action  string `json:"action" enum:"force_update,stack_redeploy"`
	Service string `json:"service,omitempty" doc:"Required for force_update"`
	Stack   string `json:"stack,omitempty" doc:"Required for stack_redeploy"`
}

type apiModuleInstall struct {
	Name    string   `json:"name" doc:"Shown in the panel and used in the module's URL"`
	Image   string   `json:"image"`
	Port    string   `json:"port,omitempty" doc:"The module's HTTP port; 8080 when empty"`
	Service string   `json:"service,omitempty" doc:"Swarm service name; <name>_module when empty"`
	Network string   `json:"network,omitempty" doc:"Overlay network; the panel's own when empty"`
	Env     []string `json:"env,omitempty" doc:"KEY=value entries" nullable:"false"`
}

type apiModuleInstalled struct {
	Name    string `json:"name"`
	Service string `json:"service"`
}

type webhookOutput struct{ Body apiWebhook }
type moduleInstalledOutput struct{ Body apiModuleInstalled }

func (s *Server) registerManage(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "create-webhook", Method: http.MethodPost, Path: "/api/v1/webhooks",
		Summary: "Create a webhook; its path carries the secret", DefaultStatus: http.StatusCreated,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct{ Body apiWebhookCreate }) (*webhookOutput, error) {
		b := in.Body
		switch {
		case b.Action == "force_update" && (b.Service == "" || b.Stack != ""):
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "force_update takes a service and no stack")
		case b.Action == "stack_redeploy" && (b.Stack == "" || b.Service != ""):
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "stack_redeploy takes a stack and no service")
		}
		hook, err := s.DB.CreateWebhook(store.Webhook{
			ServiceName: b.Service, StackName: b.Stack, ActionType: b.Action,
		})
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &webhookOutput{Body: toAPIWebhooks([]store.Webhook{hook})[0]}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-webhook", Method: http.MethodDelete, Path: "/api/v1/webhooks/{id}",
		Summary: "Delete a webhook", DefaultStatus: http.StatusNoContent, Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct {
		ID int64 `path:"id"`
	}) (*struct{}, error) {
		hooks, err := s.DB.Webhooks()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		found := false
		for _, h := range hooks {
			found = found || h.ID == in.ID
		}
		if !found {
			return nil, problem(http.StatusNotFound, "not_found", "no such webhook")
		}
		if err := s.DB.DeleteWebhook(in.ID); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "install-module", Method: http.MethodPost, Path: "/api/v1/modules",
		Summary: "Deploy a module image as a service the panel will pick up", DefaultStatus: http.StatusCreated,
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct{ Body apiModuleInstall }) (*moduleInstalledOutput, error) {
		m := moduleInstall(in.Body)
		if err := m.validate(); err != nil {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
		}
		if err := s.deployModule(ctx, m); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		service := m.Service
		if service == "" {
			service = m.Name + "_module"
		}
		return &moduleInstalledOutput{Body: apiModuleInstalled{Name: m.Name, Service: service}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "uninstall-module", Method: http.MethodDelete, Path: "/api/v1/modules/{name}",
		Summary:       "Remove the module's service; its image and data are not touched",
		DefaultStatus: http.StatusNoContent, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
	}) (*struct{}, error) {
		m, ok := s.Modules.Get(ctx, in.Name)
		if !ok {
			return nil, problem(http.StatusNotFound, "not_found", "no such module")
		}
		if err := s.Docker.RemoveService(ctx, m.Service); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		s.Modules.Forget()
		return nil, nil
	})
}
