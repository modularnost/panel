package web

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiUpdateAttempt struct {
	Version string `json:"version"`
	Backup  string `json:"backup,omitempty"`
	State   string `json:"state" enum:"requested,updating,completed,failed,paused,rolling_back,rollback_paused,rolled_back,unknown"`
}

func (s *Server) lastUpdate(ctx context.Context, running string) (*apiUpdateAttempt, error) {
	update, err := s.DB.LastPanelUpdate(ctx)
	if err != nil {
		return nil, backendProblem(http.StatusInternalServerError, "internal", err)
	}
	if update == nil {
		return nil, nil
	}
	out := &apiUpdateAttempt{Version: update.Version, Backup: update.Backup, State: "unknown"}
	if update.Failed {
		out.State = "failed"
	} else if rollout, err := s.Docker.ServiceRollout(ctx, update.Service); err == nil {
		out.State = rolloutState(*update, rollout, running, time.Now())
	}
	return out, nil
}

func rolloutState(update store.PanelUpdate, rollout docker.Rollout, running string, now time.Time) string {
	if !rollout.StartedAt.After(update.PreviousStart) {
		if now.Sub(update.RequestedAt) < 3*time.Minute {
			return "requested"
		}
		return "unknown"
	}
	switch rollout.State {
	case swarm.UpdateStateRollbackStarted, swarm.UpdateStateRollbackPaused, swarm.UpdateStateRollbackCompleted:
		// Swarm clears PreviousSpec once an automatic rollback completes.
		restored := rollout.PreviousImage == "" && rollout.Image == update.OriginalImage
		if rollout.Image != update.Image && rollout.PreviousImage != update.Image && !restored {
			return "unknown"
		}
		switch rollout.State {
		case swarm.UpdateStateRollbackStarted:
			return "rolling_back"
		case swarm.UpdateStateRollbackPaused:
			return "rollback_paused"
		default:
			return "rolled_back"
		}
	}
	if rollout.Image != update.Image {
		return "unknown"
	}
	switch rollout.State {
	case swarm.UpdateStateUpdating:
		return "updating"
	case swarm.UpdateStatePaused:
		return "paused"
	case swarm.UpdateStateCompleted:
		if running == update.Version {
			return "completed"
		}
		return "updating"
	default:
		return "unknown"
	}
}
