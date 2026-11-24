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
	var tokens []string
	for _, tc := range []struct {
		name string
		ttl  time.Duration
	}{
		{"expired", -time.Hour},
		{"active", time.Hour},
		{"permanent", 0},
	} {
		token, _, err := db.CreateToken(user.ID, tc.name, "", tc.ttl)
		if err != nil {
			t.Fatal(err)
		}
		if tc.ttl >= 0 {
			tokens = append(tokens, token)
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
	for _, token := range tokens {
		if _, err := db.UserByToken(token); err != nil {
			t.Fatalf("a non-expired token was lost: %v", err)
		}
	}
}
