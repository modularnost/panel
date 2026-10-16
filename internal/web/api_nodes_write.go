package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

// refuseDrain stops the one change that takes the panel down with it: the last
// manager that can schedule work leaves scheduling, and the panel, which runs on
// a manager, has nowhere to go.
func refuseDrain(nodes []docker.Node, id, availability string) error {
	if availability == "active" {
		return nil
	}
	var target *docker.Node
	for i := range nodes {
		if nodes[i].ID == id {
			target = &nodes[i]
		}
	}
	if target == nil || target.Role != "manager" || target.Availability != "active" {
		return nil
	}
	for _, n := range nodes {
		if n.ID != id && n.Role == "manager" && n.Availability == "active" && n.Status == "ready" {
			return nil
		}
	}
	return errors.New("this is the only manager that can run the panel: add another manager first")
}

type apiAvailability struct {
	Availability string `json:"availability" enum:"active,pause,drain"`
}

type apiJoinRotate struct {
	Role string `json:"role" enum:"worker,manager"`
}

type nodeOutput struct{ Body apiNode }

func (s *Server) registerNodeActions(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "set-node-availability", Method: http.MethodPut, Path: "/api/v1/nodes/{id}/availability",
		Summary:     "Make a node active, paused or drained",
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Body apiAvailability
	}) (*nodeOutput, error) {
		nodes, err := s.Docker.Nodes(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		var node *docker.Node
		for i := range nodes {
			if nodes[i].ID == in.ID || nodes[i].Hostname == in.ID {
				node = &nodes[i]
			}
		}
		if node == nil {
			return nil, problem(http.StatusNotFound, "not_found", "no such node")
		}
		if err := refuseDrain(nodes, node.ID, in.Body.Availability); err != nil {
			return nil, problem(http.StatusConflict, "last_manager", err.Error())
		}
		if err := s.Docker.SetAvailability(ctx, node.ID, in.Body.Availability); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		node.Availability = in.Body.Availability
		return &nodeOutput{Body: s.apiNodes([]docker.Node{*node})[0]}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rotate-join-token", Method: http.MethodPost, Path: "/api/v1/nodes/join/rotate",
		Summary:     "Replace the join token: commands issued before stop working for new servers",
		Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct{ Body apiJoinRotate }) (*joinOutput, error) {
		if err := s.Docker.RotateJoinToken(ctx, in.Body.Role); err != nil {
			return nil, problem(http.StatusBadGateway, "docker_error", err.Error())
		}
		info, err := s.Docker.JoinInfo(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		return &joinOutput{Body: apiJoin{Role: in.Body.Role, Address: info.Address, Command: info.JoinCommand(in.Body.Role)}}, nil
	})
}
