package docker

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

type Config struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Labels      map[string]string  `json:"labels,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	Attachments []ConfigAttachment `json:"attachments" nullable:"false"`
}

type ConfigAttachment struct {
	Service string `json:"service"`
	Target  string `json:"target"`
}

func (c *Client) Configs(ctx context.Context) ([]Config, error) {
	result, err := c.api.ConfigList(ctx, client.ConfigListOptions{})
	if err != nil {
		return nil, err
	}
	attachments, err := c.configAttachments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Config, 0, len(result.Items))
	for _, s := range result.Items {
		out = append(out, configMetadata(s, attachments[s.ID]))
	}
	return out, nil
}

func configMetadata(s swarm.Config, attachments []ConfigAttachment) Config {
	return Config{s.ID, s.Spec.Name, s.Spec.Labels, s.CreatedAt, append([]ConfigAttachment{}, attachments...)}
}

func (c *Client) InspectConfig(ctx context.Context, id string) (Config, []byte, error) {
	config, err := c.configReference(ctx, id)
	if err != nil {
		return Config{}, nil, err
	}
	attachments, err := c.configAttachments(ctx)
	return configMetadata(config, attachments[config.ID]), config.Spec.Data, err
}

func (c *Client) configAttachments(ctx context.Context) (map[string][]ConfigAttachment, error) {
	services, err := c.api.ServiceList(ctx, client.ServiceListOptions{})
	if err != nil {
		return nil, err
	}
	attachments := make(map[string][]ConfigAttachment)
	for _, svc := range services.Items {
		if spec := svc.Spec.TaskTemplate.ContainerSpec; spec != nil {
			for _, ref := range spec.Configs {
				if ref == nil {
					continue
				}
				target := ""
				if ref.File != nil {
					target = ref.File.Name
				}
				attachments[ref.ConfigID] = append(attachments[ref.ConfigID], ConfigAttachment{svc.Spec.Name, target})
			}
		}
	}
	return attachments, nil
}

func (c *Client) CreateConfig(ctx context.Context, name string, data []byte) (string, error) {
	if !ValidName(name) || len(data) == 0 || len(data) > 500<<10 {
		return "", fmt.Errorf("%w: invalid config name or size", errdefs.ErrInvalidArgument)
	}
	out, err := c.api.ConfigCreate(ctx, client.ConfigCreateOptions{Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name}, Data: data,
	}})
	return out.ID, err
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	_, err := c.api.ConfigRemove(ctx, id, client.ConfigRemoveOptions{})
	return err
}

func (c *Client) configReference(ctx context.Context, id string) (swarm.Config, error) {
	out, err := c.api.ConfigInspect(ctx, id, client.ConfigInspectOptions{})
	return out.Config, err
}

func (c *Client) SetServiceConfig(ctx context.Context, service, target, id string) error {
	if !validConfigTarget(target) {
		return fmt.Errorf("%w: expected an absolute container file path", errdefs.ErrInvalidArgument)
	}
	var config swarm.Config
	if id != "" {
		var err error
		config, err = c.configReference(ctx, id)
		if err != nil {
			return err
		}
	}
	return c.updateConfigAttachment(ctx, service, func(spec *swarm.ContainerSpec) bool {
		for i, ref := range spec.Configs {
			if ref == nil || ref.File == nil || ref.File.Name != target {
				continue
			}
			if ref.ConfigID == config.ID {
				return false
			}
			if id == "" {
				spec.Configs = append(spec.Configs[:i], spec.Configs[i+1:]...)
			} else {
				ref.ConfigID, ref.ConfigName = config.ID, config.Spec.Name
			}
			return true
		}
		if id == "" {
			return false
		}
		spec.Configs = append(spec.Configs, &swarm.ConfigReference{
			ConfigID: config.ID, ConfigName: config.Spec.Name,
			File: &swarm.ConfigReferenceFileTarget{Name: target, UID: "0", GID: "0", Mode: 0444},
		})
		return true
	})
}

func (c *Client) updateConfigAttachment(ctx context.Context, service string, change func(*swarm.ContainerSpec) bool) error {
	for range 3 {
		s, err := c.service(ctx, service)
		if err != nil {
			return err
		}
		if s.Spec.TaskTemplate.ContainerSpec == nil {
			return fmt.Errorf("%w: service must run a container", errdefs.ErrInvalidArgument)
		}
		if !change(s.Spec.TaskTemplate.ContainerSpec) {
			return nil
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

func validConfigTarget(target string) bool {
	return path.IsAbs(target) && target != "/" && path.Clean(target) == target && !strings.ContainsAny(target, "\\:\x00\r\n")
}
