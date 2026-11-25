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

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func fixtureService(t *testing.T) (*Client, string, string) {
	t.Helper()
	image := os.Getenv("PANEL_SWARM_FIXTURE")
	if image == "" {
		t.Skip("set PANEL_SWARM_FIXTURE to a Swarm image with sleep")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	name := "panel-fixture-" + rand.Text()
	one := uint64(1)
	local, err := c.LocalSwarmNode(t.Context())
	if err != nil || local == "" {
		t.Fatalf("expected a Swarm manager: %v", err)
	}
	res, err := c.api.ServiceCreate(t.Context(), client.ServiceCreateOptions{Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: image, Command: []string{"sleep", "300"}},
			Placement:     &swarm.Placement{Constraints: []string{"node.id==" + local}},
		},
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.RemoveService(ctx, res.ID)
	})
	return c, res.ID, name
}

func TestSwarmSecretLifecycle(t *testing.T) {
	c, service, name := fixtureService(t)
	ctx := t.Context()
	const target = "credential"
	var ids []string
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.SetServiceSecret(ctx, service, target, "")
		for _, id := range ids {
			_ = c.RemoveSecret(ctx, id)
		}
	}()
	for _, version := range []string{"one", "two"} {
		id, err := c.CreateSecret(ctx, name+"-"+version, []byte("fixture "+version))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err := c.SetServiceSecret(ctx, service, target, id); err != nil {
			t.Fatal(err)
		}
		awaitFixtureFile(t, name, "/run/secrets/"+target, "fixture "+version)
		if err := c.RemoveSecret(ctx, id); err == nil {
			t.Fatal("removed an attached secret")
		}
		rows, err := c.Secrets(ctx)
		if err != nil || !slices.ContainsFunc(rows, func(row Secret) bool {
			return row.ID == id && slices.Contains(row.Attachments, SecretAttachment{name, target})
		}) {
			t.Fatalf("secret attachment missing: %v", err)
		}
	}
	if err := c.SetServiceSecret(ctx, service, target, ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := c.RemoveSecret(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
}

func awaitFixtureFile(t *testing.T, service, file, expected string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "label=com.docker.swarm.service.name="+service).Output()
		if err == nil {
			for _, id := range strings.Fields(string(out)) {
				data, err := exec.CommandContext(ctx, "docker", "exec", id, "cat", file).Output()
				if err == nil && string(data) == expected {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fixture did not receive the expected contents at %s", file)
		case <-ticker.C:
		}
	}
}
