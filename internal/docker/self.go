package docker

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// Self is what the panel knows about its own deployment: a new module needs
// its network and address.
type Self struct {
	Service string // the panel's own swarm service name, e.g. "panel_panel"
	Network string // the overlay network it is attached to
	Stack   string // the stack it was deployed from, empty outside one
	Image   string // repo:tag it runs, without the digest the daemon pins
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
	s.Stack = ct.Config.Labels["com.docker.stack.namespace"]
	s.Image, _, _ = strings.Cut(ct.Config.Image, "@")
	var names []string
	for name := range ct.NetworkSettings.Networks {
		names = append(names, name)
	}
	s.Network = pickNetwork(names, os.Getenv("PANEL_NETWORK"), ct.Config.Labels["com.docker.stack.namespace"])
	return s, nil
}

// pickNetwork chooses the default module network: PANEL_NETWORK, else the
// stack's private network (not the public proxy one), else the first by name.
func pickNetwork(names []string, explicit, stack string) string {
	var usable []string
	for _, n := range names {
		// ingress is not usable for service discovery.
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
