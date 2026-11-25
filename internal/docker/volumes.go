package docker

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

type Volume struct {
	Node      string            `json:"node"`
	Services  []string          `json:"services" nullable:"false"`
	Name      string            `json:"name"`
	Driver    string            `json:"driver"`
	CreatedAt string            `json:"created_at,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

func (c *Client) LocalVolumes(ctx context.Context) ([]Volume, error) {
	list, err := c.api.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).Add("driver", "local")})
	if err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(list.Items))
	for _, v := range list.Items {
		if _, anonymous := v.Labels["com.docker.volume.anonymous"]; anonymous {
			continue
		}
		out = append(out, Volume{Name: v.Name, Driver: v.Driver, CreatedAt: v.CreatedAt, Labels: v.Labels})
	}
	return out, nil
}

func validVolumeName(name string) bool { return len(name) <= 64 && nameRe.MatchString(name) }

type ServiceVolume struct {
	Name     string `json:"name"`
	Node     string `json:"node"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

func (c *Client) ServiceVolumes(ctx context.Context, service string) ([]ServiceVolume, error) {
	s, err := c.service(ctx, service)
	if err != nil {
		return nil, err
	}
	out := []ServiceVolume{}
	if s.Spec.TaskTemplate.ContainerSpec == nil {
		return out, nil
	}
	node := volumeNode(s.Spec.TaskTemplate.Placement)
	for _, m := range s.Spec.TaskTemplate.ContainerSpec.Mounts {
		if m.Type == mount.TypeVolume {
			out = append(out, ServiceVolume{Name: m.Source, Node: node, Target: m.Target, ReadOnly: m.ReadOnly})
		}
	}
	return out, nil
}

func (c *Client) LocalVolume(ctx context.Context, name string) (Volume, error) {
	if !validVolumeName(name) {
		return Volume{}, errdefs.ErrInvalidArgument
	}
	out, err := c.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return Volume{}, err
	}
	if out.Volume.Driver != "local" {
		return Volume{}, errdefs.ErrInvalidArgument
	}
	v := out.Volume
	return Volume{Name: v.Name, Driver: v.Driver, CreatedAt: v.CreatedAt, Labels: v.Labels}, nil
}

func (c *Client) CreateLocalVolume(ctx context.Context, name string) (Volume, error) {
	if !ValidName(name) {
		return Volume{}, errdefs.ErrInvalidArgument
	}
	_, err := c.LocalVolume(ctx, name)
	if !errdefs.IsNotFound(err) {
		if err == nil {
			err = errdefs.ErrConflict
		}
		return Volume{}, err
	}
	out, err := c.api.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Driver: "local"})
	if err != nil {
		return Volume{}, err
	}
	v := out.Volume
	return Volume{Name: v.Name, Driver: v.Driver, CreatedAt: v.CreatedAt, Labels: v.Labels}, nil
}

func (c *Client) RemoveLocalVolume(ctx context.Context, name string) error {
	if _, err := c.LocalVolume(ctx, name); err != nil {
		return err
	}
	_, err := c.api.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
	return err
}

func (c *Client) VolumeAttachments(ctx context.Context, node string) (map[string][]string, error) {
	services, err := c.api.ServiceList(ctx, client.ServiceListOptions{})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, svc := range services.Items {
		spec := svc.Spec.TaskTemplate
		pinned := volumeNode(spec.Placement)
		if spec.ContainerSpec == nil || pinned != "" && pinned != node {
			continue
		}
		for _, m := range spec.ContainerSpec.Mounts {
			if m.Type == mount.TypeVolume && !slices.Contains(out[m.Source], svc.Spec.Name) {
				out[m.Source] = append(out[m.Source], svc.Spec.Name)
			}
		}
	}
	return out, nil
}

func validVolumeTarget(target string) bool {
	return path.IsAbs(target) && target != "/" && path.Clean(target) == target && !strings.ContainsAny(target, "\\:\x00\r\n")
}

func volumeNode(p *swarm.Placement) string {
	if p != nil {
		for _, constraint := range p.Constraints {
			key, value, ok := strings.Cut(constraint, "==")
			if ok && strings.TrimSpace(key) == "node.id" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func (c *Client) AttachServiceVolume(ctx context.Context, service, node, name, target string, readOnly bool) error {
	if !validVolumeTarget(target) || !ValidName(name) || !ValidName(node) {
		return errdefs.ErrInvalidArgument
	}
	for range 3 {
		s, err := c.service(ctx, service)
		if err != nil {
			return err
		}
		if s.Spec.TaskTemplate.ContainerSpec == nil {
			return errdefs.ErrInvalidArgument
		}
		changed, err := setVolumeMount(&s.Spec, target, name, node, readOnly)
		if err != nil || !changed {
			return err
		}
		_, err = c.api.ServiceUpdate(ctx, s.ID, client.ServiceUpdateOptions{Version: s.Version, Spec: s.Spec})
		if err == nil || !strings.Contains(err.Error(), "out of sequence") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errdefs.ErrConflict
}

func setVolumeMount(spec *swarm.ServiceSpec, target, name, node string, readOnly bool) (bool, error) {
	task := &spec.TaskTemplate
	container := task.ContainerSpec
	index := slices.IndexFunc(container.Mounts, func(m mount.Mount) bool { return m.Target == target })
	if index >= 0 && container.Mounts[index].Type != mount.TypeVolume {
		return false, errdefs.ErrConflict
	}
	pinned := volumeNode(task.Placement)
	if pinned != "" && pinned != node || spec.Mode.Global != nil || spec.Mode.GlobalJob != nil {
		return false, fmt.Errorf("%w: service placement conflicts with the selected node", errdefs.ErrConflict)
	}
	if pinned != node {
		if slices.ContainsFunc(container.Mounts, func(m mount.Mount) bool { return m.Type == mount.TypeVolume }) {
			return false, fmt.Errorf("%w: service already has volumes without a node pin", errdefs.ErrConflict)
		}
		if task.Placement == nil {
			task.Placement = &swarm.Placement{}
		}
		task.Placement.Constraints = append(task.Placement.Constraints, "node.id=="+node)
	}
	attachment := mount.Mount{Type: mount.TypeVolume, Source: name, Target: target, ReadOnly: readOnly, VolumeOptions: &mount.VolumeOptions{NoCopy: true}}
	if index >= 0 {
		if container.Mounts[index].Source == name && container.Mounts[index].ReadOnly == readOnly {
			return pinned != node, nil
		}
		container.Mounts[index] = attachment
	} else {
		container.Mounts = append(container.Mounts, attachment)
	}
	return true, nil
}
