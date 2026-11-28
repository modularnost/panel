package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/agent"
)

type nodeUsage struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Availability string `json:"availability"`
	agent.HostUsage
}

type nodeUsageOutput struct {
	Body []nodeUsage `nullable:"false"`
}

func (s *Server) registerNodeUsage(api huma.API, viewer huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "list-node-usage", Method: http.MethodGet, Path: "/api/v1/node-usage",
		Summary: "Current host resource usage for Swarm nodes", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*nodeUsageOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		nodes, err := s.Docker.Nodes(ctx)
		if err != nil {
			return nil, backendProblem(http.StatusBadGateway, "docker_unavailable", err)
		}
		local, _ := s.Docker.LocalSwarmNode(ctx)
		addresses, _ := s.Docker.AgentNetworkAddresses(ctx, s.AgentService)
		rows := make([]nodeUsage, len(nodes))
		var wg sync.WaitGroup
		slots := make(chan struct{}, 8)
		for i, n := range nodes {
			rows[i] = nodeUsage{ID: n.ID, Name: n.Hostname, Status: n.Status, Availability: n.Availability}
			if n.Status != "ready" || (n.ID != local && !addresses[n.ID].IsValid()) {
				continue
			}
			wg.Go(func() {
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					return
				}
				var usage agent.HostUsage
				if n.ID == local {
					usage = agent.ReadHostUsage(ctx)
				} else if err := s.agentLocalRequest(ctx, addresses[n.ID], http.MethodGet, "/host-usage", nil, &usage); err != nil {
					return
				}
				rows[i].HostUsage = usage
			})
		}
		wg.Wait()
		return &nodeUsageOutput{Body: rows}, nil
	})
}
