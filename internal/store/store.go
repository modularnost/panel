// Package store is the SQLite layer. It holds ONLY what Swarm does not.
package store

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// ponytail: single connection — the panel is one process with few writers.
	// Lift this if concurrent reads ever become the bottleneck.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	// embed.FS.ReadDir already returns entries sorted by name.
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i, f := range files {
		if i < version {
			continue
		}
		name := f.Name()
		sqlText, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(sqlText)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			return err
		}
	}
	return nil
}

type Webhook struct {
	ID            int64
	ServiceName   string
	StackName     string
	SecretToken   string
	ActionType    string // force_update | stack_redeploy
	ImagePattern  string
	CreatedAt     time.Time
	LastTriggered sql.NullTime
}

func NewToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *DB) CreateWebhook(w Webhook) (Webhook, error) {
	if w.SecretToken == "" {
		w.SecretToken = NewToken()
	}
	res, err := d.Exec(`INSERT INTO webhooks (service_name, stack_name, secret_token, action_type, image_pattern)
		VALUES (?,?,?,?,?)`, w.ServiceName, w.StackName, w.SecretToken, w.ActionType, w.ImagePattern)
	if err != nil {
		return w, err
	}
	w.ID, _ = res.LastInsertId()
	w.CreatedAt = time.Now()
	return w, nil
}

func (d *DB) DeleteWebhook(id int64) error {
	_, err := d.Exec(`DELETE FROM webhooks WHERE id=?`, id)
	return err
}

const webhookCols = `id, service_name, stack_name, secret_token, action_type, image_pattern, created_at, last_triggered_at`

func scanWebhook(s interface{ Scan(...any) error }) (Webhook, error) {
	var w Webhook
	err := s.Scan(&w.ID, &w.ServiceName, &w.StackName, &w.SecretToken, &w.ActionType,
		&w.ImagePattern, &w.CreatedAt, &w.LastTriggered)
	return w, err
}

func (d *DB) Webhooks() ([]Webhook, error) { return d.webhooks("", nil) }

// WebhooksFor returns the hooks that fire for this service: its own, plus the
// stack-wide ones.
func (d *DB) WebhooksFor(service, stack string) ([]Webhook, error) {
	return d.webhooks(`WHERE service_name = ? OR (stack_name = ? AND stack_name != '')`,
		[]any{service, stack})
}

func (d *DB) webhooks(where string, args []any) ([]Webhook, error) {
	rows, err := d.Query(`SELECT `+webhookCols+` FROM webhooks `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) WebhookByToken(token string) (Webhook, error) {
	return scanWebhook(d.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE secret_token=?`, token))
}

func (d *DB) TouchWebhook(id int64) error {
	_, err := d.Exec(`UPDATE webhooks SET last_triggered_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

type DeployEvent struct {
	ID           int64
	ServiceName  string
	StackName    string
	Source       string // webhook | manual | ui
	OldDigest    string
	NewDigest    string
	Status       string // pending | success | failed
	TriggeredBy  string
	StartedAt    time.Time
	FinishedAt   sql.NullTime
	ErrorMessage string
}

// StartDeploy writes a pending row and returns the function that finishes it.
func (d *DB) StartDeploy(e DeployEvent) (finish func(oldDigest, newDigest string, err error), id int64) {
	res, dbErr := d.Exec(`INSERT INTO deploy_events (service_name, stack_name, trigger_source, status, triggered_by)
		VALUES (?,?,?, 'pending', ?)`, e.ServiceName, e.StackName, e.Source, e.TriggeredBy)
	if dbErr == nil {
		id, _ = res.LastInsertId()
	}
	return func(oldDigest, newDigest string, err error) {
		status, msg := "success", ""
		if err != nil {
			status, msg = "failed", err.Error()
		}
		d.Exec(`UPDATE deploy_events SET status=?, old_image_digest=?, new_image_digest=?,
			error_message=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
			status, oldDigest, newDigest, msg, id)
	}, id
}

func (d *DB) RecentDeploys(limit int) ([]DeployEvent, error) {
	return d.deploys("", nil, limit)
}

// DeploysFor returns the history of one service, including redeploys of the
// whole stack it belongs to.
func (d *DB) DeploysFor(service, stack string, limit int) ([]DeployEvent, error) {
	return d.deploys(`WHERE service_name = ? OR (stack_name = ? AND stack_name != '')`,
		[]any{service, stack}, limit)
}

func (d *DB) deploys(where string, args []any, limit int) ([]DeployEvent, error) {
	rows, err := d.Query(`SELECT id, service_name, stack_name, trigger_source, old_image_digest,
		new_image_digest, status, triggered_by, started_at, finished_at, error_message
		FROM deploy_events `+where+` ORDER BY started_at DESC, id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeployEvent
	for rows.Next() {
		var e DeployEvent
		if err := rows.Scan(&e.ID, &e.ServiceName, &e.StackName, &e.Source, &e.OldDigest,
			&e.NewDigest, &e.Status, &e.TriggeredBy, &e.StartedAt, &e.FinishedAt, &e.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Stacks (compose files) ---

type Stack struct {
	Name      string
	Compose   string
	UpdatedBy string
	UpdatedAt time.Time
}

func (d *DB) SaveStack(name, compose, by string) error {
	_, err := d.Exec(`INSERT INTO stacks (name, compose, updated_by, updated_at)
		VALUES (?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET compose=excluded.compose,
			updated_by=excluded.updated_by, updated_at=CURRENT_TIMESTAMP`, name, compose, by)
	return err
}

// Stack returns the stored compose file, if the stack was deployed from the panel.
func (d *DB) Stack(name string) (Stack, bool) {
	var s Stack
	err := d.QueryRow(`SELECT name, compose, updated_by, updated_at FROM stacks WHERE name=?`,
		name).Scan(&s.Name, &s.Compose, &s.UpdatedBy, &s.UpdatedAt)
	return s, err == nil
}

func (d *DB) DeleteStack(name string) error {
	_, err := d.Exec(`DELETE FROM stacks WHERE name=?`, name)
	return err
}
