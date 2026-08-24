package docker

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// NewService is what the "run a service" form collects. Anything fancier than
// this belongs in a compose file and goes through DeployStack.
type NewService struct {
	Name     string
	Image    string
	Replicas uint64
	Env      []string // KEY=value
	Ports    []string // published:target[/proto]
	Networks []string
	Labels   map[string]string
}

// Swarm object names: the CLI is not a shell here, but a name starting with a
// dash would still be read as a flag, and Swarm rejects the rest anyway.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func ValidName(s string) bool { return len(s) <= 63 && nameRe.MatchString(s) }

// parsePorts turns "8080:80" or "53:53/udp" into a Swarm port config.
func parsePorts(specs []string) ([]swarm.PortConfig, error) {
	var out []swarm.PortConfig
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		proto := "tcp"
		if body, p, ok := strings.Cut(spec, "/"); ok {
			spec, proto = body, strings.ToLower(p)
			if proto != "tcp" && proto != "udp" {
				return nil, fmt.Errorf("port %q: protocol must be tcp or udp", spec)
			}
		}
		pubStr, targetStr, ok := strings.Cut(spec, ":")
		if !ok {
			return nil, fmt.Errorf("port %q: expected published:target", spec)
		}
		pub, err := strconv.ParseUint(strings.TrimSpace(pubStr), 10, 16)
		if err != nil || pub == 0 {
			return nil, fmt.Errorf("port %q: bad published port", spec)
		}
		target, err := strconv.ParseUint(strings.TrimSpace(targetStr), 10, 16)
		if err != nil || target == 0 {
			return nil, fmt.Errorf("port %q: bad target port", spec)
		}
		out = append(out, swarm.PortConfig{
			Protocol:      swarm.PortConfigProtocol(proto),
			PublishedPort: uint32(pub),
			TargetPort:    uint32(target),
			PublishMode:   swarm.PortConfigPublishModeIngress,
		})
	}
	return out, nil
}

// CreateService starts a single replicated service. The image is resolved
// against the registry so the digest lands in the spec from the start.
func (c *Client) CreateService(ctx context.Context, n NewService) (string, error) {
	if !ValidName(n.Name) {
		return "", fmt.Errorf("service name %q: letters, digits, dot, dash and underscore only", n.Name)
	}
	if strings.TrimSpace(n.Image) == "" {
		return "", fmt.Errorf("image is required")
	}
	ports, err := parsePorts(n.Ports)
	if err != nil {
		return "", err
	}
	if n.Replicas == 0 {
		n.Replicas = 1
	}
	var nets []swarm.NetworkAttachmentConfig
	for _, name := range n.Networks {
		if name = strings.TrimSpace(name); name != "" {
			nets = append(nets, swarm.NetworkAttachmentConfig{Target: name})
		}
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: n.Name, Labels: n.Labels},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: n.Image, Env: n.Env},
			Networks:      nets,
		},
		Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &n.Replicas}},
		EndpointSpec: &swarm.EndpointSpec{Ports: ports},
	}
	resp, err := c.api.ServiceCreate(ctx, spec, swarm.ServiceCreateOptions{QueryRegistry: true})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// DeployStack runs `docker stack deploy`.
//
// ponytail: compose is a client-side format — the daemon API knows nothing
// about it, and re-implementing the loader (services, networks, volumes,
// configs, secrets, extends, interpolation) is a project of its own. The panel
// image ships the CLI that already does it. Swap this for a library only if the
// CLI dependency ever becomes a problem.
func (c *Client) DeployStack(ctx context.Context, name, compose string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("stack name %q: letters, digits, dot, dash and underscore only", name)
	}
	if strings.TrimSpace(compose) == "" {
		return "", fmt.Errorf("compose file is empty")
	}
	cmd := exec.CommandContext(ctx, "docker", "stack", "deploy",
		"--compose-file", "-", // read the file from stdin, nothing hits the disk
		"--prune", // services dropped from the file leave the cluster
		"--detach=true",
		name)
	cmd.Stdin = strings.NewReader(compose)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker stack deploy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// RemoveStack drops every service of a stack. Uses the API, not the CLI: this
// one is just a filtered delete.
func (c *Client) RemoveStack(ctx context.Context, name string) error {
	list, err := c.stackServices(ctx, name)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return fmt.Errorf("stack %q not found", name)
	}
	for _, s := range list {
		if err := c.api.ServiceRemove(ctx, s.ID); err != nil {
			return fmt.Errorf("%s: %w", s.Spec.Name, err)
		}
	}
	return nil
}

// RemoveService deletes one service.
func (c *Client) RemoveService(ctx context.Context, nameOrID string) error {
	s, err := c.service(ctx, nameOrID)
	if err != nil {
		return err
	}
	return c.api.ServiceRemove(ctx, s.ID)
}
