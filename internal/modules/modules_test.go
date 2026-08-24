package modules

import "testing"

func TestTokenAndPermissions(t *testing.T) {
	r := NewRegistry(nil, "secret")
	tok := r.Token("hello")
	if !r.ValidToken("hello", tok) {
		t.Fatal("a module's own token was rejected")
	}
	// One module's token must not work for another.
	if r.ValidToken("evil", tok) {
		t.Fatal("token accepted for a different name")
	}
	if r.ValidToken("hello", "") || r.ValidToken("hello", "deadbeef") {
		t.Fatal("empty or forged token accepted")
	}
	// Different secrets must produce different tokens.
	if NewRegistry(nil, "other").Token("hello") == tok {
		t.Fatal("token does not depend on the secret")
	}

	m := Module{Manifest: Manifest{Permissions: []string{"service:read"}}}
	if !m.Allowed("service:read") || m.Allowed("service:update") {
		t.Fatal("permission check is lying")
	}
}
