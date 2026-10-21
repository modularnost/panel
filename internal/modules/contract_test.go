package modules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fetch(t *testing.T, body string) (Manifest, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	r := NewRegistry(nil, "secret")
	m := Module{Name: "m", BaseURL: srv.URL}
	err := r.fetchManifest(context.Background(), &m)
	return m.Manifest, err
}

func TestManifestContract(t *testing.T) {
	good, err := fetch(t, `{"contract":3,"name":"m","contributions":[
		{"slot":"nav","title":"M","path":"/"},
		{"slot":"service.tab","title":"Build","path":"/service","role":"viewer","match":{"build":"on"}},
		{"slot":"dashboard.widget","title":"Later","path":"/w"},
		{"slot":"nav","title":"Newer","path":"/x","someday":true}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	// The slot the core does not know is dropped; the field it does not know is ignored.
	if len(good.Contributions) != 3 || good.Contributions[0].Role != "operator" || good.Contributions[1].Match["build"] != "on" {
		t.Fatalf("%+v", good.Contributions)
	}

	for name, c := range map[string]struct{ body, want string }{
		"no contract (an old manifest)": {`{"name":"m","nav_entries":[{"title":"M","path":"/"}]}`, "no \"contract\""},
		"the contract of frames":        {`{"contract":2}`, "contract 2"},
		"a newer contract":              {`{"contract":4}`, "contract 4"},
		"an unknown role":               {`{"contract":3,"contributions":[{"slot":"nav","title":"M","path":"/","role":"root"}]}`, "unknown role"},
		"a path that climbs":            {`{"contract":3,"contributions":[{"slot":"nav","title":"M","path":"/a/../b"}]}`, "must start with /"},
		"a relative path":               {`{"contract":3,"contributions":[{"slot":"nav","title":"M","path":"a"}]}`, "must start with /"},
		"no title":                      {`{"contract":3,"contributions":[{"slot":"nav","path":"/"}]}`, "no title"},
	} {
		if _, err := fetch(t, c.body); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", name, err, c.want)
		}
	}
}

func TestRoleForUsesTheLongestDeclaredPrefix(t *testing.T) {
	m := Module{Manifest: Manifest{Contributions: []Contribution{
		{Slot: SlotNav, Path: "/", Role: "viewer"},
		{Slot: SlotServiceTab, Path: "/service", Role: "operator"},
		{Slot: SlotServicesAction, Path: "/new/", Role: "admin"},
	}}}
	for path, want := range map[string]string{
		"/":                 "viewer",
		"/report":           "viewer", // the module's own XHR needs no contribution of its own
		"/service":          "operator",
		"/service/42":       "operator",
		"/servicex":         "viewer", // not a segment boundary
		"/new":              "admin",
		"/new/step":         "admin",
		"/service/../new":   "admin", // cleaned before it is compared
		"/service/%2e%2e/x": "operator",
	} {
		if got := m.RoleFor(path); got != want {
			t.Errorf("RoleFor(%q) = %q, want %q", path, got, want)
		}
	}

	if got := (Module{}).RoleFor("/anything"); got != "operator" {
		t.Errorf("a path nothing covers needs operator, got %q", got)
	}
}

func TestSignUser(t *testing.T) {
	sig := SignUser("tok", "m", "ann", "viewer", 100)
	if len(sig) != 64 || sig != SignUser("tok", "m", "ann", "viewer", 100) {
		t.Fatalf("not stable: %q", sig)
	}
	for name, other := range map[string]string{
		"another token":  SignUser("tok2", "m", "ann", "viewer", 100),
		"another module": SignUser("tok", "n", "ann", "viewer", 100),
		"another user":   SignUser("tok", "m", "bob", "viewer", 100),
		"another role":   SignUser("tok", "m", "ann", "admin", 100),
		"another time":   SignUser("tok", "m", "ann", "viewer", 101),
	} {
		if other == sig {
			t.Errorf("%s must change the signature", name)
		}
	}
}
