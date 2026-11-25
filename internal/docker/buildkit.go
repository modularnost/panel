package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const buildKitImage = "moby/buildkit:v0.35.1@sha256:baf6138eb8c37644800c7c913fba051e840ebb9ba13eaf92d92ef9a1712d0b4c"
const buildKitName = "modularnost-builder-buildkit"
const buildKitLabel = "panel.builder.buildkit"

// The worker has SYS_ADMIN, so only reviewed recipe and startup files may run.
func approvedBuildKitRecipe(bundle ModuleBundle) ([][]byte, error) {
	files := []struct{ path, hash string }{
		{"buildkit-worker.json", "c771f614eb3a08798a3e7235b6baf8d3aa0cf5a0936c08d7a1ca37cb0cc9c4d0"},
		{"scripts/buildkitd.toml", "1026da1778a87142fb0542f3afa2fdbfdad0033cf1fbfa70a18d755391db08d4"},
		{"scripts/buildkitd-network.sh", "a6d660c4e545f55063f69e3173ca57359d1a2ab387a5a914e38f448a02b404e3"},
	}
	result := make([][]byte, 0, len(files))
	for _, file := range files {
		raw := bytes.ReplaceAll(bundle.Files[file.path], []byte("\r\n"), []byte("\n"))
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != file.hash {
			return nil, fmt.Errorf("%w: unapproved BuildKit worker file %s", errdefs.ErrInvalidArgument, file.path)
		}
		result = append(result, raw)
	}
	return result, nil
}

func (c *Client) ProvisionBuildKit(ctx context.Context, bundle ModuleBundle, overlay, node, volume string) (_ string, err error) {
	files, err := approvedBuildKitRecipe(bundle)
	if err != nil {
		return "", err
	}
	buildNetwork, err := c.api.NetworkInspect(ctx, overlay, client.NetworkInspectOptions{Scope: "swarm"})
	if err != nil {
		return "", err
	}
	n := buildNetwork.Network
	if n.Name != overlay || n.Driver != "overlay" || n.Internal || n.Ingress {
		return "", fmt.Errorf("%w: BuildKit needs a dedicated build overlay that allows internet egress", errdefs.ErrInvalidArgument)
	}
	recipe := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{buildKitImage, n.ID, node, volume}, "\n"))))
	existing, err := c.service(ctx, buildKitName)
	if err == nil {
		if existing.Spec.Labels[buildKitLabel] != recipe {
			return "", fmt.Errorf("%w: a different BuildKit worker already exists", errdefs.ErrConflict)
		}
		return "", c.waitForBuildKit(ctx)
	}
	if !errdefs.IsNotFound(err) {
		return "", err
	}

	networks, err := c.Networks(ctx)
	if err != nil {
		return "", err
	}
	var subnets []string
	for _, net := range networks {
		if net.Driver != "overlay" {
			continue
		}
		for _, addr := range net.Subnets {
			if addr.Subnet != "" {
				subnets = append(subnets, addr.Subnet)
			}
		}
	}
	gateway, err := c.api.NetworkInspect(ctx, "docker_gwbridge", client.NetworkInspectOptions{})
	if err != nil {
		return "", err
	}
	for _, addr := range gateway.Network.IPAM.Config {
		if addr.Subnet.IsValid() {
			subnets = append(subnets, addr.Subnet.String())
		}
	}
	var secrets []*swarm.SecretReference
	for alias, target := range map[string]string{
		"builder_buildkit_ca":                  "buildkit-ca.pem",
		"builder_buildkit_server":              "buildkit-server.pem",
		"builder_buildkit_server_key":          "buildkit-server-key.pem",
		"builder_registry_ca":                  "registry-ca.pem",
		"builder_buildkit_registry_client":     "registry-client.pem",
		"builder_buildkit_registry_client_key": "registry-client-key.pem",
	} {
		secret, err := c.secretReference(ctx, alias)
		if err != nil {
			return "", fmt.Errorf("read BuildKit secret %s: %w", alias, err)
		}
		secrets = append(secrets, &swarm.SecretReference{SecretID: secret.ID, SecretName: secret.Spec.Name,
			File: &swarm.SecretReferenceFileTarget{Name: "/etc/buildkit/tls/" + target, UID: "0", GID: "0", Mode: 0o400}})
	}
	var configs []*swarm.ConfigReference
	for i, target := range []string{"/etc/buildkit/buildkitd.toml", "/etc/buildkit/network.sh"} {
		data := files[i+1]
		hash := sha256.Sum256(data)
		name := fmt.Sprintf("modularnost-buildkit-config-%x", hash[:8])
		config, err := c.configReference(ctx, name)
		if errdefs.IsNotFound(err) {
			config.ID, err = c.CreateConfig(ctx, name, data)
		} else if err == nil && !bytes.Equal(config.Spec.Data, data) {
			return "", fmt.Errorf("%w: a worker config with different contents already exists", errdefs.ErrConflict)
		}
		if err != nil {
			return "", err
		}
		configs = append(configs, &swarm.ConfigReference{ConfigID: config.ID, ConfigName: name,
			File: &swarm.ConfigReferenceFileTarget{Name: target, UID: "0", GID: "0", Mode: 0o444}})
	}
	one := uint64(1)
	out, err := c.api.ServiceCreate(ctx, client.ServiceCreateOptions{Spec: swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: buildKitName, Labels: map[string]string{buildKitLabel: recipe}},
		UpdateConfig: &swarm.UpdateConfig{Order: swarm.UpdateOrderStopFirst},
		Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		TaskTemplate: swarm.TaskSpec{
			Placement: &swarm.Placement{Constraints: []string{"node.id==" + node}},
			Networks:  []swarm.NetworkAttachmentConfig{{Target: n.ID, Aliases: []string{"buildkitd"}}},
			ContainerSpec: &swarm.ContainerSpec{
				Image: buildKitImage, Command: []string{"/bin/sh"}, Args: []string{"/etc/buildkit/network.sh"},
				Env:           []string{"BUILDKIT_BLOCKED_SUBNETS=" + strings.Join(subnets, " ")},
				CapabilityAdd: []string{"SYS_ADMIN", "NET_ADMIN", "SYS_PTRACE"},
				Privileges:    &swarm.Privileges{Seccomp: &swarm.SeccompOpts{Mode: swarm.SeccompModeUnconfined}, AppArmor: &swarm.AppArmorOpts{Mode: swarm.AppArmorModeDisabled}},
				Healthcheck: &container.HealthConfig{
					Test:     []string{"CMD-SHELL", "buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers >/dev/null 2>&1 && nc -z 127.0.0.1 1234"},
					Interval: 5 * time.Second, Timeout: 5 * time.Second, StartPeriod: 5 * time.Second, Retries: 3,
				},
				Mounts:  []mount.Mount{{Type: mount.TypeVolume, Source: volume, Target: "/var/lib/buildkit"}},
				Configs: configs, Secrets: secrets,
			},
		},
	}, QueryRegistry: true})
	if err != nil {
		return "", err
	}
	if err := c.waitForBuildKit(ctx); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		return "", errors.Join(err, c.RemoveService(cleanup, out.ID))
	}
	return out.ID, nil
}

func (c *Client) buildKitTask(ctx context.Context) (string, error) {
	tasks, err := c.api.ContainerList(ctx, client.ContainerListOptions{Filters: make(client.Filters).Add("label", "com.docker.swarm.service.name="+buildKitName)})
	if err != nil {
		return "", err
	}
	for _, task := range tasks.Items {
		if exec.CommandContext(ctx, "docker", "exec", task.ID, "buildctl", "--addr", "unix:///run/buildkit/buildkitd.sock", "debug", "workers").Run() == nil {
			return task.ID, nil
		}
	}
	return "", errors.New("BuildKit worker is not ready")
}

func (c *Client) waitForBuildKit(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := c.buildKitTask(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("BuildKit did not become ready")
		case <-ticker.C:
		}
	}
}

func (c *Client) BuilderSecrets(ctx context.Context) ([]*swarm.SecretReference, error) {
	refs := make([]*swarm.SecretReference, 0, 3)
	for _, file := range []string{"ca", "client", "client-key"} {
		name := "builder_buildkit_" + strings.ReplaceAll(file, "-", "_")
		secret, err := c.secretReference(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("read Builder secret %s: %w", name, err)
		}
		refs = append(refs, &swarm.SecretReference{
			SecretID: secret.ID, SecretName: secret.Spec.Name,
			File: &swarm.SecretReferenceFileTarget{Name: "buildkit-" + file + ".pem", UID: "10001", GID: "10001", Mode: 0o400},
		})
	}
	return refs, nil
}

func (c *Client) SeedBuilderProbe(ctx context.Context, endpoint string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	const name = "modularnost/install-probe"
	const build = `set -eu
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
printf 'FROM scratch\nLABEL org.opencontainers.image.title="Modularnost registry probe"\n' > "$dir/Dockerfile"
buildctl --addr unix:///run/buildkit/buildkitd.sock build --frontend dockerfile.v0 --local context="$dir" --local dockerfile="$dir" --output type=image,name=registry:5000/modularnost/install-probe:setup,push=true --metadata-file "$dir/metadata.json" >&2
cat "$dir/metadata.json"
`
	task, err := c.buildKitTask(ctx)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "docker", "exec", task, "sh", "-ec", build).Output()
	if err != nil {
		return "", fmt.Errorf("publish the BuildKit registry probe: %w", err)
	}
	var metadata struct {
		Digest string `json:"containerimage.digest"`
	}
	if err := json.Unmarshal(out, &metadata); err != nil {
		return "", errors.New("BuildKit returned invalid registry probe metadata")
	}
	image := endpoint + "/" + name + "@" + metadata.Digest
	if !ValidRegistryProbe(image, endpoint) {
		return "", errors.New("BuildKit returned an invalid registry probe digest")
	}
	return image, nil
}

func (c *Client) VerifyBuilderConnection(ctx context.Context, service string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		tasks, err := c.api.ContainerList(ctx, client.ContainerListOptions{
			Filters: make(client.Filters).Add("label", "com.docker.swarm.service.name="+service),
		})
		if err != nil {
			return err
		}
		for _, task := range tasks.Items {
			if err := exec.CommandContext(ctx, "docker", "exec", task.ID, "buildctl", "--addr", "tcp://buildkitd:1234",
				"--tlscacert", "/run/secrets/buildkit-ca.pem", "--tlscert", "/run/secrets/buildkit-client.pem",
				"--tlskey", "/run/secrets/buildkit-client-key.pem", "debug", "workers").Run(); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("could not authenticate Builder to BuildKit with its Swarm secrets")
		case <-ticker.C:
		}
	}
}

func (c *Client) EnsureBuilderSecrets(ctx context.Context, inputs map[string]string) error {
	for name, value := range inputs {
		switch name {
		case "builder_buildkit_ca", "builder_buildkit_client", "builder_buildkit_client_key",
			"builder_buildkit_server", "builder_buildkit_server_key", "builder_buildkit_registry_client", "builder_buildkit_registry_client_key",
			"builder_registry_ca", "builder_registry_cert", "builder_registry_key",
			"builder_pull_cert", "builder_pull_key", "builder_pull_client", "builder_pull_client_key", "builder_pull_users":
		default:
			return fmt.Errorf("%w: unsupported Builder secret name", errdefs.ErrInvalidArgument)
		}
		if len(value) == 0 || len(value) > 64<<10 {
			return fmt.Errorf("%w: Builder secret must be between 1 byte and 64 KB", errdefs.ErrInvalidArgument)
		}
	}
	const label = "panel.builder.secret_digest"
	for name, value := range inputs {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
		existing, err := c.secretReference(ctx, name)
		if err == nil {
			if existing.Spec.Labels[label] != digest {
				return fmt.Errorf("%w: use the existing %s secret by omitting it from the upload", errdefs.ErrConflict, name)
			}
			continue
		}
		if !errdefs.IsNotFound(err) {
			return err
		}
		if _, err := c.api.SecretCreate(ctx, client.SecretCreateOptions{Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: name, Labels: map[string]string{label: digest}},
			Data:        []byte(value),
		}}); err != nil {
			return err
		}
	}
	return nil
}
