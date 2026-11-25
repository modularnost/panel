package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (d *DB) LoadModuleCatalog(ctx context.Context) ([]byte, time.Time, error) {
	var body string
	var checkedAt int64
	err := d.QueryRowContext(ctx, `SELECT body, checked_at FROM module_catalog WHERE id = 1`).Scan(&body, &checkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	return []byte(body), time.Unix(checkedAt, 0), err
}

func (d *DB) SaveModuleCatalog(ctx context.Context, body []byte, checkedAt time.Time) error {
	_, err := d.ExecContext(ctx, `INSERT INTO module_catalog (id, body, checked_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET body = excluded.body, checked_at = excluded.checked_at`, string(body), checkedAt.Unix())
	return err
}

func (d *DB) ModuleStorageNode(ctx context.Context, name, node string) (string, error) {
	var stored string
	err := d.QueryRowContext(ctx, `INSERT INTO module_storage (name, node) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET node = module_storage.node RETURNING node`, name, node).Scan(&stored)
	return stored, err
}

func (d *DB) ModuleVolume(ctx context.Context, key, node, volume string) (string, string, error) {
	var storedNode, storedVolume string
	err := d.QueryRowContext(ctx, `INSERT INTO module_storage (name, node, volume_name) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET volume_name = CASE WHEN module_storage.volume_name = ''
			THEN excluded.volume_name ELSE module_storage.volume_name END RETURNING node, volume_name`,
		key, node, volume).Scan(&storedNode, &storedVolume)
	return storedNode, storedVolume, err
}

func (d *DB) ModuleVolumeSelection(ctx context.Context, key string) (string, string, error) {
	var node, volume string
	err := d.QueryRowContext(ctx, "SELECT node, volume_name FROM module_storage WHERE name = ?", key).Scan(&node, &volume)
	return node, volume, err
}
