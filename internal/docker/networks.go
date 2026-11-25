package docker

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

var ErrNetworkConflict = errors.New("network configuration changed or is in use")
var ErrProtectedNetwork = errors.New("network is managed by Docker, the panel, or a stack")
var ErrInvalidNetwork = errors.New("network cannot be used for this operation")

type NetworkSubnet struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway,omitempty"`
}

type NetworkService struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Stack string `json:"stack,omitempty"`
}

type Network struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Driver     string           `json:"driver"`
	Scope      string           `json:"scope"`
	Stack      string           `json:"stack,omitempty"`
	Internal   bool             `json:"internal"`
	Attachable bool             `json:"attachable"`
	Encrypted  bool             `json:"encrypted"`
	Protected  bool             `json:"protected"`
	ConfigOnly bool             `json:"config_only"`
	Subnets    []NetworkSubnet  `json:"subnets" nullable:"false"`
	Services   []NetworkService `json:"services" nullable:"false"`
	Containers []string         `json:"containers" nullable:"false"`
}

type NewNetwork struct {
	Name       string `json:"name" minLength:"1" maxLength:"63"`
	Subnet     string `json:"subnet,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	Internal   bool   `json:"internal,omitempty"`
	Attachable bool   `json:"attachable,omitempty"`
	Encrypted  bool   `json:"encrypted,omitempty"`
}

type NetworkAttachment struct {
	ID        string   `json:"id"`
	Aliases   []string `json:"aliases" nullable:"false"`
	Automatic bool     `json:"automatic,omitempty"`
}

type ServiceNetworkState struct {
	ID          string              `json:"-"`
	Name        string              `json:"name"`
	Stack       string              `json:"stack,omitempty"`
	Version     string              `json:"version"`
	Source      string              `json:"source" enum:"service,compose,external,system"`
	Attachments []NetworkAttachment `json:"attachments" nullable:"false"`
}

func networkInfo(n network.Network, self Self) Network {
	_, encrypted := n.Options["encrypted"]
	protected := n.Ingress || n.Name == self.Network
	switch n.Name {
	case "bridge", "host", "none", "docker_gwbridge":
		protected = true
	}
	out := Network{ID: n.ID, Name: n.Name, Scope: n.Scope, Driver: n.Driver, Stack: n.Labels[stackLabel], Internal: n.Internal, Attachable: n.Attachable, Encrypted: encrypted, Protected: protected, ConfigOnly: n.ConfigOnly, Subnets: []NetworkSubnet{}, Services: []NetworkService{}, Containers: []string{}}
	for _, cfg := range n.IPAM.Config {
		subnet := NetworkSubnet{}
		if cfg.Subnet.IsValid() {
			subnet.Subnet = cfg.Subnet.String()
		}
		if cfg.Gateway.IsValid() {
			subnet.Gateway = cfg.Gateway.String()
		}
		out.Subnets = append(out.Subnets, subnet)
	}
	return out
}

func serviceAttachments(s swarm.Service) []NetworkAttachment {
	attachments := make([]NetworkAttachment, 0, len(s.Spec.TaskTemplate.Networks))
	for _, n := range s.Spec.TaskTemplate.Networks {
		attachments = append(attachments, NetworkAttachment{ID: n.Target, Aliases: append([]string{}, n.Aliases...)})
	}
	for _, vip := range s.Endpoint.VirtualIPs {
		if !slices.ContainsFunc(attachments, func(n NetworkAttachment) bool { return n.ID == vip.NetworkID }) {
			attachments = append(attachments, NetworkAttachment{ID: vip.NetworkID, Aliases: []string{}, Automatic: true})
		}
	}
	return attachments
}

func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	nets, err := c.api.NetworkList(ctx, client.NetworkListOptions{Filters: make(client.Filters).Add("scope", "swarm")})
	if err != nil {
		return nil, err
	}
	services, err := c.api.ServiceList(ctx, client.ServiceListOptions{})
	if err != nil {
		return nil, err
	}
	members := make(map[string][]NetworkService)
	for _, svc := range services.Items {
		for _, attach := range serviceAttachments(svc) {
			members[attach.ID] = append(members[attach.ID], NetworkService{svc.ID, svc.Spec.Name, svc.Spec.Labels[stackLabel]})
		}
	}
	self, _ := c.Self(ctx)
	out := make([]Network, 0, len(nets.Items))
	for _, n := range nets.Items {
		row := networkInfo(n.Network, self)
		row.Services = append(row.Services, members[n.ID]...)
		slices.SortFunc(row.Services, func(a, b NetworkService) int { return strings.Compare(a.Name, b.Name) })
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b Network) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (c *Client) Network(ctx context.Context, id string) (Network, error) {
	nets, err := c.Networks(ctx)
	if err != nil {
		return Network{}, err
	}
	for _, n := range nets {
		if n.ID != id {
			continue
		}
		inspected, err := c.api.NetworkInspect(ctx, id, client.NetworkInspectOptions{})
		if err != nil {
			return Network{}, err
		}
		for _, endpoint := range inspected.Network.Containers {
			n.Containers = append(n.Containers, endpoint.Name)
		}
		slices.Sort(n.Containers)
		return n, nil
	}
	return Network{}, errdefs.ErrNotFound
}

func (c *Client) CreateNetwork(ctx context.Context, in NewNetwork) (string, error) {
	return c.createNetwork(ctx, in, "swarm", "overlay", "")
}

func (c *Client) createNetwork(ctx context.Context, in NewNetwork, scope, driver, parent string) (string, error) {
	if !ValidName(in.Name) {
		return "", fmt.Errorf("%w: invalid network name", ErrInvalidNetwork)
	}
	if in.Gateway != "" && in.Subnet == "" {
		return "", fmt.Errorf("%w: a gateway requires a subnet", ErrInvalidNetwork)
	}
	opts := client.NetworkCreateOptions{Scope: scope, Driver: driver, Internal: in.Internal, Attachable: in.Attachable}
	if in.Subnet != "" {
		subnet, err := netip.ParsePrefix(in.Subnet)
		if err != nil {
			return "", fmt.Errorf("%w: invalid subnet", ErrInvalidNetwork)
		}
		var gateway netip.Addr
		if in.Gateway != "" {
			gateway, err = netip.ParseAddr(in.Gateway)
			if err != nil || !subnet.Contains(gateway) {
				return "", fmt.Errorf("%w: gateway must belong to the subnet", ErrInvalidNetwork)
			}
		}
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: subnet, Gateway: gateway}}}
		if subnet.Addr().Is6() {
			enabled := true
			opts.EnableIPv6 = &enabled
		}
	}
	if in.Encrypted {
		opts.Options = map[string]string{"encrypted": ""}
	}
	if parent != "" {
		opts.Options = map[string]string{"parent": parent}
	}
	result, err := c.api.NetworkCreate(ctx, in.Name, opts)
	return result.ID, err
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	n, err := c.Network(ctx, id)
	if err != nil {
		return err
	}
	if n.Protected || n.Stack != "" || n.Scope != "swarm" {
		return ErrProtectedNetwork
	}
	if len(n.Services) > 0 || len(n.Containers) > 0 {
		return ErrNetworkConflict
	}
	_, err = c.api.NetworkRemove(ctx, id, client.NetworkRemoveOptions{})
	return err
}

func (c *Client) ServiceNetworks(ctx context.Context, id string) (ServiceNetworkState, error) {
	if !nameRe.MatchString(id) {
		return ServiceNetworkState{}, errdefs.ErrNotFound
	}
	s, err := c.service(ctx, id)
	if err != nil {
		return ServiceNetworkState{}, err
	}
	state := ServiceNetworkState{ID: s.ID, Name: s.Spec.Name, Stack: s.Spec.Labels[stackLabel], Version: s.Version.String(), Source: "service", Attachments: serviceAttachments(s)}
	return state, nil
}

func (c *Client) UpdateServiceNetworks(ctx context.Context, id, version string, add, remove []string) error {
	s, err := c.service(ctx, id)
	if err != nil {
		return err
	}
	if s.Version.String() != version {
		return ErrNetworkConflict
	}
	if s.Spec.Labels[stackLabel] != "" {
		return ErrProtectedNetwork
	}
	spec := s.Spec
	spec.TaskTemplate.Networks = slices.DeleteFunc(slices.Clone(s.Spec.TaskTemplate.Networks), func(n swarm.NetworkAttachmentConfig) bool {
		return slices.Contains(remove, n.Target)
	})

	for _, id := range add {
		spec.TaskTemplate.Networks = append(spec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{Target: id})
	}
	_, err = c.api.ServiceUpdate(ctx, s.ID, client.ServiceUpdateOptions{Version: s.Version, Spec: spec, RegistryAuthFrom: swarm.RegistryAuthFromSpec})
	if err != nil && strings.Contains(err.Error(), "out of sequence") {
		return ErrNetworkConflict
	}
	return err
}
