package docker

import (
	"context"
	"time"

	"github.com/moby/moby/api/types/swarm"
)

type Rollout struct {
	State         swarm.UpdateState
	StartedAt     time.Time
	Image         string
	PreviousImage string
}

func (c *Client) ServiceRollout(ctx context.Context, name string) (Rollout, error) {
	svc, err := c.service(ctx, name)
	if err != nil {
		return Rollout{}, err
	}
	var out Rollout
	if spec := svc.Spec.TaskTemplate.ContainerSpec; spec != nil {
		out.Image, _ = splitImage(spec.Image)
	}
	if old := svc.PreviousSpec; old != nil && old.TaskTemplate.ContainerSpec != nil {
		out.PreviousImage, _ = splitImage(old.TaskTemplate.ContainerSpec.Image)
	}
	if status := svc.UpdateStatus; status != nil {
		out.State = status.State
		if status.StartedAt != nil {
			out.StartedAt = *status.StartedAt
		}
	}
	return out, nil
}

func (r Rollout) Active() bool {
	return r.State == swarm.UpdateStateUpdating || r.State == swarm.UpdateStateRollbackStarted
}
