package web

import (
	"context"
	"log"
	"time"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

func (s *Server) RunUpdateBackupCleanup(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	var pruned string
	for ctx.Err() == nil {
		backup, err := s.pruneCompletedUpdateBackups(ctx, pruned)
		if err != nil && ctx.Err() == nil {
			log.Printf("pruning update backups: %v", err)
		}
		if backup != "" {
			pruned = backup
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) pruneCompletedUpdateBackups(ctx context.Context, pruned string) (string, error) {
	update, err := s.DB.LastPanelUpdate(ctx)
	if err != nil || update == nil || update.Backup == "" || update.Backup == pruned ||
		update.Failed || update.Version != Version || s.updating.Load() {
		return "", err
	}
	rollout, err := s.Docker.ServiceRollout(ctx, update.Service)
	if err != nil || !updateBackupReady(update, rollout) {
		return "", err
	}
	if !s.updating.CompareAndSwap(false, true) {
		return "", nil
	}
	defer s.updating.Store(false)

	// The active rollback copy may have changed while Swarm was queried.
	current, err := s.DB.LastPanelUpdate(ctx)
	if err != nil || current == nil || current.Backup != update.Backup ||
		current.Service != update.Service || !updateBackupReady(current, rollout) {
		return "", err
	}
	confirmed, err := s.DB.ConfirmUpdateBackup(current.Backup)
	if err != nil {
		return "", err
	}
	if confirmed {
		if err := s.DB.PruneUpdateBackups(current.Backup); err != nil {
			return "", err
		}
	}
	return current.Backup, nil
}

func (s *Server) confirmPreviousUpdateBackup(ctx context.Context, service string, rollout docker.Rollout) error {
	update, err := s.DB.LastPanelUpdate(ctx)
	if err != nil || update == nil || update.Service != service || !updateBackupReady(update, rollout) {
		return err
	}
	// A second update can start before the background check sees the first.
	_, err = s.DB.ConfirmUpdateBackup(update.Backup)
	return err
}

func updateBackupReady(update *store.PanelUpdate, rollout docker.Rollout) bool {
	return !update.Failed && update.Backup != "" &&
		rolloutState(*update, rollout, Version, time.Now()) == "completed"
}
