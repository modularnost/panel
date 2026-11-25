package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const buildKitImage = "moby/buildkit:v0.35.1@sha256:baf6138eb8c37644800c7c913fba051e840ebb9ba13eaf92d92ef9a1712d0b4c"
const buildKitName = "modularnost-builder-buildkit"
const buildKitLabel = "panel.builder.buildkit"
const buildKitReadyFile = "etc/buildkit/.panel-ready"

type BuildKitTLS struct {
	CA                 string `json:"ca" writeOnly:"true"`
	ServerCertificate  string `json:"server_certificate" writeOnly:"true"`
	ServerKey          string `json:"server_key" writeOnly:"true"`
	RegistryCA         string `json:"registry_ca" writeOnly:"true"`
	RegistryClientCert string `json:"registry_client_certificate" writeOnly:"true"`
	RegistryClientKey  string `json:"registry_client_key" writeOnly:"true"`
}

func (in BuildKitTLS) Validate() error {
	if !validBuildKitPair(in.CA, in.ServerCertificate, in.ServerKey, "buildkitd", x509.ExtKeyUsageServerAuth) ||
		!validBuildKitPair(in.RegistryCA, in.RegistryClientCert, in.RegistryClientKey, "", x509.ExtKeyUsageClientAuth) {
		return errors.New("invalid BuildKit server or registry client TLS certificate")
	}
	return nil
}

func validBuildKitPair(ca, cert, key, hostname string, usage x509.ExtKeyUsage) bool {
	pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil || pair.Leaf == nil {
		return false
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ca)) {
		return false
	}
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		if leaf, err := x509.ParseCertificate(raw); err == nil {
			intermediates.AddCert(leaf)
		}
	}
	_, err = pair.Leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: hostname, KeyUsages: []x509.ExtKeyUsage{usage}})
	return err == nil
}

const buildKitConfig = `
[worker.oci]
  networkMode = "bridge"
[dns]
  nameservers = ["1.1.1.1", "8.8.8.8"]
[grpc]
  address = ["unix:///run/buildkit/buildkitd.sock", "tcp://0.0.0.0:1234"]
  [grpc.tls]
    ca = "/etc/buildkit/tls/buildkit-ca.pem"
    cert = "/etc/buildkit/tls/buildkit-server.pem"
    key = "/etc/buildkit/tls/buildkit-server-key.pem"
[registry."registry:5000"]
  ca = ["/etc/buildkit/tls/registry-ca.pem"]
  [[registry."registry:5000".keypair]]
    cert = "/etc/buildkit/tls/registry-client.pem"
    key = "/etc/buildkit/tls/registry-client-key.pem"
`

const buildKitStartup = `set -eu
iptables -C INPUT -i buildkit0 -j DROP 2>/dev/null || iptables -I INPUT -i buildkit0 -j DROP
iptables -N CNI-ADMIN 2>/dev/null || iptables -F CNI-ADMIN
for subnet in 0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.0.0.0/24 192.168.0.0/16 198.18.0.0/15 224.0.0.0/3 ${BUILDKIT_BLOCKED_SUBNETS:?}; do
	iptables -A CNI-ADMIN -i buildkit0 -d "$subnet" -j DROP
done
ip6tables -C INPUT -i buildkit0 -j DROP 2>/dev/null || ip6tables -I INPUT -i buildkit0 -j DROP
ip6tables -C FORWARD -i buildkit0 -j DROP 2>/dev/null || ip6tables -I FORWARD -i buildkit0 -j DROP
exec /usr/bin/buildkitd-entrypoint
`

func buildKitFiles(in BuildKitTLS) ([]byte, error) {
	var buf bytes.Buffer
	out := tar.NewWriter(&buf)
	for _, dir := range []string{"etc/buildkit", "etc/buildkit/tls"} {
		if err := out.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o700}); err != nil {
			return nil, err
		}
	}
	for name, content := range map[string]string{
		"etc/buildkit/buildkitd.toml":              buildKitConfig,
		"etc/buildkit/tls/buildkit-ca.pem":         in.CA,
		"etc/buildkit/tls/buildkit-server.pem":     in.ServerCertificate,
		"etc/buildkit/tls/buildkit-server-key.pem": in.ServerKey,
		"etc/buildkit/tls/registry-ca.pem":         in.RegistryCA,
		"etc/buildkit/tls/registry-client.pem":     in.RegistryClientCert,
		"etc/buildkit/tls/registry-client-key.pem": in.RegistryClientKey,
	} {
		if err := out.WriteHeader(&tar.Header{Name: name, Size: int64(len(content)), Mode: 0o600}); err != nil {
			return nil, err
		}
		if _, err := out.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := out.WriteHeader(&tar.Header{Name: buildKitReadyFile, Mode: 0o600}); err != nil {
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Client) ProvisionBuildKit(ctx context.Context, overlay string, in BuildKitTLS) (_ string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := in.Validate(); err != nil {
		return "", fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, err)
	}
	buildNetwork, err := c.api.NetworkInspect(ctx, overlay, client.NetworkInspectOptions{Scope: "swarm"})
	if err != nil {
		return "", err
	}
	n := buildNetwork.Network
	if n.Name != overlay || n.Driver != "overlay" || !n.Internal || !n.Attachable {
		return "", fmt.Errorf("%w: the build overlay must be internal and attachable", errdefs.ErrInvalidArgument)
	}
	recipe := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{buildKitImage, buildKitStartup, buildKitConfig, n.ID,
		in.CA, in.ServerCertificate, in.ServerKey, in.RegistryCA, in.RegistryClientCert, in.RegistryClientKey}, "\n"))))
	existing, err := c.api.ContainerInspect(ctx, buildKitName, client.ContainerInspectOptions{})
	if err == nil {
		if existing.Container.Config == nil || existing.Container.Config.Labels[buildKitLabel] != recipe {
			return "", fmt.Errorf("%w: a different BuildKit container already exists", errdefs.ErrConflict)
		}
		running := existing.Container.State != nil && existing.Container.State.Running
		_, statErr := c.api.ContainerStatPath(ctx, buildKitName, client.ContainerStatPathOptions{Path: "/" + buildKitReadyFile})
		if errdefs.IsNotFound(statErr) && !running {
			if err := c.RemoveBuildKit(ctx, buildKitName); err != nil {
				return "", err
			}
			err = errdefs.ErrNotFound
		} else {
			if statErr != nil {
				return "", statErr
			}
			if existing.Container.NetworkSettings == nil || existing.Container.NetworkSettings.Networks[overlay] == nil {
				if _, err := c.api.NetworkConnect(ctx, n.ID, client.NetworkConnectOptions{Container: buildKitName, EndpointConfig: &network.EndpointSettings{Aliases: []string{"buildkitd"}}}); err != nil {
					return "", err
				}
			}
			if !running {
				if _, err := c.api.ContainerStart(ctx, buildKitName, client.ContainerStartOptions{}); err != nil {
					return "", err
				}
			}
			return "", c.waitForBuildKit(ctx, buildKitName)
		}
	}
	if !errdefs.IsNotFound(err) {
		return "", err
	}
	var subnets []string
	for _, name := range []string{"bridge", "docker_gwbridge", overlay} {
		details, err := c.api.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
		if err != nil {
			return "", err
		}
		for _, ipam := range details.Network.IPAM.Config {
			if ipam.Subnet.IsValid() && ipam.Subnet.Addr().Is4() {
				subnets = append(subnets, ipam.Subnet.String())
			}
		}
	}
	volume, err := c.api.VolumeCreate(ctx, client.VolumeCreateOptions{Driver: "local", Name: buildKitName})
	if err != nil {
		return "", err
	}
	if volume.Volume.Driver != "local" || len(volume.Volume.Options) != 0 {
		return "", fmt.Errorf("%w: BuildKit storage requires a standard local volume", errdefs.ErrConflict)
	}
	if pull, err := c.api.ImagePull(ctx, buildKitImage, client.ImagePullOptions{}); err != nil {
		return "", err
	} else {
		defer pull.Close()
		if err := pull.Wait(ctx); err != nil {
			return "", err
		}
	}
	created, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: buildKitName,
		Config: &container.Config{Image: buildKitImage, Entrypoint: []string{"/bin/sh", "-ec"},
			Cmd: []string{buildKitStartup}, Env: []string{"BUILDKIT_BLOCKED_SUBNETS=" + strings.Join(subnets, " ")},
			Labels: map[string]string{buildKitLabel: recipe}},
		HostConfig: &container.HostConfig{NetworkMode: "bridge", Privileged: true, RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: buildKitName, Target: "/var/lib/buildkit"}}},
	})
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.RemoveBuildKit(ctx, created.ID))
		}
	}()
	files, err := buildKitFiles(in)
	if err != nil {
		return "", err
	}
	if _, err := c.api.CopyToContainer(ctx, created.ID, client.CopyToContainerOptions{DestinationPath: "/", Content: bytes.NewReader(files)}); err != nil {
		return "", err
	}
	if _, err := c.api.NetworkConnect(ctx, n.ID, client.NetworkConnectOptions{Container: created.ID, EndpointConfig: &network.EndpointSettings{Aliases: []string{"buildkitd"}}}); err != nil {
		return "", err
	}
	if _, err := c.api.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return "", err
	}
	return created.ID, c.waitForBuildKit(ctx, created.ID)
}

func (c *Client) RemoveBuildKit(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_, err := c.api.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Client) waitForBuildKit(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := exec.CommandContext(ctx, "docker", "exec", id, "buildctl", "--addr", "unix:///run/buildkit/buildkitd.sock", "debug", "workers").Run(); err == nil {
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
	out, err := exec.CommandContext(ctx, "docker", "exec", buildKitName, "sh", "-ec", build).Output()
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
