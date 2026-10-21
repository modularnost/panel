package web

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/modules"
	"modularnost/internal/store"
)

type apiContribution struct {
	Slot  string            `json:"slot" enum:"nav,service.tab,services.action"`
	Title string            `json:"title"`
	Path  string            `json:"path" doc:"The page the module answers at"`
	Role  string            `json:"role" enum:"viewer,operator,admin" doc:"The lowest role that sees it and may reach the path"`
	Match map[string]string `json:"match" nullable:"false" doc:"For service.tab: only services carrying all these labels"`
}

type apiModule struct {
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Service       string            `json:"service"`
	OK            bool              `json:"ok"`
	State         string            `json:"state" enum:"ready,starting,failed,error" doc:"starting: not all replicas run yet; failed: Swarm rejects or fails its tasks, error holds the reason; error: it runs but serves no manifest"`
	Error         string            `json:"error,omitempty" doc:"Why the module is not ready"`
	Contributions []apiContribution `json:"contributions" nullable:"false"`
	Permissions   []string          `json:"permissions" nullable:"false"`
}

func toAPIModules(in []modules.Module) []apiModule {
	out := make([]apiModule, 0, len(in))
	for _, m := range in {
		state := m.State
		if state == "" { // a module built by hand, not by discovery
			state = modules.StateReady
			if !m.OK() {
				state = modules.StateError
			}
		}
		mod := apiModule{
			Name: m.Name, Version: m.Version, Service: m.Service, OK: m.OK(), State: state, Error: m.Err,
			Contributions: []apiContribution{}, Permissions: []string{},
		}
		for _, c := range m.Manifest.Contributions {
			match := c.Match
			if match == nil {
				match = map[string]string{}
			}
			mod.Contributions = append(mod.Contributions, apiContribution{c.Slot, c.Title, c.Path, c.Role, match})
		}
		mod.Permissions = append(mod.Permissions, m.Manifest.Permissions...)
		out = append(out, mod)
	}
	return out
}

type apiWebhook struct {
	ID      int64  `json:"id"`
	Service string `json:"service,omitempty"`
	Stack   string `json:"stack,omitempty"`
	Action  string `json:"action" enum:"force_update,stack_redeploy"`
	// Path carries the secret: whoever has it can trigger the redeploy, so
	// webhooks are listed for admins only.
	Path          string     `json:"path"`
	CreatedAt     time.Time  `json:"created_at"`
	LastTriggered *time.Time `json:"last_triggered_at,omitempty"`
}

func toAPIWebhooks(in []store.Webhook) []apiWebhook {
	out := make([]apiWebhook, 0, len(in))
	for _, w := range in {
		h := apiWebhook{
			ID: w.ID, Service: w.ServiceName, Stack: w.StackName, Action: w.ActionType,
			Path: "/hook/" + w.SecretToken, CreatedAt: w.CreatedAt,
		}
		if w.LastTriggered.Valid {
			h.LastTriggered = &w.LastTriggered.Time
		}
		out = append(out, h)
	}
	return out
}

type moduleListOutput struct {
	Body []apiModule `nullable:"false"`
}
type webhookListOutput struct {
	Body []apiWebhook `nullable:"false"`
}

func (s *Server) registerModules(api huma.API, viewer huma.Middlewares) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "list-modules", Method: http.MethodGet, Path: "/api/v1/modules",
		Summary: "Installed modules with their manifests", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*moduleListOutput, error) {
		mods, err := s.Modules.List(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		return &moduleListOutput{Body: toAPIModules(mods)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-webhooks", Method: http.MethodGet, Path: "/api/v1/webhooks",
		Summary: "All webhooks", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*webhookListOutput, error) {
		hooks, err := s.DB.Webhooks()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &webhookListOutput{Body: toAPIWebhooks(hooks)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-service-webhooks", Method: http.MethodGet, Path: "/api/v1/services/{id}/webhooks",
		Summary: "Webhooks that fire for one service, its own and its stack's", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*webhookListOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		for _, sv := range svcs {
			if sv.ID == in.ID || sv.Name == in.ID {
				hooks, err := s.DB.WebhooksFor(sv.Name, sv.Stack)
				if err != nil {
					return nil, problem(http.StatusInternalServerError, "internal", err.Error())
				}
				return &webhookListOutput{Body: toAPIWebhooks(hooks)}, nil
			}
		}
		return nil, problem(http.StatusNotFound, "not_found", "no such service")
	})
}
