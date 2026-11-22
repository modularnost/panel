package store

import (
	"context"
	"testing"
	"time"
)

func TestTokenCleanupRemovesOnlyExpiredTokens(t *testing.T) {
	db, err := Open(t.TempDir() + "/tokens.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser("ann", "a-test-password", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, tc := range []struct {
		name    string
		expires any
	}{
		{"expired", now.Add(-time.Hour).In(time.FixedZone("Legacy Local Time", 14*60*60))},
		{"active", now.Add(time.Hour).In(time.FixedZone("HST", -12*60*60))},
		{"permanent", nil},
	} {
		if _, err := db.Exec(`INSERT INTO api_tokens (user_id, name, token_hash, expires_at) VALUES (?,?,?,?)`,
			user.ID, tc.name, hashToken(tc.name), tc.expires); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		db.RunTokenCleanup(ctx, 10*time.Millisecond)
	}()
	defer func() {
		cancel()
		<-done
	}()

	for {
		var expired int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM api_tokens WHERE name='expired'").Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, token := range []string{"active", "permanent"} {
		if _, err := db.UserByToken(token); err != nil {
			t.Fatalf("%s token was lost: %v", token, err)
		}
	}
}
