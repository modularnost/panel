package web

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiTaskStats struct {
	Node       string  `json:"node"`
	Service    string  `json:"service"`
	Stack      string  `json:"stack,omitempty"`
	Slot       int     `json:"slot"`
	CPU        float64 `json:"cpu" doc:"Percent of one core, as docker stats reports it"`
	Mem        uint64  `json:"mem" doc:"Bytes"`
	MemLimit   uint64  `json:"mem_limit" doc:"Bytes; 0 when no limit is set"`
	NetRX      uint64  `json:"net_rx" doc:"Cumulative received bytes"`
	NetTX      uint64  `json:"net_tx" doc:"Cumulative transmitted bytes"`
	BlockRead  uint64  `json:"block_read" doc:"Cumulative bytes read from block devices"`
	BlockWrite uint64  `json:"block_write" doc:"Cumulative bytes written to block devices"`
	Error      string  `json:"error,omitempty" doc:"Set on a row for a node that could not be reached"`
}

func toAPITaskStats(in []docker.TaskStats) []apiTaskStats {
	out := make([]apiTaskStats, 0, len(in))
	for _, s := range in {
		out = append(out, apiTaskStats{
			Node: s.Node, Service: s.Service, Stack: s.Stack, Slot: s.Slot, CPU: s.CPU,
			Mem: s.Mem, MemLimit: s.MemLimit, NetRX: s.NetRX, NetTX: s.NetTX,
			BlockRead: s.BlockRead, BlockWrite: s.BlockWrite, Error: s.Err,
		})
	}
	return out
}

type apiNode struct {
	ID           string `json:"id"`
	Hostname     string `json:"hostname"`
	Role         string `json:"role" enum:"worker,manager"`
	Leader       bool   `json:"leader"`
	Status       string `json:"status"`
	Availability string `json:"availability"`
	Address      string `json:"address,omitempty"`
	Engine       string `json:"engine,omitempty"`
	OS           string `json:"os,omitempty"`
}

func toAPINodes(in []docker.Node) []apiNode {
	out := make([]apiNode, 0, len(in))
	for _, n := range in {
		out = append(out, apiNode{
			ID: n.ID, Hostname: n.Hostname, Role: n.Role, Leader: n.Leader, Status: n.Status,
			Availability: n.Availability, Address: n.Address, Engine: n.Engine, OS: n.OS,
		})
	}
	return out
}

type apiJoin struct {
	Role    string `json:"role" enum:"worker,manager"`
	Address string `json:"address"`
	Command string `json:"command" doc:"Run this on the machine that should join; it contains the join token"`
}

type statsOutput struct {
	Body []apiTaskStats `nullable:"false"`
}
type nodeListOutput struct {
	Body []apiNode `nullable:"false"`
}
type joinOutput struct{ Body apiJoin }

func (s *Server) registerCluster(api huma.API, viewer huma.Middlewares) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "list-stats", Method: http.MethodGet, Path: "/api/v1/stats",
		Summary: "Current CPU and memory per replica, across nodes", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Service string `query:"service" doc:"Narrow to one service name"`
	}) (*statsOutput, error) {
		stats, err := s.Metrics.Stats(ctx, in.Service)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "metrics_unavailable", err.Error())
		}
		return &statsOutput{Body: toAPITaskStats(stats)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-nodes", Method: http.MethodGet, Path: "/api/v1/nodes",
		Summary: "Cluster members", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*nodeListOutput, error) {
		nodes, err := s.Docker.Nodes(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		return &nodeListOutput{Body: toAPINodes(nodes)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-join", Method: http.MethodGet, Path: "/api/v1/nodes/join",
		Summary: "The command that adds a server to the cluster", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Role string `query:"role" default:"worker" enum:"worker,manager"`
	}) (*joinOutput, error) {
		info, err := s.Docker.JoinInfo(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		return &joinOutput{Body: apiJoin{Role: in.Role, Address: info.Address, Command: info.JoinCommand(in.Role)}}, nil
	})
}
