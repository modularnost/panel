package docker

import (
	"context"
	"os"

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
	for name := range ct.NetworkSettings.Networks {
		// ingress carries published ports and is not usable for service
		// discovery between services.
		if name != "ingress" && name != "bridge" && name != "host" {
			s.Network = name
			break
		}
	}
	return s, nil
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
