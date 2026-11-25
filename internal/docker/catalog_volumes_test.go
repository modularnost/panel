package docker

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func TestCatalogVolumesPinConsumers(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI needed for Compose merge")
	}
	raw := `
services:
  module:
    volumes:
      - type: volume
        source: data
        target: /data
    secrets:
      - source: app_key
        target: app.key
volumes:
  data: {}
secrets:
  app_key:
    external: true
`
	bundle := ModuleBundle{StackFiles: []string{"stack.yml"}, Files: map[string][]byte{"stack.yml": []byte(raw)}}
	aliases, err := bundle.CatalogVolumeAliases()
	if err != nil || !slices.Equal(aliases, []string{"data"}) {
		t.Fatalf("catalog aliases: %v, %v", aliases, err)
	}
	spec := CatalogStack{Image: "docker.io/library/busybox@sha256:" + strings.Repeat("a", 64), PanelNetwork: "panelnet",
		Volumes: map[string]CatalogVolume{"data": {Name: "stored-data", Node: "fixtureworker"}}}
	compose, _, err := RenderCatalogStack(t.Context(), bundle, spec)
	if err != nil {
		t.Fatal(err)
	}
	compose, err = BindCatalogSecrets(compose, map[string]string{"app_key": "saved-secret"})
	if err != nil || !strings.Contains(compose, "node.id==fixtureworker") || !strings.Contains(compose, "name: stored-data") || !strings.Contains(compose, "name: saved-secret") {
		t.Fatalf("invalid persistent Compose: %v\n%s", err, compose)
	}
}

func TestSwarmVolumeLifecycle(t *testing.T) {
	workerHost := os.Getenv("PANEL_SWARM_WORKER_HOST")
	image := os.Getenv("PANEL_SWARM_FIXTURE")
	if workerHost == "" || image == "" {
		t.Skip("requires an isolated Swarm manager and PANEL_SWARM_WORKER_HOST")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	workerAPI, err := client.New(client.WithHost(workerHost))
	if err != nil {
		t.Fatal(err)
	}
	defer workerAPI.Close()
	worker := &Client{api: workerAPI}
	ctx := t.Context()
	node, err := worker.LocalSwarmNode(ctx)
	if err != nil || node == "" {
		t.Fatalf("Swarm worker required: %v", err)
	}
	manager, err := c.LocalSwarmNode(ctx)
	if err != nil || manager == node {
		t.Fatalf("a distinct manager is required: %v", err)
	}
	name := "panel-volume-" + rand.Text()
	if _, err := worker.CreateLocalVolume(ctx, name); err != nil {
		t.Fatal(err)
	}
	var service string
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if service != "" {
			_ = c.RemoveService(ctx, service)
		}
		_ = worker.RemoveLocalVolume(ctx, name)
	}()
	if _, err := c.LocalVolume(ctx, name); !errdefs.IsNotFound(err) {
		t.Fatalf("volume unexpectedly exists on manager: %v", err)
	}
	one := uint64(1)
	res, err := c.api.ServiceCreate(ctx, client.ServiceCreateOptions{Spec: swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: image, Command: []string{"sleep", "300"}}},
		Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	service = res.ID
	if err := c.AttachServiceVolume(ctx, service, node, name, "/data", false); err != nil {
		t.Fatal(err)
	}
	before := awaitVolumeTask(t, c, service, node, "")
	execVolumeFixture(t, workerHost, before.Status.ContainerStatus.ContainerID, "sh", "-c", "printf retained >/data/value")
	if _, err := c.ForceUpdate(ctx, service, nil); err != nil {
		t.Fatal(err)
	}
	after := awaitVolumeTask(t, c, service, node, before.ID)
	if out := execVolumeFixture(t, workerHost, after.Status.ContainerStatus.ContainerID, "cat", "/data/value"); out != "retained" {
		t.Fatal("worker volume lost its data after redeploy")
	}
	if err := c.RemoveService(ctx, service); err != nil {
		t.Fatal(err)
	}
	service = ""
	if _, err := worker.LocalVolume(ctx, name); err != nil {
		t.Fatalf("volume missing after uninstall: %v", err)
	}
}
func awaitVolumeTask(t *testing.T, c *Client, service, node, old string) swarm.Task {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		tasks, err := c.api.TaskList(ctx, client.TaskListOptions{Filters: make(client.Filters).Add("service", service)})
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks.Items {
			if task.Status.State == swarm.TaskStateRunning && task.DesiredState == swarm.TaskStateRunning && task.NodeID == node && task.ID != old && task.Status.ContainerStatus != nil {
				return task
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("service did not converge on selected worker: %v", tasks.Items)
		case <-ticker.C:
		}
	}
}

func execVolumeFixture(t *testing.T, host, container string, command ...string) string {
	t.Helper()
	args := append([]string{"--host", host, "exec", container}, command...)
	out, err := exec.CommandContext(t.Context(), "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("worker fixture command: %v: %s", err, out)
	}
	return string(out)
}
