package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const caddyFixture = ":8080 {\n  respond \"fixture\"\n}\n"

func TestCatalogConfigFiles(t *testing.T) {
	base := []byte("services:\n  module:\n    configs:\n      - source: caddy\n        target: /etc/caddy/Caddyfile\nconfigs:\n  caddy:\n    file: assets/Caddyfile\n")
	bundle := ModuleBundle{
		StackFiles: []string{"stack.yml"},
		Files:      map[string][]byte{"stack.yml": base, "assets/Caddyfile": []byte(caddyFixture)},
	}
	configs, err := bundle.CatalogConfigFiles()
	if err != nil || len(configs) != 1 || string(configs[0].Data) != caddyFixture {
		t.Fatalf("loading packaged Caddyfile: %v", err)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		image := "docker.io/library/caddy@sha256:" + strings.Repeat("a", 64)
		compose, _, err := RenderCatalogStack(t.Context(), bundle, CatalogStack{Image: image, PanelNetwork: "panelnet", ConfigNames: map[string]string{"caddy": "versioned-caddy"}})
		if err != nil || !strings.Contains(compose, "versioned-caddy") || strings.Contains(compose, "assets/Caddyfile") || strings.Contains(compose, caddyFixture) {
			t.Fatalf("rendered catalog config reference: %v\n%s", err, compose)
		}
	}
	for _, file := range []string{"../Caddyfile", "/etc/shadow", "assets/missing"} {
		bundle.Files["stack.yml"] = bytes.ReplaceAll(base, []byte("assets/Caddyfile"), []byte(file))
		if _, err := bundle.CatalogConfigFiles(); err == nil {
			t.Errorf("accepted %q", file)
		}
	}
}

func TestSwarmConfigLifecycle(t *testing.T) {
	image := os.Getenv("PANEL_SWARM_CADDY")
	if image == "" {
		t.Skip("set PANEL_SWARM_CADDY to a public Caddy image")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	name := "panel-caddy-" + rand.Text()
	local, err := c.LocalSwarmNode(ctx)
	if err != nil || local == "" {
		t.Fatalf("Swarm manager required: %v", err)
	}
	one := uint64(1)
	res, err := c.api.ServiceCreate(ctx, client.ServiceCreateOptions{Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: image},
			Placement: &swarm.Placement{Constraints: []string{"node.id==" + local}}},
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.RemoveService(ctx, res.ID)
		for _, id := range ids {
			_ = c.RemoveConfig(ctx, id)
		}
	}()
	for _, version := range []string{"one", "two"} {
		data := []byte(strings.ReplaceAll(caddyFixture, "fixture", version))
		id, err := c.CreateConfig(ctx, name+"-"+version, data)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err := c.SetServiceConfig(ctx, res.ID, "/etc/caddy/Caddyfile", id); err != nil {
			t.Fatal(err)
		}
		awaitFixtureFile(t, name, "/etc/caddy/Caddyfile", string(data))
		meta, got, err := c.InspectConfig(ctx, id)
		if err != nil || !bytes.Equal(data, got) || len(meta.Attachments) != 1 || meta.Attachments[0].Service != name {
			t.Fatalf("inspect attached config: %v, %v", meta, err)
		}
		if err := c.RemoveConfig(ctx, id); err == nil {
			t.Fatal("removed an attached config")
		}
	}
	if err := c.SetServiceConfig(ctx, res.ID, "/etc/caddy/Caddyfile", ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := c.RemoveConfig(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalogConfigsComposeWithSecretsAndVolumes(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI is required for Compose merge")
	}
	bundle := ModuleBundle{
		StackFiles: []string{"stack.yml"},
		Files: map[string][]byte{
			"assets/Caddyfile": []byte(caddyFixture),
			"stack.yml": []byte(`
services:
  module:
    configs:
      - source: caddy
        target: /etc/caddy/Caddyfile
    secrets:
      - source: credential
        target: credential
    volumes:
      - type: volume
        source: data
        target: /data
configs:
  caddy: {file: assets/Caddyfile}
secrets:
  credential: {external: true}
volumes:
  data: {}
`),
		},
	}
	spec := CatalogStack{
		Image:        "docker.io/library/caddy@sha256:" + strings.Repeat("a", 64),
		PanelNetwork: "panelnet",
		ConfigNames:  map[string]string{"caddy": "fixture-config"},
		Volumes:      map[string]CatalogVolume{"data": {Name: "fixture-data", Node: "fixtureworker"}},
	}
	compose, _, err := RenderCatalogStack(t.Context(), bundle, spec)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindCatalogSecrets(compose, map[string]string{"credential": "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"name: fixture-config", "name: fixture-data", "name: fixture-secret", "node.id==fixtureworker"} {
		if !strings.Contains(bound, expected) {
			t.Errorf("rendered Compose is missing %q:\n%s", expected, bound)
		}
	}
}
