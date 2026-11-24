package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFailedMigrationRollsBackSchema(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	_, err = db.Exec("CREATE TABLE stacks (registry_host TEXT); PRAGMA user_version=6")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err == nil || !strings.Contains(err.Error(), "0007_registries.sql") {
		t.Fatalf("expected registry migration to fail on the duplicate column, got %v", err)
	}

	var tables, version int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='registries'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || version != 6 {
		t.Fatalf("failed migration left %d registries tables and schema version %d", tables, version)
	}
}

func TestLastDeployments(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO deploy_events (service_name, stack_name, trigger_source, status, finished_at)
		VALUES ('app_web', 'app', 'api', 'success', '2026-01-01 10:00:00'),
		       ('', 'app', 'api', 'success', '2026-01-01 11:00:00'),
		       ('', 'app', 'api', 'failed', '2026-01-01 12:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	times, err := db.LastDeployments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest := times.ForService("app_web", "app"); latest == nil || latest.Hour() != 11 {
		t.Fatalf("expected successful stack deployment, got %v", latest)
	}
	if latest := times.ForService("external_web", "external"); latest != nil {
		t.Fatalf("expected unknown deployment time, got %v", latest)
	}
}

func TestConcurrentUserChangesKeepAnAdmin(t *testing.T) {
	db, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first, err := db.CreateUser("first", "password", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateUser("second", "password", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- db.DeleteUser(first.ID)
	}()
	go func() {
		<-start
		results <- db.SetRole(second.ID, RoleViewer)
	}()
	close(start)

	refused := 0
	for range 2 {
		err := <-results
		switch {
		case errors.Is(err, ErrLastAdmin):
			refused++
		case err != nil:
			t.Fatal(err)
		}
	}
	if refused != 1 {
		t.Fatalf("refused %d changes, want 1", refused)
	}
}

func TestWebhookAndDeployRoundtrip(t *testing.T) {
	db, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	w, err := db.CreateWebhook(Webhook{ServiceName: "app_web", ActionType: "force_update"})
	if err != nil || w.SecretToken == "" {
		t.Fatalf("create: %v, token=%q", err, w.SecretToken)
	}
	var stored string
	if err := db.QueryRow("SELECT token_hash FROM webhooks WHERE id=?", w.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hashToken(w.SecretToken) {
		t.Fatal("webhook must store the hash of its token")
	}
	if _, err := db.WebhookByToken(stored); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stored hash must not authenticate: %v", err)
	}
	got, err := db.WebhookByToken(w.SecretToken)
	if err != nil || got.ServiceName != "app_web" {
		t.Fatalf("by token: %v %+v", err, got)
	}
	if _, err := db.CreateWebhook(Webhook{ActionType: "bogus"}); err == nil {
		t.Fatal("the action_type CHECK did not fire")
	}

	finish, _ := db.StartDeploy(DeployEvent{ServiceName: "app_web", Source: "webhook", Status: "pending"})
	finish("sha256:old", "sha256:new", nil)
	events, err := db.RecentDeploys(10)
	if err != nil || len(events) != 1 || events[0].Status != "success" || events[0].NewDigest != "sha256:new" {
		t.Fatalf("deploys: %v %+v", err, events)
	}

	if err := db.DeleteWebhook(w.ID); err != nil {
		t.Fatal(err)
	}
	if hooks, _ := db.Webhooks(); len(hooks) != 0 {
		t.Fatalf("not deleted: %+v", hooks)
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct-password") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(h, "other") || CheckPassword(h, "") {
		t.Fatal("wrong password accepted")
	}
	// The salt is random, so equal passwords hash differently.
	h2, _ := HashPassword("correct-password")
	if h == h2 {
		t.Fatal("hash is unsalted")
	}
	// Garbage instead of a hash must not pass and must not panic.
	for _, bad := range []string{"", "$$$", "pbkdf2-sha256$0$$", "bcrypt$1$a$b", "pbkdf2-sha256$1$!!$!!"} {
		if CheckPassword(bad, "x") {
			t.Fatalf("garbage hash accepted: %q", bad)
		}
	}
}

func TestRolesAndTokens(t *testing.T) {
	db, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if !AtLeast(RoleAdmin, RoleViewer) || !AtLeast(RoleOperator, RoleOperator) {
		t.Fatal("a higher role does not cover a lower one")
	}
	if AtLeast(RoleViewer, RoleOperator) || AtLeast("", RoleViewer) || AtLeast(RoleAdmin, "nonsense") {
		t.Fatal("role check let something through")
	}

	op, err := db.CreateUser("operator1", "password12345", RoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser("shorty", "123", RoleViewer); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := db.CreateUser("god", "password12345", "superadmin"); err == nil {
		t.Fatal("nonexistent role accepted")
	}

	if _, err := db.Authenticate("operator1", "password12345"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Authenticate("operator1", "wrong one"); err == nil {
		t.Fatal("signed in with a wrong password")
	}
	if _, err := db.Authenticate("no such user", "password12345"); err == nil {
		t.Fatal("signed in as a nonexistent user")
	}

	// A token with its own role cannot outrank its user.
	weak, _, err := db.CreateToken(op.ID, "ci", RoleViewer, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := db.UserByToken(weak); u.Role != RoleViewer {
		t.Fatalf("token role was not applied: %q", u.Role)
	}
	strong, _, _ := db.CreateToken(op.ID, "ci2", RoleAdmin, 0)
	if u, _ := db.UserByToken(strong); u.Role != RoleOperator {
		t.Fatalf("token escalated rights to %q", u.Role)
	}

	// An expired token gets nobody in.
	dead, _, _ := db.CreateToken(op.ID, "old", "", -time.Hour)
	if _, err := db.UserByToken(dead); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := db.UserByToken("made-up"); err == nil {
		t.Fatal("nonexistent token accepted")
	}

	// Revocation works.
	if err := db.DeleteTokenByValue(weak); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByToken(weak); err == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestRevokeSessionsKeepsNamedTokens(t *testing.T) {
	db, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, err := db.CreateUser("ann", "long-enough-1", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	session, _, _ := db.CreateToken(u.ID, "", "", time.Hour)
	named, _, _ := db.CreateToken(u.ID, "ci", "", 0)

	if err := db.RevokeSessions(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByToken(session); err == nil {
		t.Error("the session must be gone")
	}
	if _, err := db.UserByToken(named); err != nil {
		t.Errorf("the named token must stay: %v", err)
	}
}

func TestBackupIsAReadableCopy(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateUser("ann", "s3cret-pass", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if path, err := db.Path(); err != nil || !strings.HasSuffix(path, "panel.db") {
		t.Fatalf("path %q: %v", path, err)
	}
	copyPath := dir + "/copy.db"
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result := make(chan error, 1)
	go func() { result <- db.Backup(copyPath) }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		conn.Close()
		<-result
		t.Fatal("backup waited for the panel database connection")
	}
	conn.Close()
	cp, err := Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	if n, err := cp.CountUsers(); err != nil || n != 1 {
		t.Fatalf("copy has %d users: %v", n, err)
	}
	if err := db.Backup(copyPath); err == nil {
		t.Fatal("an existing file must not be overwritten")
	}
}

func TestMigrationsKeepExistingWebhookURLs(t *testing.T) {
	path := t.TempDir() + "/old.db"
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"0001_init.sql", "0002_users.sql", "0003_stacks.sql"} {
		text, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(text)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := old.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.Exec(`INSERT INTO webhooks (service_name, secret_token, action_type, image_pattern)
		VALUES ('app_web', 'tok', 'force_update', 'ghcr.io/me/app:*')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hooks, err := db.Webhooks()
	if err != nil || len(hooks) != 1 || hooks[0].SecretToken != "" || hooks[0].ServiceName != "app_web" {
		t.Fatalf("hooks=%+v err=%v", hooks, err)
	}
	hook, err := db.WebhookByToken("tok")
	if err != nil || hook.ServiceName != "app_web" || hook.SecretToken != "" {
		t.Fatalf("old URL does not resolve to the expected hook: %v", err)
	}
	var stored string
	if err := db.QueryRow("SELECT token_hash FROM webhooks WHERE id=?", hook.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hashToken("tok") {
		t.Fatal("migration must hash the existing token")
	}
	if _, err := db.Exec(`SELECT image_pattern FROM webhooks`); err == nil {
		t.Fatal("the column must be gone")
	}
}
