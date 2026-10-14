package modules

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDocument(t *testing.T) {
	points := func(n int) string { return strings.Repeat("[1,2],", n-1) + "[1,2]" }
	series := func(n int) string {
		return strings.Repeat(`{"name":"s","points":[]},`, n-1) + `{"name":"s","points":[]}`
	}

	good, err := ParseDocument(strings.NewReader(`{"ui":1,"blocks":[
		{"type":"text","text":"hi"},
		{"type":"somethingnew","whatever":[1,2]},
		{"type":"chart","series":[{"name":"cpu","points":[[1,2],[3,4]]}]}]}`))
	if err != nil || len(good.Blocks) != 3 {
		t.Fatalf("%+v, %v", good, err)
	}
	// A block the panel does not know passes: the renderer shows it as unsupported.
	if good.Blocks[1]["type"] != "somethingnew" {
		t.Errorf("%+v", good.Blocks[1])
	}
	if empty, err := ParseDocument(strings.NewReader(`{"ui":1}`)); err != nil || empty.Blocks == nil {
		t.Errorf("no blocks is an empty page, not null: %+v, %v", empty, err)
	}

	for name, c := range map[string]struct{ body, want string }{
		"not JSON":                 {`<html>`, "not valid"},
		"no ui":                    {`{"blocks":[]}`, `no "ui"`},
		"a newer ui":               {`{"ui":2,"blocks":[]}`, "ui 2"},
		"a block that is a list":   {`{"ui":1,"blocks":[[]]}`, "not valid"},
		"a block with no type":     {`{"ui":1,"blocks":[{"text":"x"}]}`, "no type"},
		"too many blocks":          {`{"ui":1,"blocks":[` + strings.Repeat(`{"type":"text"},`, MaxBlocks) + `{"type":"text"}]}`, "blocks, at most"},
		"too many series":          {fmt.Sprintf(`{"ui":1,"blocks":[{"type":"chart","series":[%s]}]}`, series(MaxSeries+1)), "series, at most"},
		"too many points":          {fmt.Sprintf(`{"ui":1,"blocks":[{"type":"chart","series":[{"points":[%s]}]}]}`, points(MaxPoints+1)), "points, at most"},
		"more than the size limit": {`{"ui":1,"blocks":[{"type":"text","text":"` + strings.Repeat("x", MaxDocumentBytes) + `"}]}`, "bytes"},
	} {
		if _, err := ParseDocument(strings.NewReader(c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", name, err, c.want)
		}
	}
}

func TestUIPageIsOnlyWhatTheManifestDeclared(t *testing.T) {
	m := Module{Manifest: Manifest{Contributions: []Contribution{
		{Slot: SlotNav, Path: "/", Render: RenderUI},
		{Slot: SlotServiceTab, Path: "/service/", Render: RenderUI},
		{Slot: SlotNav, Path: "/framed", Render: RenderFrame},
	}}}
	for p, want := range map[string]bool{
		"/": true, "": true, "/service": true, "/service/": true, "/a/../service": true,
		"/framed":    false, // a frame is not a document
		"/other":     false,
		"/service/x": false, // a page is exact: a prefix does not declare what is below it
	} {
		if _, ok := m.UIPage(p); ok != want {
			t.Errorf("UIPage(%q) = %v, want %v", p, ok, want)
		}
	}
}

func TestFetchPageSendsTheViewerSigned(t *testing.T) {
	var got http.Header
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, gotPath, gotQuery = r.Header.Clone(), r.URL.Path, r.URL.RawQuery
		w.Write([]byte(`{"ui":1,"blocks":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	r := NewRegistry(nil, "secret")
	m := Module{Name: "m", BaseURL: srv.URL}
	d, err := r.FetchPage(context.Background(), m, "/service", "service=shop&range=6h", Viewer{"ann", "viewer"})
	if err != nil || len(d.Blocks) != 1 {
		t.Fatalf("%+v, %v", d, err)
	}
	if gotPath != "/service" || gotQuery != "service=shop&range=6h" {
		t.Errorf("asked for %s?%s", gotPath, gotQuery)
	}
	token := r.Token("m")
	if got.Get("X-Panel-Token") != token || got.Get("X-Panel-User") != "ann" || got.Get("X-Panel-Role") != "viewer" {
		t.Errorf("headers: %v", got)
	}
	var ts int64
	fmt.Sscan(got.Get("X-Panel-Time"), &ts)
	if got.Get("X-Panel-User-Sig") != SignUser(token, "m", "ann", "viewer", ts) {
		t.Errorf("the viewer is not signed the way the module verifies it")
	}
	if got.Get("Cookie") != "" || got.Get("Authorization") != "" {
		t.Errorf("a credential went to the module: %v", got)
	}
}

func TestFetchPageFailures(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"the module answers an error":  {http.StatusInternalServerError, "boom", "500"},
		"the answer is not a document": {http.StatusOK, "<html>", "not valid"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		_, err := NewRegistry(nil, "s").FetchPage(context.Background(), Module{Name: "m", BaseURL: srv.URL}, "/", "", Viewer{})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want one containing %q", name, err, c.want)
		}
	}
}
