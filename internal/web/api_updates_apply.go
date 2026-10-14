package web

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

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

		backup, err := s.backupDatabase(tag)
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "backup_failed", err.Error())
		}
		image := strings.TrimPrefix(tag, "v")
		me, _ := ctx.Value(ctxKey{}).(store.User)

		// Agents first, and synchronously: a failure here leaves the panel alone.
		if s.AgentService != "" {
			finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: s.AgentService, Source: "update", TriggeredBy: me.Username})
			d, err := s.Docker.UpdateImage(ctx, s.AgentService, image)
			finish(d.OldDigest, d.NewDigest, err)
			logWarnings(d)
			if err != nil {
				return nil, problem(http.StatusBadGateway, "docker_error", "agent service: "+err.Error())
			}
		}

		started = true
		go func() {
			defer s.updating.Store(false)
			time.Sleep(panelUpdateDelay)
			finish, _ := s.DB.StartDeploy(store.DeployEvent{ServiceName: self.Service, Source: "update", TriggeredBy: me.Username})
			d, err := s.Docker.UpdateImage(context.Background(), self.Service, image)
			finish(d.OldDigest, d.NewDigest, err)
			logWarnings(d)
			if err != nil {
				log.Printf("update to %s: %v", tag, err)
			}
		}()
		return &updateStartedOutput{Body: apiUpdateStarted{Version: tag, Image: image, Backup: backup}}, nil
	})
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
