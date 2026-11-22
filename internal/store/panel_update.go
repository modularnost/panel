package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PanelUpdate struct {
	Version       string    `json:"version"`
	Service       string    `json:"service"`
	Image         string    `json:"image"`
	OriginalImage string    `json:"original_image"`
	Backup        string    `json:"backup"`
	RequestedAt   time.Time `json:"requested_at"`
	PreviousStart time.Time `json:"previous_start"`
	Failed        bool      `json:"failed"`
}

func (d *DB) LastPanelUpdate(ctx context.Context) (*PanelUpdate, error) {
	var raw string
	err := d.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='panel_update'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var update PanelUpdate
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		return nil, err
	}
	return &update, nil
}

func (d *DB) SavePanelUpdate(ctx context.Context, update PanelUpdate) error {
	raw, err := json.Marshal(update)
	if err != nil {
		return err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES ('panel_update', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}
