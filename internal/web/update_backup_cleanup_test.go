package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

func TestPruneCompletedUpdateBackups(t *testing.T) {
	const target = "registry/panel:1.2.3@sha256:target"
	const wrongDigest = "registry/panel:1.2.3@sha256:other"
	started := time.Now()
	var rollout atomic.Pointer[swarm.Service]
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.48")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rollout.Load())
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	dk, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()

	version := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = version })
	path := filepath.Join(t.TempDir(), "panel.db")
	db, err := store.Open(filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Server{DB: db, Docker: dk}

	backup := func(tag string, confirmed bool) string {
		t.Helper()
		name, err := db.CreateUpdateBackup(tag)
		if err != nil || name == "" {
			t.Fatalf("create backup: %q, %v", name, err)
		}
		if confirmed {
			ok, err := db.ConfirmUpdateBackup(name)
			if err != nil || !ok {
				t.Fatalf("confirm backup: %v, %v", ok, err)
			}
		}
		return name
	}
	oldest := backup("v1.1.0", true)
	older := backup("v1.1.1", true)
	previous := backup("v1.1.2", true)
	interrupted := backup("v1.2.3", false)
	current := backup("v1.2.3", false)
	newer := backup("v1.2.4", false)
	legacy := path + ".before-v1.0.1-20260101T010203Z"
	manual := filepath.Join(filepath.Dir(current), "manual.db")
	for _, file := range []string{legacy, manual} {
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	attempt := store.PanelUpdate{
		Version: Version, Image: target, Service: "test_panel", Backup: current,
		RequestedAt: started, PreviousStart: started.Add(-time.Minute),
	}
	for _, tc := range []struct {
		name, image string
		state       swarm.UpdateState
		failed      bool
		prunes      bool
	}{
		{name: "paused", image: target, state: swarm.UpdateStatePaused},
		{name: "rollback", image: target, state: swarm.UpdateStateRollbackStarted},
		{name: "different digest", image: wrongDigest, state: swarm.UpdateStateCompleted},
		{name: "failed", image: target, state: swarm.UpdateStateCompleted, failed: true},
		{name: "completed", image: target, state: swarm.UpdateStateCompleted, prunes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rollout.Store(&swarm.Service{
				Spec: swarm.ServiceSpec{
					TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: tc.image}},
				},
				UpdateStatus: &swarm.UpdateStatus{State: tc.state, StartedAt: &started},
			})
			attempt.Failed = tc.failed
			if err := db.SavePanelUpdate(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			pruned, err := s.pruneCompletedUpdateBackups(t.Context(), "")
			if err != nil || (pruned == current) != tc.prunes {
				t.Fatalf("pruned %q, error %v", pruned, err)
			}
			for _, file := range []string{older, previous, current, newer, legacy, manual} {
				if _, err := os.Stat(file); err != nil {
					t.Errorf("retained file %s: %v", file, err)
				}
			}
			for _, file := range []string{oldest, oldest + ".confirmed", interrupted} {
				_, err := os.Stat(file)
				if tc.prunes && !errors.Is(err, os.ErrNotExist) || !tc.prunes && err != nil {
					t.Errorf("prune=%t, file %s: %v", tc.prunes, file, err)
				}
			}
		})
	}
}
