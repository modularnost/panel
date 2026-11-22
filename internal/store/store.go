// Package store is the SQLite layer. It holds ONLY what Swarm does not.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("panel_hash_token", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		token, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("expected a text token")
		}
		return hashToken(token), nil
	})
}

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite has a single writer and the panel few clients.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

// Backup writes a consistent copy of the database to dest, which must not
// exist yet. Migrations only go forward, so this is the way back.
func (d *DB) Backup(dest string) error {
	_, err := d.Exec("VACUUM INTO ?", dest)
	return err
}

// Path is the database file; empty for an in-memory database.
func (d *DB) Path() (string, error) {
	var p string
	err := d.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&p)
	return p, err
}

func migrate(db *sql.DB) error {
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	// embed.FS.ReadDir already returns entries sorted by name.
	for i, f := range files {
		name := f.Name()
		if err := applyMigration(db, name, i+1); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, name string, nextVersion int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= nextVersion {
		return nil
	}
	sqlText, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(sqlText)); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", nextVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

type Webhook struct {
	ID            int64
	ServiceName   string
	StackName     string
	SecretToken   string
	ActionType    string // force_update | stack_redeploy
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
	res, err := d.Exec(`INSERT INTO webhooks (service_name, stack_name, token_hash, action_type)
		VALUES (?,?,?,?)`, w.ServiceName, w.StackName, hashToken(w.SecretToken), w.ActionType)
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

const webhookCols = `id, service_name, stack_name, action_type, created_at, last_triggered_at`

func scanWebhook(s interface{ Scan(...any) error }) (Webhook, error) {
	var w Webhook
	err := s.Scan(&w.ID, &w.ServiceName, &w.StackName, &w.ActionType,
		&w.CreatedAt, &w.LastTriggered)
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
	return scanWebhook(d.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE token_hash=?`, hashToken(token)))
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
		_, e := d.Exec(`UPDATE deploy_events SET status=?, old_image_digest=?, new_image_digest=?,
			error_message=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
			status, oldDigest, newDigest, msg, id)
		if e != nil {
			log.Printf("deploy event %d: %v", id, e)
		}
	}, id
}

func (d *DB) RecentDeploys(limit int) ([]DeployEvent, error) {
	return d.deploys("", nil, limit)
}

type DeploymentTimes struct {
	Services map[string]*time.Time
	Stacks   map[string]*time.Time
}

func (d *DB) LastDeployments(ctx context.Context) (DeploymentTimes, error) {
	times := DeploymentTimes{Services: map[string]*time.Time{}, Stacks: map[string]*time.Time{}}
	rows, err := d.QueryContext(ctx, `SELECT service_name, stack_name, MAX(unixepoch(finished_at))
		FROM deploy_events WHERE status = 'success' AND finished_at IS NOT NULL
		GROUP BY service_name, stack_name`)
	if err != nil {
		return times, err
	}
	defer rows.Close()
	for rows.Next() {
		var service, stack string
		var seconds int64
		if err := rows.Scan(&service, &stack, &seconds); err != nil {
			return times, err
		}
		finished := time.Unix(seconds, 0).UTC()
		if service != "" {
			if previous := times.Services[service]; previous == nil || finished.After(*previous) {
				times.Services[service] = &finished
			}
		} else if stack != "" {
			times.Stacks[stack] = &finished
		}
	}
	return times, rows.Err()
}

func (t DeploymentTimes) ForService(service, stack string) *time.Time {
	direct, group := t.Services[service], t.Stacks[stack]
	if direct == nil || (group != nil && group.After(*direct)) {
		return group
	}
	return direct
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

type Stack struct {
	Name         string
	Compose      string
	RegistryHost string
	UpdatedBy    string
	UpdatedAt    time.Time
}

func (d *DB) SaveStack(name, compose, by, registryHost string) error {
	_, err := d.Exec(`INSERT INTO stacks (name, compose, updated_by, registry_host, updated_at)
		VALUES (?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET compose=excluded.compose,
			updated_by=excluded.updated_by, registry_host=excluded.registry_host, updated_at=CURRENT_TIMESTAMP`, name, compose, by, registryHost)
	return err
}

// Stack returns the stored compose file, if the stack was deployed from the panel.
func (d *DB) Stack(name string) (Stack, bool) {
	var s Stack
	err := d.QueryRow(`SELECT name, compose, updated_by, updated_at, registry_host FROM stacks WHERE name=?`,
		name).Scan(&s.Name, &s.Compose, &s.UpdatedBy, &s.UpdatedAt, &s.RegistryHost)
	return s, err == nil
}

func (d *DB) DeleteStack(name string) error {
	_, err := d.Exec(`DELETE FROM stacks WHERE name=?`, name)
	return err
}

// FirewallPeers are the addresses of servers that were allowed to join and
// have not yet appeared in the swarm.
func (d *DB) FirewallPeers() ([]FirewallPeer, error) {
	rows, err := d.Query(`SELECT address, created_at FROM firewall_peers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FirewallPeer
	for rows.Next() {
		var p FirewallPeer
		if err := rows.Scan(&p.Address, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type FirewallPeer struct {
	Address   string
	CreatedAt time.Time
}

func (d *DB) AddFirewallPeer(address string) error {
	_, err := d.Exec(`INSERT INTO firewall_peers (address) VALUES (?)
		ON CONFLICT(address) DO UPDATE SET created_at = CURRENT_TIMESTAMP`, address)
	return err
}

func (d *DB) DeleteFirewallPeer(address string) error {
	_, err := d.Exec(`DELETE FROM firewall_peers WHERE address = ?`, address)
	return err
}

// PruneFirewallPeers drops what is older than max and what Swarm knows now.
func (d *DB) PruneFirewallPeers(max time.Duration, known map[string]bool) error {
	peers, err := d.FirewallPeers()
	if err != nil {
		return err
	}
	for _, p := range peers {
		if known[p.Address] || time.Since(p.CreatedAt) > max {
			if err := d.DeleteFirewallPeer(p.Address); err != nil {
				return err
			}
		}
	}
	return nil
}

type FirewallPort struct {
	Port  int
	Proto string
	Note  string
}

func (d *DB) FirewallPorts() ([]FirewallPort, error) {
	rows, err := d.Query(`SELECT port, proto, note FROM firewall_ports ORDER BY port, proto`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FirewallPort
	for rows.Next() {
		var p FirewallPort
		if err := rows.Scan(&p.Port, &p.Proto, &p.Note); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) AddFirewallPort(p FirewallPort) error {
	_, err := d.Exec(`INSERT INTO firewall_ports (port, proto, note) VALUES (?, ?, ?)
		ON CONFLICT(port, proto) DO UPDATE SET note = excluded.note`, p.Port, p.Proto, p.Note)
	return err
}

func (d *DB) DeleteFirewallPort(port int, proto string) error {
	_, err := d.Exec(`DELETE FROM firewall_ports WHERE port = ? AND proto = ?`, port, proto)
	return err
}

// SeedFirewallPorts copies the installer's list in the first time only, so that
// a port removed on the Ports page does not come back with the next start.
func (d *DB) SeedFirewallPorts(ports []FirewallPort) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('firewall_seeded', '1')`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		for _, p := range ports {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO firewall_ports (port, proto, note) VALUES (?, ?, ?)`, p.Port, p.Proto, p.Note); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
