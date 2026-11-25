package web

import (
	"cmp"
	"context"
	"net/http"
	"net/netip"
	"slices"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
)

type nodeNetworkInventory struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Available bool             `json:"available"`
	Networks  []docker.Network `json:"networks" nullable:"false"`
}
type nodeNetworksOutput struct {
	Body []nodeNetworkInventory `nullable:"false"`
}
type nodeNetworkPath struct {
	Node string `path:"node" pattern:"^[a-z0-9]+$"`
	ID   string `path:"id" pattern:"^[a-z0-9]+$"`
}

func (s *Server) registerLocalNetworks(api huma.API, viewer, admin huma.Middlewares) {
	huma.Register(api, huma.Operation{OperationID: "list-node-networks", Method: http.MethodGet, Path: "/api/v1/network-nodes", Summary: "Local network inventory by Swarm node", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, _ *struct{}) (*nodeNetworksOutput, error) {
		nodes, err := s.Docker.Nodes(ctx)
		if err != nil {
			return nil, networkProblem(err)
		}
		local, _ := s.Docker.LocalSwarmNode(ctx)
		addresses, _ := s.Docker.AgentNetworkAddresses(ctx, s.AgentService)
		out := make([]nodeNetworkInventory, len(nodes))
		var wg sync.WaitGroup
		slots := make(chan struct{}, 8)
		for i, node := range nodes {
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				row := nodeNetworkInventory{ID: node.ID, Name: node.Hostname, Networks: []docker.Network{}}
				var rows []docker.Network
				var err error
				if node.ID == local {
					rows, err = s.Docker.LocalNetworks(ctx)
				} else {
					err = s.agentLocalRequest(ctx, addresses[node.ID], http.MethodGet, "/networks", nil, &rows)
				}
				if err == nil {
					row.Available = true
					row.Networks = rows
				}
				out[i] = row
			})
		}
		wg.Wait()
		slices.SortFunc(out, func(a, b nodeNetworkInventory) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		return &nodeNetworksOutput{Body: out}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-node-network", Method: http.MethodGet, Path: "/api/v1/network-nodes/{node}/networks/{id}", Summary: "Details for a local network", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, in *nodeNetworkPath) (*networkOutput, error) {
		out := &networkOutput{}
		err := s.withLocalNode(ctx, in.Node, func() error {
			var err error
			out.Body, err = s.Docker.LocalNetwork(ctx, in.ID)
			return err
		}, http.MethodGet, "/networks/"+in.ID, nil, &out.Body)
		if err != nil {
			return nil, networkProblem(err)
		}
		return out, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-node-network", Method: http.MethodPost, Path: "/api/v1/network-nodes/{node}/networks", DefaultStatus: http.StatusCreated, Summary: "Create a local network on a node", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *struct {
		Node string `path:"node" pattern:"^[a-z0-9]+$"`
		Body docker.NewLocalNetwork
	}) (*networkCreated, error) {
		out := &networkCreated{}
		result := &agent.NetworkCreated{}
		err := s.withLocalNode(ctx, in.Node, func() error {
			var err error
			result.ID, err = s.Docker.CreateLocalNetwork(ctx, in.Body)
			return err
		}, http.MethodPost, "/networks", in.Body, result)
		if err != nil {
			return nil, networkProblem(err)
		}
		out.Body.ID = result.ID
		return out, nil
	})
	huma.Register(api, huma.Operation{OperationID: "remove-node-network", Method: http.MethodDelete, Path: "/api/v1/network-nodes/{node}/networks/{id}", Summary: "Remove an unused local network on a node", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *nodeNetworkPath) (*struct{}, error) {
		err := s.withLocalNode(ctx, in.Node, func() error { return s.Docker.RemoveLocalNetwork(ctx, in.ID) }, http.MethodDelete, "/networks/"+in.ID, nil, nil)
		if err != nil {
			return nil, networkProblem(err)
		}
		return nil, nil
	})
}

func (s *Server) agentLocalRequest(ctx context.Context, addr netip.Addr, method, path string, in, out any) error {
	if s.Metrics == nil {
		return errdefs.ErrUnavailable
	}
	return s.Metrics.LocalRequest(ctx, addr, method, path, in, out)
}

func (s *Server) withLocalNode(ctx context.Context, node string, local func() error, method, path string, in, out any) error {
	current, err := s.Docker.LocalSwarmNode(ctx)
	if err != nil {
		return err
	}
	if current == node {
		return local()
	}
	addresses, err := s.Docker.AgentNetworkAddresses(ctx, s.AgentService)
	if err != nil {
		return err
	}
	if !addresses[node].IsValid() {
		return errdefs.ErrUnavailable
	}
	return s.agentLocalRequest(ctx, addresses[node], method, path, in, out)
}
