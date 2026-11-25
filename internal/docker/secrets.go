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

type Secret struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Labels      map[string]string  `json:"labels,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	Attachments []SecretAttachment `json:"attachments" nullable:"false"`
}

type SecretAttachment struct {
	Service string `json:"service"`
	Target  string `json:"target"`
}

func (c *Client) Secrets(ctx context.Context) ([]Secret, error) {
	result, err := c.api.SecretList(ctx, client.SecretListOptions{})
	if err != nil {
		return nil, err
	}
	services, err := c.api.ServiceList(ctx, client.ServiceListOptions{})
	if err != nil {
		return nil, err
	}
	attachments := make(map[string][]SecretAttachment)
	for _, svc := range services.Items {
		if spec := svc.Spec.TaskTemplate.ContainerSpec; spec != nil {
			for _, ref := range spec.Secrets {
				if ref == nil {
					continue
				}
				target := ""
				if ref.File != nil {
					target = ref.File.Name
				}
				attachments[ref.SecretID] = append(attachments[ref.SecretID], SecretAttachment{svc.Spec.Name, target})
			}
		}
	}
	out := make([]Secret, 0, len(result.Items))
	for _, s := range result.Items {
		out = append(out, Secret{s.ID, s.Spec.Name, s.Spec.Labels, s.CreatedAt, append([]SecretAttachment{}, attachments[s.ID]...)})
	}
	return out, nil
}

func (c *Client) CreateSecret(ctx context.Context, name string, data []byte) (string, error) {
	if !ValidName(name) || len(data) == 0 || len(data) > 500<<10 {
		return "", fmt.Errorf("%w: invalid secret name or size", errdefs.ErrInvalidArgument)
	}
	out, err := c.api.SecretCreate(ctx, client.SecretCreateOptions{Spec: swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name}, Data: data,
	}})
	return out.ID, err
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	_, err := c.api.SecretRemove(ctx, id, client.SecretRemoveOptions{})
	return err
}

func (c *Client) secretReference(ctx context.Context, id string) (swarm.Secret, error) {
	out, err := c.api.SecretInspect(ctx, id, client.SecretInspectOptions{})
	return out.Secret, err
}

func validAttachmentTarget(target string, absolute bool) bool {
	return target != "" && !strings.ContainsAny(target, "\\:\x00\r\n") && path.Clean(target) == target &&
		(absolute && path.IsAbs(target) && target != "/" || !absolute && ValidName(target))
}

func (c *Client) SetServiceSecret(ctx context.Context, service, target, id string) error {
	if !validAttachmentTarget(target, false) {
		return fmt.Errorf("%w: expected a filename in /run/secrets", errdefs.ErrInvalidArgument)
	}
	var secret swarm.Secret
	if id != "" {
		var err error
		secret, err = c.secretReference(ctx, id)
		if err != nil {
			return err
		}
	}
	return c.updateAttachment(ctx, service, func(spec *swarm.ContainerSpec) bool {
		for i, ref := range spec.Secrets {
			if ref == nil || ref.File == nil || ref.File.Name != target && ref.File.Name != "/run/secrets/"+target {
				continue
			}
			if ref.SecretID == secret.ID {
				return false
			}
			if id == "" {
				spec.Secrets = append(spec.Secrets[:i], spec.Secrets[i+1:]...)
			} else {
				ref.SecretID, ref.SecretName = secret.ID, secret.Spec.Name
			}
			return true
		}
		if id == "" {
			return false
		}
		spec.Secrets = append(spec.Secrets, &swarm.SecretReference{
			SecretID: secret.ID, SecretName: secret.Spec.Name,
			File: &swarm.SecretReferenceFileTarget{Name: target, UID: "0", GID: "0", Mode: 0444},
		})
		return true
	})
}

func (c *Client) updateAttachment(ctx context.Context, service string, change func(*swarm.ContainerSpec) bool) error {
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

func (c *Client) SecretName(ctx context.Context, id string) (string, error) {
	s, err := c.secretReference(ctx, id)
	return s.Spec.Name, err
}
