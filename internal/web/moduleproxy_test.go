package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The module is trusted code, but its page shares the panel's origin: it must
// not see the session, and it must not be able to write to the panel's cookies.
func TestModuleProxyKeepsCookiesAndCredentialsApart(t *testing.T) {
	var seen http.Header
	module := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		http.SetCookie(w, &http.Cookie{Name: "panel_session", Value: "planted", Path: "/"})
		w.Write([]byte("page " + r.URL.Path))
	}))
	defer module.Close()
	target, _ := url.Parse(module.URL)

	proxy := httptest.NewServer(newModuleProxy("metrics", target, "tok"))
	defer proxy.Close()

	req, _ := http.NewRequest("GET", proxy.URL+"/modules/metrics/page", nil)
	req.Header.Set("Cookie", "panel_session=secret")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if seen.Get("Cookie") != "" || seen.Get("Authorization") != "" {
		t.Errorf("the module saw the caller's credentials: cookie=%q authorization=%q", seen.Get("Cookie"), seen.Get("Authorization"))
	}
	if seen.Get("X-Panel-Module") != "metrics" || seen.Get("X-Panel-Token") != "tok" {
		t.Errorf("the module's own identity is missing: %v", seen)
	}
	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("a cookie from the module reached the browser: %v", got)
	}
}
