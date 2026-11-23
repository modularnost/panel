package web

import (
	"errors"
	"testing"
)

func TestPasswordChecksRejectExcessWork(t *testing.T) {
	s := &Server{}
	s.passwordChecks.Store(maxPasswordChecks)
	if _, err := s.authenticatePassword("operator", "password"); !errors.Is(err, errPasswordChecksBusy) {
		t.Fatalf("expected a busy error, got %v", err)
	}
	if got := s.passwordChecks.Load(); got != maxPasswordChecks {
		t.Fatalf("rejected request changed active checks to %d", got)
	}
}
