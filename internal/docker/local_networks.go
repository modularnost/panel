package docker

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func (c *Client) LocalNetworks(ctx context.Context) ([]Network, error) {
	result, err := c.api.NetworkList(ctx, client.NetworkListOptions{Filters: make(client.Filters).Add("scope", "local")})
	if err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(result.Items))
	for _, n := range result.Items {
		if n.Scope == "local" {
			out = append(out, networkInfo(n.Network, Self{}))
		}
	}
	slices.SortFunc(out, func(a, b Network) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (c *Client) LocalNetwork(ctx context.Context, id string) (Network, error) {
	result, err := c.api.NetworkInspect(ctx, id, client.NetworkInspectOptions{Scope: "local"})
	if err != nil {
		return Network{}, err
	}
	if result.Network.ID != id || result.Network.Scope != "local" {
		return Network{}, errdefs.ErrNotFound
	}
	n := networkInfo(result.Network.Network, Self{})
	for _, ep := range result.Network.Containers {
		n.Containers = append(n.Containers, ep.Name)
	}
	slices.Sort(n.Containers)
	return n, nil
}

type NewLocalNetwork struct {
	NewNetwork
	Driver string `json:"driver" enum:"bridge,macvlan,ipvlan"`
	Parent string `json:"parent,omitempty" maxLength:"128"`
}

func (c *Client) CreateLocalNetwork(ctx context.Context, in NewLocalNetwork) (string, error) {
	if in.Encrypted || in.Attachable {
		return "", ErrInvalidNetwork
	}
	if in.Driver != "bridge" && in.Driver != "macvlan" && in.Driver != "ipvlan" {
		return "", ErrInvalidNetwork
	}
	if in.Driver == "bridge" && in.Parent != "" {
		return "", ErrInvalidNetwork
	}
	return c.createNetwork(ctx, in.NewNetwork, "local", in.Driver, in.Parent)
}

func (c *Client) RemoveLocalNetwork(ctx context.Context, id string) error {
	n, err := c.LocalNetwork(ctx, id)
	if err != nil {
		return err
	}
	if n.Protected || n.Stack != "" {
		return ErrProtectedNetwork
	}
	if len(n.Containers) > 0 {
		return ErrNetworkConflict
	}
	_, err = c.api.NetworkRemove(ctx, id, client.NetworkRemoveOptions{})
	return err
}

func (c *Client) AgentNetworkAddresses(ctx context.Context, service string) (map[string]netip.Addr, error) {
	out := make(map[string]netip.Addr)
	if service == "" {
		return out, nil
	}
	self, err := c.Self(ctx)
	if err != nil {
		return nil, err
	}
	if self.Network == "" {
		return nil, fmt.Errorf("panel network unavailable")
	}
	agent, err := c.service(ctx, service)
	if err != nil {
		return nil, err
	}
	tasks, err := c.tasks(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(tasks, func(a, b swarm.Task) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for _, t := range tasks {
		if t.Status.State != swarm.TaskStateRunning || t.DesiredState != swarm.TaskStateRunning || out[t.NodeID].IsValid() {
			continue
		}
		for _, n := range t.NetworksAttachments {
			if n.Network.Spec.Name != self.Network {
				continue
			}
			for _, prefix := range n.Addresses {
				if prefix.IsValid() {
					out[t.NodeID] = prefix.Addr()
					break
				}
			}
		}
	}
	return out, nil
}
