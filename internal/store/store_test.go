package store

import (
	"testing"
	"time"
)

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
	weak, err := db.CreateToken(op.ID, "ci", RoleViewer, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := db.UserByToken(weak); u.Role != RoleViewer {
		t.Fatalf("token role was not applied: %q", u.Role)
	}
	strong, _ := db.CreateToken(op.ID, "ci2", RoleAdmin, 0)
	if u, _ := db.UserByToken(strong); u.Role != RoleOperator {
		t.Fatalf("token escalated rights to %q", u.Role)
	}

	// An expired token gets nobody in.
	dead, _ := db.CreateToken(op.ID, "old", "", -time.Hour)
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
