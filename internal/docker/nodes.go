package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
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
}

func (n Node) Healthy() bool { return n.Status == "ready" && n.Availability == "active" }

func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	list, err := c.api.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(list))
	for _, n := range list {
		node := Node{
			ID:           n.ID,
			Hostname:     n.Description.Hostname,
			Role:         string(n.Spec.Role),
			Status:       string(n.Status.State),
			Availability: string(n.Spec.Availability),
			Address:      n.Status.Addr,
			Engine:       n.Description.Engine.EngineVersion,
			OS:           n.Description.Platform.OS + "/" + n.Description.Platform.Architecture,
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

// JoinInfo is everything needed to write the command that adds a server.
//
// The panel cannot run it: `docker swarm join` executes on the machine that is
// joining. All a manager can do is hand out the address and the token.
type JoinInfo struct {
	Address      string // host:2377 of a manager
	WorkerToken  string
	ManagerToken string
}

func (c *Client) JoinInfo(ctx context.Context) (JoinInfo, error) {
	sw, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return JoinInfo{}, err
	}
	info, err := c.api.Info(ctx)
	if err != nil {
		return JoinInfo{}, err
	}
	addr := ""
	if len(info.Swarm.RemoteManagers) > 0 {
		addr = info.Swarm.RemoteManagers[0].Addr
	}
	if addr == "" && info.Swarm.NodeAddr != "" {
		addr = info.Swarm.NodeAddr + ":2377"
	}
	return JoinInfo{
		Address:      addr,
		WorkerToken:  sw.JoinTokens.Worker,
		ManagerToken: sw.JoinTokens.Manager,
	}, nil
}

// RotateJoinToken invalidates the old token for one role. Everything already in
// the cluster stays; only future joins need the new one.
func (c *Client) RotateJoinToken(ctx context.Context, role string) error {
	sw, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return err
	}
	var flags swarm.UpdateFlags
	switch role {
	case "worker":
		flags.RotateWorkerToken = true
	case "manager":
		flags.RotateManagerToken = true
	default:
		return fmt.Errorf("unknown role %q", role)
	}
	return c.api.SwarmUpdate(ctx, sw.Version, sw.Spec, flags)
}

// SetAvailability drains a node before maintenance, or brings it back.
func (c *Client) SetAvailability(ctx context.Context, id, availability string) error {
	switch availability {
	case "active", "pause", "drain":
	default:
		return fmt.Errorf("unknown availability %q", availability)
	}
	n, _, err := c.api.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return err
	}
	spec := n.Spec
	spec.Availability = swarm.NodeAvailability(availability)
	return c.api.NodeUpdate(ctx, n.ID, n.Version, spec)
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
