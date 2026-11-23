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
