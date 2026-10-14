package stackfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const before = `services:
  traefik:
    image: traefik:v3
  panel:
    image: ghcr.io/modularnost/panel:0.1.0
    environment:
      IMAGE_NOTE: "image: not a line of its own"
  agent:
    image: ghcr.io/modularnost/panel:0.1.0
`

const after = `services:
  traefik:
    image: traefik:v3
  panel:
    image: ghcr.io/modularnost/panel:0.2.0
    environment:
      IMAGE_NOTE: "image: not a line of its own"
  agent:
    image: ghcr.io/modularnost/panel:0.2.0
`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRetagMovesOnlyOurOwnImage(t *testing.T) {
	path := write(t, before)
	changed, err := Retag(path, "ghcr.io/modularnost/panel:0.2.0")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != after {
		t.Fatalf("got:\n%s", got)
	}
	// Windows has no permission bits to keep.
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v: the file holds secrets", info.Mode().Perm())
	}
}

func TestRetagIsANoOpWhenItAlreadyMatches(t *testing.T) {
	path := write(t, after)
	changed, err := Retag(path, "ghcr.io/modularnost/panel:0.2.0")
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestRetagLeavesAForeignRepositoryAlone(t *testing.T) {
	path := write(t, before)
	if changed, err := Retag(path, "registry.example.com/other:1"); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != before {
		t.Fatal("the file must not change")
	}
}

func TestRetagMissingFile(t *testing.T) {
	if _, err := Retag(filepath.Join(t.TempDir(), "nope.yml"), "x:1"); err == nil {
		t.Fatal("want an error")
	}
}
