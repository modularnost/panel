package modules

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	fetch := errors.New(`Get "http://m:8080/manifest": no such host`)
	for name, c := range map[string]struct {
		running, desired uint64
		failure          string
		state, msg       string
	}{
		"waiting for the first replica": {0, 1, "", StateStarting, "starting (0/1)"},
		"part of a global service":      {2, 3, "", StateStarting, "starting (2/3)"},
		"a wrong image tag":             {0, 1, "No such image: x:9", StateFailed, "No such image: x:9"},
		"up, but no manifest":           {1, 1, "", StateError, fetch.Error()},
		"up, an old failure is ignored": {1, 1, "No such image: x:9", StateError, fetch.Error()},
	} {
		state, msg := classify(c.running, c.desired, c.failure, fetch)
		if state != c.state || msg != c.msg {
			t.Errorf("%s: %s %q, want %s %q", name, state, msg, c.state, c.msg)
		}
	}
}
