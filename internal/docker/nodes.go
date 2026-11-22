package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

// Node is a cluster member as the panel shows it.
type Node struct {
	ID           string
	Hostname     string
	Role         string // worker | manager
	Leader       bool
	Status       string // ready | down | ...
	Availability string // active | pause | drain
	Address      string
	Engine       string
	OS           string
	NanoCPUs     int64
	MemoryBytes  int64
}

func (n Node) Healthy() bool { return n.Status == "ready" && n.Availability == "active" }

func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	list, err := c.api.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(list.Items))
	for _, n := range list.Items {
		node := Node{
			ID:           n.ID,
			Hostname:     n.Description.Hostname,
			Role:         string(n.Spec.Role),
			Status:       string(n.Status.State),
			Availability: string(n.Spec.Availability),
			Address:      n.Status.Addr,
			Engine:       n.Description.Engine.EngineVersion,
			OS:           n.Description.Platform.OS + "/" + n.Description.Platform.Architecture,
			NanoCPUs:     n.Description.Resources.NanoCPUs,
			MemoryBytes:  n.Description.Resources.MemoryBytes,
		}
		if n.ManagerStatus != nil {
			node.Leader = n.ManagerStatus.Leader
			if n.ManagerStatus.Addr != "" {
				node.Address = n.ManagerStatus.Addr
			}
		}
		out = append(out, node)
	}
	return out, nil
}

// JoinInfo is what the join command needs: `docker swarm join` runs on the
// joining machine, so a manager can only hand out the address and the token.
type JoinInfo struct {
	Address      string // host:2377 of a manager
	WorkerToken  string
	ManagerToken string
}

func (c *Client) JoinInfo(ctx context.Context) (JoinInfo, error) {
	sw, err := c.api.SwarmInspect(ctx, client.SwarmInspectOptions{})
	if err != nil {
		return JoinInfo{}, err
	}
	info, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return JoinInfo{}, err
	}
	addr := ""
	if len(info.Info.Swarm.RemoteManagers) > 0 {
		addr = info.Info.Swarm.RemoteManagers[0].Addr
	}
	if addr == "" && info.Info.Swarm.NodeAddr != "" {
		addr = info.Info.Swarm.NodeAddr + ":2377"
	}
	return JoinInfo{
		Address:      addr,
		WorkerToken:  sw.Swarm.JoinTokens.Worker,
		ManagerToken: sw.Swarm.JoinTokens.Manager,
	}, nil
}

// RotateJoinToken invalidates the old token for one role. Everything already in
// the cluster stays; only future joins need the new one.
func (c *Client) RotateJoinToken(ctx context.Context, role string) error {
	sw, err := c.api.SwarmInspect(ctx, client.SwarmInspectOptions{})
	if err != nil {
		return err
	}
	opts := client.SwarmUpdateOptions{Version: sw.Swarm.Version, Spec: sw.Swarm.Spec}
	switch role {
	case "worker":
		opts.RotateWorkerToken = true
	case "manager":
		opts.RotateManagerToken = true
	default:
		return fmt.Errorf("unknown role %q", role)
	}
	_, err = c.api.SwarmUpdate(ctx, opts)
	return err
}

// SetAvailability drains a node before maintenance, or brings it back.
func (c *Client) SetAvailability(ctx context.Context, id, availability string) error {
	switch availability {
	case "active", "pause", "drain":
	default:
		return fmt.Errorf("unknown availability %q", availability)
	}
	res, err := c.api.NodeInspect(ctx, id, client.NodeInspectOptions{})
	if err != nil {
		return err
	}
	n := res.Node
	spec := n.Spec
	spec.Availability = swarm.NodeAvailability(availability)
	_, err = c.api.NodeUpdate(ctx, n.ID, client.NodeUpdateOptions{Version: n.Version, Spec: spec})
	return err
}

// JoinCommand is what the user pastes on the new server.
func (j JoinInfo) JoinCommand(role string) string {
	token := j.WorkerToken
	if role == "manager" {
		token = j.ManagerToken
	}
	if token == "" || j.Address == "" {
		return ""
	}
	return strings.Join([]string{"docker swarm join --token", token, j.Address}, " ")
}
