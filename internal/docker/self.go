package docker

import (
	"context"
	"os"
	"slices"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// Self is what the panel knows about its own deployment. Installing a module
// needs it: the module has to land on the same overlay network and be told how
// to call back, and asking the user for both is asking them to get it wrong.
type Self struct {
	Service string // the panel's own swarm service name, e.g. "panel_panel"
	Network string // the overlay network it is attached to
}

// Self inspects the panel's own container. In Docker the hostname of a
// container is its id, which is enough to look ourselves up.
func (c *Client) Self(ctx context.Context) (Self, error) {
	var s Self
	host, err := os.Hostname()
	if err != nil {
		return s, err
	}
	ct, err := c.api.ContainerInspect(ctx, host)
	if err != nil {
		return s, err
	}
	s.Service = ct.Config.Labels["com.docker.swarm.service.name"]
	var names []string
	for name := range ct.NetworkSettings.Networks {
		names = append(names, name)
	}
	s.Network = pickNetwork(names, os.Getenv("PANEL_NETWORK"), ct.Config.Labels["com.docker.stack.namespace"])
	return s, nil
}

// pickNetwork chooses the network modules join by default. Behind Traefik the
// panel sits on several, and Go's map order made the answer random. Explicit
// PANEL_NETWORK wins; otherwise the stack's private network, so modules are not
// put on the public proxy network by accident; otherwise the first by name.
func pickNetwork(names []string, explicit, stack string) string {
	var usable []string
	for _, n := range names {
		// ingress carries published ports and is not usable for service
		// discovery between services.
		if n != "ingress" && n != "bridge" && n != "host" {
			usable = append(usable, n)
		}
	}
	slices.Sort(usable)
	if explicit != "" {
		return explicit
	}
	if stack != "" && slices.Contains(usable, stack+"_default") {
		return stack + "_default"
	}
	if len(usable) > 0 {
		return usable[0]
	}
	return ""
}

// OverlayNetworks lists the networks a module could be attached to.
func (c *Client) OverlayNetworks(ctx context.Context) ([]string, error) {
	list, err := c.api.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("driver", "overlay")),
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range list {
		if n.Name == "ingress" {
			continue
		}
		out = append(out, n.Name)
	}
	return out, nil
}
