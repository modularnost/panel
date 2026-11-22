package web

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

func TestPanelRolloutState(t *testing.T) {
	at := time.Now()
	const oldImage = "registry/panel:0.7.0"
	const image = "registry/panel:0.8.0"
	update := store.PanelUpdate{Version: "v0.8.0", Image: image, OriginalImage: oldImage, RequestedAt: at, PreviousStart: at.Add(-time.Hour)}
	for _, tc := range []struct {
		name, wanted, running string
		rollout               docker.Rollout
		elapsed               time.Duration
	}{
		{"stale status", "requested", "v0.7.0", docker.Rollout{State: swarm.UpdateStateCompleted, StartedAt: update.PreviousStart, Image: oldImage}, 0},
		{"submission timed out", "unknown", "v0.7.0", docker.Rollout{StartedAt: update.PreviousStart}, 4 * time.Minute},
		{"replacing", "updating", "v0.7.0", docker.Rollout{State: swarm.UpdateStateUpdating, StartedAt: at, Image: image}, 0},
		{"previous version answering", "updating", "v0.7.0", docker.Rollout{State: swarm.UpdateStateCompleted, StartedAt: at, Image: image}, 0},
		{"new version running", "completed", "v0.8.0", docker.Rollout{State: swarm.UpdateStateCompleted, StartedAt: at, Image: image}, 0},
		{"paused", "paused", "v0.7.0", docker.Rollout{State: swarm.UpdateStatePaused, StartedAt: at, Image: image}, 0},
		{"rolling back", "rolling_back", "v0.7.0", docker.Rollout{State: swarm.UpdateStateRollbackStarted, StartedAt: at, Image: oldImage, PreviousImage: image}, 0},
		{"rolled back", "rolled_back", "v0.7.0", docker.Rollout{State: swarm.UpdateStateRollbackCompleted, StartedAt: at, Image: oldImage}, 0},
		{"unrelated rollout", "unknown", "v0.8.0", docker.Rollout{State: swarm.UpdateStateCompleted, StartedAt: at, Image: "registry/panel:edge"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rolloutState(update, tc.rollout, tc.running, at.Add(tc.elapsed)); got != tc.wanted {
				t.Fatalf("got %q, want %q", got, tc.wanted)
			}
		})
	}
}

func TestUpdatesIncludesFailedAttemptAndBackup(t *testing.T) {
	client, url, db := apiServer(t)
	if _, err := db.CreateUser("admin", "update-test-pass", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	status, _ := send(t, client, "POST", url+"/api/v1/auth/login", "application/json", `{"username":"admin","password":"update-test-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	const backup = "/data/panel.db.before-v0.8.0"
	if err := db.SavePanelUpdate(context.Background(), store.PanelUpdate{Version: "v0.8.0", Backup: backup, Failed: true}); err != nil {
		t.Fatal(err)
	}
	status, body := send(t, client, "GET", url+"/api/v1/updates", "", "")
	attempt, _ := body["last_update"].(map[string]any)
	if status != 200 || attempt["state"] != "failed" || attempt["backup"] != backup || attempt["version"] != "v0.8.0" {
		t.Fatalf("updates: %d %v", status, body)
	}
}
