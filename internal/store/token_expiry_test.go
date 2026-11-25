package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestPurgeExpiredTokensUsesIndexAndPreservesLegacyTimes(t *testing.T) {
	path := t.TempDir() + "/tokens.db"
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range files {
		if file.Name() == "0013_token_expiry_keys.sql" {
			break
		}
		if err := applyMigration(old, file.Name(), i+1); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := old.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 12 {
		t.Fatalf("fixture schema version = %d, %v, want 12", version, err)
	}
	if _, err := old.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (1, 'ann', 'unused', 'admin')`); err != nil {
		t.Fatal(err)
	}
	for name, expiry := range map[string]any{
		"expired-modern": time.Now().UTC().Add(-time.Hour),
		"expired-legacy": "2001-02-03 04:05:06.123456789 +0300 MSK",
		"future-legacy":  "2099-02-03 04:05:06.123456789 -0500 EST",
		"no-expiry":      nil,
		"unparsed":       "unknown",
	} {
		if _, err := old.Exec(`INSERT INTO api_tokens (user_id, name, token_hash, expires_at) VALUES (1,?,?,?)`, name, name, expiry); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, _, err := db.CreateToken(1, "expired-new", "", -time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateToken(1, "future-new", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := db.PurgeExpiredTokens(t.Context()); err != nil {
		t.Fatal(err)
	}
	var names string
	if err := db.QueryRow(`SELECT group_concat(name, ',') FROM (SELECT name FROM api_tokens ORDER BY name)`).Scan(&names); err != nil || names != "future-legacy,future-new,no-expiry,unparsed" {
		t.Fatalf("remaining tokens = %q, %v", names, err)
	}
	var id, parent, unused int
	var plan string
	err = db.QueryRow(`EXPLAIN QUERY PLAN DELETE FROM api_tokens WHERE expires_at_key < ?`, time.Now().UTC().Format(tokenExpiryKeyFormat)).Scan(&id, &parent, &unused, &plan)
	if err != nil || !strings.Contains(plan, "api_tokens_expiry") {
		t.Fatalf("cleanup plan = %q, %v", plan, err)
	}
}
