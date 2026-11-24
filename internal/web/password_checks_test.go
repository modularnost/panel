package web

import (
	"errors"
	"testing"
)

func TestPasswordChecksRejectExcessWork(t *testing.T) {
	s := &Server{}
	s.passwordChecks.Store(maxPasswordChecks)
	if _, _, err := s.authenticatePassword(req("192.0.2.1:1"), "operator", "password"); !errors.Is(err, errPasswordChecksBusy) {
		t.Fatalf("expected a busy error, got %v", err)
	}
	if got := s.passwordChecks.Load(); got != maxPasswordChecks {
		t.Fatalf("rejected request changed active checks to %d", got)
	}
	if len(s.logins.m) != 0 {
		t.Fatal("busy check retained a reservation")
	}
}
