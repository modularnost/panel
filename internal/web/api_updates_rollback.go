package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiUpdateRollbackStarted struct {
	Image  string `json:"image" doc:"The original image the panel service is rolled back to"`
	Backup string `json:"backup,omitempty" doc:"The database backup taken before the update"`
}

type updateRollbackStartedOutput struct{ Body apiUpdateRollbackStarted }

func (s *Server) registerUpdateRollback(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "rollback-update", Method: http.MethodPost, Path: "/api/v1/updates/rollback",
		Summary:       "Roll back the panel and agents to the image running before the last update",
		DefaultStatus: http.StatusAccepted, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*updateRollbackStartedOutput, error) {
		attempt, err := s.DB.LastPanelUpdate(ctx)
		if err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		if attempt == nil || attempt.OriginalImage == "" {
			return nil, problem(http.StatusUnprocessableEntity, "nothing_to_rollback",
				"no previous update to roll back to")
		}
		self, err := s.Docker.Self(ctx)
		if err != nil || self.Service == "" {
			return nil, problem(http.StatusConflict, "not_a_service", "the panel does not run as a swarm service")
		}
		if !s.updating.CompareAndSwap(false, true) {
			return nil, problem(http.StatusConflict, "update_in_progress", "an update is already running")
		}
		started := false
		defer func() {
			if !started {
				s.updating.Store(false)
			}
		}()

		rollout, err := s.Docker.ServiceRollout(ctx, self.Service)
		if err != nil {
			return nil, backendProblem(http.StatusBadGateway, "docker_unavailable", err)
		}
		if rollout.Active() {
			return nil, problem(http.StatusConflict, "update_in_progress", "the Swarm service is already updating")
		}
		if matchesUpdateImage(rollout.Image, attempt.OriginalImage) {
			return nil, problem(http.StatusUnprocessableEntity, "already_rolled_back",
				"the service is already running the original image")
		}

		me, _ := ctx.Value(ctxKey{}).(store.User)

		// Agents first, and synchronously: a failure here leaves the panel alone.
		if s.AgentService != "" {
			agentImage := attempt.AgentOriginalImage
			if agentImage == "" {
				agentImage = docker.ImageTag(attempt.OriginalImage)
			}
			if agentImage != "" {
				finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: s.AgentService, Source: "rollback", TriggeredBy: me.Username})
				d, err := s.Docker.Rollback(ctx, s.AgentService, agentImage)
				finish(d.OldDigest, d.NewDigest, err)
				logWarnings(d)
				if err != nil {
					return nil, backendProblem(http.StatusBadGateway, "docker_error", fmt.Errorf("agent service: %w", err))
				}
			}
		}

		attempt.PreviousStart = rollout.StartedAt
		attempt.RequestedAt = time.Now()
		attempt.Failed = false
		if err := s.DB.SavePanelUpdate(ctx, *attempt); err != nil {
			log.Printf("record rollback start: %v", err)
		}

		started = true
		go func() {
			defer s.updating.Store(false)
			time.Sleep(panelUpdateDelay)
			finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: self.Service, Source: "rollback", TriggeredBy: me.Username})
			d, err := s.Docker.Rollback(context.Background(), self.Service, attempt.OriginalImage)
			finish(d.OldDigest, d.NewDigest, err)
			logWarnings(d)
			if err != nil {
				log.Printf("rollback to %s: %v", attempt.OriginalImage, err)
			}
		}()

		return &updateRollbackStartedOutput{Body: apiUpdateRollbackStarted{
			Image:  attempt.OriginalImage,
			Backup: attempt.Backup,
		}}, nil
	})
}
