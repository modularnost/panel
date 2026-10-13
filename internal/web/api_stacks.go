package web

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiStack struct {
	Name         string `json:"name"`
	ServiceCount int    `json:"service_count"`
	Running      uint64 `json:"running"`
	Desired      uint64 `json:"desired"`
	Healthy      bool   `json:"healthy"`
	// Managed means the panel holds the compose file, so the stack can be edited
	// and redeployed from it.
	Managed bool `json:"managed"`
}

type apiStackDetail struct {
	Name         string       `json:"name"`
	ServiceCount int          `json:"service_count"`
	Running      uint64       `json:"running"`
	Desired      uint64       `json:"desired"`
	Healthy      bool         `json:"healthy"`
	Managed      bool         `json:"managed"`
	Services     []apiService `json:"services"`
	// Compose is for admins only: a compose file is access to the whole cluster.
	Compose   string     `json:"compose,omitempty"`
	UpdatedBy string     `json:"updated_by,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type apiDeploy struct {
	ID          int64      `json:"id"`
	Service     string     `json:"service"`
	Stack       string     `json:"stack,omitempty"`
	Source      string     `json:"source"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	OldDigest   string     `json:"old_digest,omitempty"`
	NewDigest   string     `json:"new_digest,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

func toAPIDeploy(e store.DeployEvent) apiDeploy {
	d := apiDeploy{
		ID: e.ID, Service: e.ServiceName, Stack: e.StackName, Source: e.Source,
		TriggeredBy: e.TriggeredBy, OldDigest: e.OldDigest, NewDigest: e.NewDigest,
		Status: e.Status, Error: e.ErrorMessage, StartedAt: e.StartedAt,
	}
	if e.FinishedAt.Valid {
		d.FinishedAt = &e.FinishedAt.Time
	}
	return d
}

func toAPIDeploys(events []store.DeployEvent) []apiDeploy {
	out := make([]apiDeploy, 0, len(events))
	for _, e := range events {
		out = append(out, toAPIDeploy(e))
	}
	return out
}

// summarizeStacks groups services by stack. Services outside any stack are not
// a stack and are left to the service list.
func summarizeStacks(svcs []docker.Service) []apiStack {
	byName := map[string]*apiStack{}
	for _, sv := range svcs {
		if sv.Stack == "" {
			continue
		}
		st, ok := byName[sv.Stack]
		if !ok {
			st = &apiStack{Name: sv.Stack, Healthy: true}
			byName[sv.Stack] = st
		}
		st.ServiceCount++
		st.Running += sv.Running
		st.Desired += sv.Desired
		st.Healthy = st.Healthy && sv.Healthy()
	}
	out := make([]apiStack, 0, len(byName))
	for _, st := range byName {
		out = append(out, *st)
	}
	slices.SortFunc(out, func(a, b apiStack) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

type stackListOutput struct{ Body []apiStack }
type stackOutput struct{ Body apiStackDetail }
type deployListOutput struct{ Body []apiDeploy }

type limitInput struct {
	Limit int `query:"limit" default:"100" minimum:"1" maximum:"500"`
}

func (s *Server) registerStacks(api huma.API, viewer huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "list-stacks", Method: http.MethodGet, Path: "/api/v1/stacks",
		Summary: "Stacks with their replica totals", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*stackListOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		out := summarizeStacks(svcs)
		for i := range out {
			_, out[i].Managed = s.DB.Stack(out[i].Name)
		}
		return &stackListOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-stack", Method: http.MethodGet, Path: "/api/v1/stacks/{name}",
		Summary: "One stack with its services", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
	}) (*stackOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		var detail apiStackDetail
		detail.Services = []apiService{}
		for _, sv := range svcs {
			if sv.Stack == in.Name {
				detail.Services = append(detail.Services, toAPIService(sv, false))
			}
		}
		stored, managed := s.DB.Stack(in.Name)
		if len(detail.Services) == 0 && !managed {
			return nil, problem(http.StatusNotFound, "not_found", "no such stack")
		}
		for _, st := range summarizeStacks(svcs) {
			if st.Name == in.Name {
				detail.ServiceCount, detail.Running = st.ServiceCount, st.Running
				detail.Desired, detail.Healthy = st.Desired, st.Healthy
			}
		}
		detail.Name, detail.Managed = in.Name, managed
		if managed {
			u, _ := ctx.Value(ctxKey{}).(store.User)
			if store.AtLeast(u.Role, store.RoleAdmin) {
				detail.Compose = stored.Compose
			}
			detail.UpdatedBy = stored.UpdatedBy
			detail.UpdatedAt = &stored.UpdatedAt
		}
		return &stackOutput{Body: detail}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-deploys", Method: http.MethodGet, Path: "/api/v1/deploys",
		Summary: "Recent deploys, newest first", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *limitInput) (*deployListOutput, error) {
		events, err := s.DB.RecentDeploys(in.Limit)
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &deployListOutput{Body: toAPIDeploys(events)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-service-deploys", Method: http.MethodGet, Path: "/api/v1/services/{id}/deploys",
		Summary: "Deploys of one service, including redeploys of its stack", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID    string `path:"id"`
		Limit int    `query:"limit" default:"50" minimum:"1" maximum:"500"`
	}) (*deployListOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		for _, sv := range svcs {
			if sv.ID == in.ID || sv.Name == in.ID {
				events, err := s.DB.DeploysFor(sv.Name, sv.Stack, in.Limit)
				if err != nil {
					return nil, problem(http.StatusInternalServerError, "internal", err.Error())
				}
				return &deployListOutput{Body: toAPIDeploys(events)}, nil
			}
		}
		return nil, problem(http.StatusNotFound, "not_found", "no such service")
	})
}
