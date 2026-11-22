package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

type apiUpdateApply struct {
	Version string `json:"version" doc:"A tag from GET /api/v1/updates; any other value is refused"`
}

type apiUpdateStarted struct {
	Version string `json:"version"`
	Image   string `json:"image" doc:"The image tag the panel service is moved to, shortly after this answer"`
	Backup  string `json:"backup,omitempty" doc:"The database copy taken before the update, on the panel's volume"`
}

type updateStartedOutput struct{ Body apiUpdateStarted }

// panelUpdateDelay leaves the answer time to reach the client: moving the
// panel's own service replaces the process that is writing it.
const panelUpdateDelay = time.Second

func (s *Server) registerUpdateApply(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "apply-update", Method: http.MethodPost, Path: "/api/v1/updates/apply",
		Summary:       "Update the agents and then the panel to a release the last check listed",
		DefaultStatus: http.StatusAccepted, Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, in *struct{ Body apiUpdateApply }) (*updateStartedOutput, error) {
		tag := in.Body.Version
		if s.Updates == nil || !s.Updates.Has(tag) {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input",
				"not a release newer than the running one: check for updates first")
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
		image := strings.TrimPrefix(tag, "v")
		panelTarget := docker.ImageRepo(rollout.Image) + ":" + image
		auth, _ := s.serviceRegistryAuth(panelTarget, nil)
		insp, err := s.Docker.DistributionInspect(ctx, panelTarget, auth)
		if err != nil {
			return nil, problem(http.StatusUnprocessableEntity, "image_not_found",
				fmt.Sprintf("cannot verify image in registry: %v", err))
		}
		targetTag := image
		if digest := string(insp.Descriptor.Digest); digest != "" {
			targetTag = image + "@" + digest
		}

		var agentRollout docker.Rollout
		var agentTag string
		if s.AgentService != "" {
			var err error
			agentRollout, err = s.Docker.ServiceRollout(ctx, s.AgentService)
			if err != nil {
				return nil, backendProblem(http.StatusBadGateway, "docker_unavailable", err)
			}
			if agentRollout.Image != "" {
				agentTarget := docker.ImageRepo(agentRollout.Image) + ":" + image
				agentAuth, _ := s.serviceRegistryAuth(agentTarget, nil)
				agentInsp, err := s.Docker.DistributionInspect(ctx, agentTarget, agentAuth)
				if err != nil {
					return nil, problem(http.StatusUnprocessableEntity, "image_not_found",
						fmt.Sprintf("cannot verify agent image in registry: %v", err))
				}
				agentTag = image
				if digest := string(agentInsp.Descriptor.Digest); digest != "" {
					agentTag = image + "@" + digest
				}
			} else {
				agentTag = targetTag
			}
		}

		backup, err := s.backupDatabase(tag)
		if err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "backup_failed", err)
		}
		attempt := store.PanelUpdate{
			Version: tag, Service: self.Service, Image: docker.ImageRepo(rollout.Image) + ":" + targetTag,
			OriginalImage: rollout.Image, AgentOriginalImage: agentRollout.Image, Backup: backup,
			RequestedAt: time.Now(), PreviousStart: rollout.StartedAt,
		}
		if err := s.DB.SavePanelUpdate(ctx, attempt); err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		me, _ := ctx.Value(ctxKey{}).(store.User)

		// Agents first, and synchronously: a failure here leaves the panel alone.
		if s.AgentService != "" {
			finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: s.AgentService, Source: "update", TriggeredBy: me.Username})
			d, err := s.Docker.UpdateImage(ctx, s.AgentService, agentTag)
			finish(d.OldDigest, d.NewDigest, err)
			logWarnings(d)
			if err != nil {
				s.failPanelUpdate(attempt)
				return nil, backendProblem(http.StatusBadGateway, "docker_error", fmt.Errorf("agent service: %w", err))
			}
		}

		started = true
		go func() {
			defer s.updating.Store(false)
			time.Sleep(panelUpdateDelay)
			finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: self.Service, Source: "update", TriggeredBy: me.Username})
			d, err := s.Docker.UpdateImage(context.Background(), self.Service, targetTag)
			finish(d.OldDigest, d.NewDigest, err)
			logWarnings(d)
			if err != nil {
				s.failPanelUpdate(attempt)
				log.Printf("update to %s: %v", tag, err)
				if s.AgentService != "" && agentRollout.Image != "" {
					rbFinish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: s.AgentService, Source: "rollback", TriggeredBy: "panel"})
					rbDeploy, rbErr := s.Docker.Rollback(context.Background(), s.AgentService, agentRollout.Image)
					rbFinish(rbDeploy.OldDigest, rbDeploy.NewDigest, rbErr)
					logWarnings(rbDeploy)
					if rbErr != nil {
						log.Printf("rollback agent service %s: %v", s.AgentService, rbErr)
					}
				}
			}
		}()
		return &updateStartedOutput{Body: apiUpdateStarted{Version: tag, Image: targetTag, Backup: backup}}, nil
	})
}

func (s *Server) failPanelUpdate(attempt store.PanelUpdate) {
	attempt.Failed = true
	if err := s.DB.SavePanelUpdate(context.Background(), attempt); err != nil {
		log.Printf("record update result: %v", err)
	}
}

// backupDatabase copies the database next to itself before a new version can
// migrate it. An in-memory database has nothing to copy.
func (s *Server) backupDatabase(tag string) (string, error) {
	path, err := s.DB.Path()
	if err != nil || path == "" {
		return "", err
	}
	dest := path + ".before-" + tag + "-" + time.Now().UTC().Format("20060102T150405Z")
	return dest, s.DB.Backup(dest)
}
