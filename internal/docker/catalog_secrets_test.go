package docker

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCatalogSecretBindings(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI required")
	}
	bundle := ModuleBundle{StackFiles: []string{"stack.yml"}, Files: map[string][]byte{
		"stack.yml": []byte("services:\n  module:\n    secrets:\n      - source: cert\n        target: certificate\nsecrets:\n  cert:\n    external: true\n"),
	}}
	spec := CatalogStack{Image: "docker.io/library/busybox@sha256:" + strings.Repeat("a", 64), PanelNetwork: "panelnet"}
	compose, _, err := RenderCatalogStack(t.Context(), bundle, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BindCatalogSecrets(compose, nil); err == nil {
		t.Fatal("accepted a missing binding")
	}
	out, err := BindCatalogSecrets(compose, map[string]string{"cert": "certificate-v2"})
	if err != nil || !strings.Contains(out, "certificate-v2") || !strings.Contains(out, "source: cert") {
		t.Fatalf("invalid external secret binding: %v\n%s", err, out)
	}
	if _, err := catalogComposeFragment([]byte("services: {module: {}}\nsecrets: {key: {file: /etc/shadow}}\n")); err == nil {
		t.Fatal("accepted a secret file from the panel host")
	}
}
