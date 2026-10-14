package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAHandler(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<html>shell</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.svg":   {Data: []byte("<svg/>")},
	}
	h := spaHandler(fsys)
	get := func(path string) (int, string, string) {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control")
	}

	for _, path := range []string{"/app/", "/app/services/web", "/app/login", "/app/../../etc/passwd"} {
		if code, body, cache := get(path); code != 200 || !strings.Contains(body, "shell") || cache != "no-cache" {
			t.Errorf("%s: want the shell, got %d %q cache=%q", path, code, body, cache)
		}
	}
	code, body, cache := get("/app/assets/app.js")
	if code != 200 || body != "console.log(1)" || !strings.Contains(cache, "immutable") {
		t.Errorf("asset: %d %q cache=%q", code, body, cache)
	}
	if code, body, _ := get("/app/favicon.svg"); code != 200 || body != "<svg/>" {
		t.Errorf("a root file: %d %q", code, body)
	}
}

func TestSPAHandlerWithoutABuild(t *testing.T) {
	rec := httptest.NewRecorder()
	spaHandler(fstest.MapFS{".gitkeep": {}})(rec, httptest.NewRequest("GET", "/app/", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "not built") {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}
