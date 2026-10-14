package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const feed = `[
 {"tag_name":"v0.1.0","body":"first","prerelease":true,"published_at":"2026-09-01T00:00:00Z","html_url":"u1"},
 {"tag_name":"v0.2.0","body":"second\n- breaking","prerelease":true,"published_at":"2026-10-01T00:00:00Z","html_url":"u2"},
 {"tag_name":"v0.10.0","body":"tenth","prerelease":false,"published_at":"2026-10-10T00:00:00Z","html_url":"u3"},
 {"tag_name":"v0.11.0","body":"wip","draft":true},
 {"tag_name":"v1.0.0-rc1","body":"rc"}
]`

func checker(t *testing.T, current string, handler http.HandlerFunc) *Checker {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(current, true)
	c.url = srv.URL
	return c
}

func serve(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }
}

func tags(rs []Release) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Tag)
	}
	return strings.Join(out, " ")
}

func TestNewerReleasesNewestFirst(t *testing.T) {
	c := checker(t, "v0.1.0", serve(feed))
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status()
	// 0.10.0 sorts after 0.2.0 by number, not by text; the draft and the rc are out.
	if got := tags(st.Newer); got != "v0.10.0 v0.2.0" {
		t.Fatalf("newer = %q", got)
	}
	if !st.ReleaseBuild || st.CheckedAt.IsZero() || st.Newer[1].Body != "second\n- breaking" {
		t.Fatalf("%+v", st)
	}
	if !c.Has("v0.2.0") || c.Has("v0.1.0") || c.Has("v9.9.9") {
		t.Fatal("Has must be true only for a newer release of the last check")
	}
}

func TestNothingToUpdateFromAnEdgeBuild(t *testing.T) {
	for _, cur := range []string{"dev", "v0.1.0-4-gabc1234", ""} {
		c := checker(t, cur, serve(feed))
		if err := c.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if st := c.Status(); st.ReleaseBuild || len(st.Newer) != 0 {
			t.Errorf("%q: %+v", cur, st)
		}
	}
}

func TestFailureKeepsThePreviousAnswer(t *testing.T) {
	fail := false
	c := checker(t, "v0.1.0", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		w.Write([]byte(feed))
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	st := c.Status()
	if len(st.Newer) != 2 || st.Error == "" {
		t.Fatalf("%+v", st)
	}
}

func TestDisabledNeverCallsOut(t *testing.T) {
	called := false
	c := checker(t, "v0.1.0", func(http.ResponseWriter, *http.Request) { called = true })
	c.enabled = false
	if err := c.Refresh(context.Background()); err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if st := c.Status(); st.Enabled || len(st.Newer) != 0 {
		t.Fatalf("%+v", st)
	}
}
