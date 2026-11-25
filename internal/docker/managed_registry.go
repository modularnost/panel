package docker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

const registryStack = "modularnost-builder-registry"
const registryRecipeLabel = "panel.managed-registry"

type ManagedRegistry struct {
	Endpoint       string
	Hostname       string
	BuildNetwork   string
	PullNetwork    string
	PullEntrypoint string
	Node           string
}

func ValidRegistryProbe(image, endpoint string) bool {
	named, err := reference.ParseNormalizedNamed(image)
	digest, pinned := named.(reference.Canonical)
	return err == nil && pinned && reference.Domain(named) == endpoint &&
		digest.Name()+"@"+digest.Digest().String() == image && digest.Digest().Algorithm() == "sha256"
}

func registryCompose(bundle ModuleBundle, in ManagedRegistry, configPath, recipe string, existing map[string]bool) (string, error) {
	if !bundle.RegistryCredentials {
		return "", errors.New("bundle does not declare registry credentials")
	}
	services := make(map[string]map[string]any)
	secrets := make(map[string]any)
	for _, file := range bundle.StackFiles {
		var stack struct {
			Services map[string]map[string]any `yaml:"services"`
			Secrets  map[string]any            `yaml:"secrets"`
		}
		if err := yaml.Unmarshal(bundle.Files[file], &stack); err != nil {
			return "", fmt.Errorf("invalid registry stack file %s", file)
		}
		for _, name := range []string{"registry", "registry-pull"} {
			if svc := stack.Services[name]; svc != nil {
				if services[name] != nil || svc["ports"] != nil || svc["network_mode"] != nil {
					return "", fmt.Errorf("unsupported network or duplicate service %s", name)
				}
				services[name] = svc
			}
		}
		for name, def := range stack.Secrets {
			secrets[name] = def
		}
	}
	for _, name := range []string{"registry", "registry-pull"} {
		svc := services[name]
		if svc == nil {
			return "", fmt.Errorf("missing %s in the Builder bundle", name)
		}
		deploy, _ := svc["deploy"].(map[string]any)
		if deploy == nil {
			deploy = make(map[string]any)
		}
		deploy["replicas"] = 1
		labels := map[string]string{registryRecipeLabel: recipe, "traefik.enable": "false"}
		if name == "registry-pull" && in.PullEntrypoint != "" {
			router := "traefik.tcp.routers." + registryStack + "."
			labels["traefik.enable"] = "true"
			labels["traefik.swarm.network"] = in.PullNetwork
			labels[router+"entrypoints"] = in.PullEntrypoint
			labels[router+"rule"] = fmt.Sprintf("HostSNI(%q)", in.Hostname)
			labels[router+"tls.passthrough"] = "true"
			labels[router+"service"] = registryStack
			labels["traefik.tcp.services."+registryStack+".loadbalancer.server.port"] = "443"
		}
		deploy["labels"] = labels
		deploy["placement"] = map[string]any{"constraints": []string{"node.id==" + in.Node}}
		svc["deploy"] = deploy
	}
	registry := services["registry"]
	registry["networks"] = map[string]any{"buildnet": map[string]any{"aliases": []string{"registry"}}}
	registry["volumes"] = []string{"registry_data:/var/lib/registry"}
	pull := services["registry-pull"]
	pull["networks"] = map[string]any{"buildnet": nil, "pullnet": map[string]any{"aliases": []string{"builder-registry-pull"}}}
	pull["environment"] = map[string]string{"REGISTRY_PULL_HOST": in.Hostname}
	usedSecrets := make(map[string]any)
	for name := range existing {
		delete(services, name)
	}
	for _, svc := range services {
		refs, _ := svc["secrets"].([]any)
		for _, ref := range refs {
			long, _ := ref.(map[string]any)
			name, _ := long["source"].(string)
			def, _ := secrets[name].(map[string]any)
			if def["external"] != true || len(def) != 1 {
				return "", errors.New("registry secrets must be named external secrets")
			}
			usedSecrets[name] = def
		}
	}
	raw, err := yaml.Marshal(map[string]any{
		"version":  "3.8",
		"services": services,
		"networks": map[string]any{
			"buildnet": map[string]any{"external": true, "name": in.BuildNetwork},
			"pullnet":  map[string]any{"external": true, "name": in.PullNetwork},
		},
		"volumes": map[string]any{"registry_data": map[string]any{"external": true, "name": registryStack + "_data"}},
		"configs": map[string]any{"registry_pull_caddyfile": map[string]string{"file": configPath}},
		"secrets": usedSecrets,
	})
	return string(raw), err
}

func registryRecipe(image string, in ManagedRegistry) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{image, in.Endpoint, in.BuildNetwork, in.PullNetwork, in.PullEntrypoint, in.Node}, "\n"))))
}

func (c *Client) ManagedRegistryExists(ctx context.Context, image string, in ManagedRegistry) (bool, error) {
	have, err := c.managedRegistryServices(ctx, image, in)
	return len(have) == 2, err
}

func (c *Client) managedRegistryServices(ctx context.Context, image string, in ManagedRegistry) (map[string]bool, error) {
	services, err := c.stackServices(ctx, registryStack)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	want := registryRecipe(image, in)
	for _, service := range services {
		name := service.Spec.Name
		if (name != registryStack+"_registry" && name != registryStack+"_registry-pull") || service.Spec.Labels[registryRecipeLabel] != want {
			return nil, fmt.Errorf("a different registry stack already exists: %w", errdefs.ErrConflict)
		}
		existing[strings.TrimPrefix(name, registryStack+"_")] = true
	}
	return existing, nil
}

func (c *Client) ProvisionManagedRegistry(ctx context.Context, image string, in ManagedRegistry) error {
	existing, err := c.managedRegistryServices(ctx, image, in)
	if err != nil || len(existing) == 2 {
		return err
	}
	bundle, err := c.ReadModuleBundle(ctx, image)
	if err != nil {
		return err
	}
	config := bundle.Files["scripts/registry-pull.Caddyfile"]
	if len(config) == 0 {
		return errors.New("the approved bundle has no registry pull configuration")
	}
	dir, err := os.MkdirTemp("", "panel-managed-registry-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(file, config, 0o600); err != nil {
		return err
	}
	compose, err := registryCompose(bundle, in, file, registryRecipe(image, in), existing)
	if err != nil {
		return err
	}
	volume, err := c.api.VolumeCreate(ctx, client.VolumeCreateOptions{Driver: "local", Name: registryStack + "_data"})
	if err != nil {
		return err
	}
	if volume.Volume.Driver != "local" || len(volume.Volume.Options) != 0 {
		return fmt.Errorf("registry storage requires a local volume without driver options: %w", errdefs.ErrConflict)
	}
	_, err = c.deployStack(ctx, registryStack, compose, nil, nil, false)
	return err
}

func (c *Client) RegistryProbe(ctx context.Context, endpoint, image, encodedAuth string) (string, error) {
	info, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return "", err
	}
	if config := info.Info.RegistryConfig; config != nil {
		if index := config.IndexConfigs[endpoint]; index != nil && !index.Secure {
			return "insecure_registry", nil
		}
		host := endpoint
		if name, _, err := net.SplitHostPort(endpoint); err == nil {
			host = name
		}
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return "endpoint_unavailable", nil
		}
		for _, prefix := range config.InsecureRegistryCIDRs {
			for _, ip := range addrs {
				if prefix.Contains(ip.Unmap()) {
					return "insecure_registry", nil
				}
			}
		}
	}
	err = c.pullRegistryProbe(ctx, image, "")
	if err == nil {
		return "anonymous_pull_allowed", nil
	}
	msg := strings.ToLower(err.Error())
	if !errdefs.IsUnauthorized(err) && !strings.Contains(msg, "unauthorized") && !strings.Contains(msg, "authentication required") && !strings.Contains(msg, "no basic auth credentials") {
		return "anonymous_pull_unverified", nil
	}
	if err := c.pullRegistryProbe(ctx, image, encodedAuth); err != nil {
		return "authorized_pull_failed", nil
	}
	return "verified", nil
}

func (c *Client) pullRegistryProbe(ctx context.Context, image, encodedAuth string) error {
	pull, err := c.api.ImagePull(ctx, image, client.ImagePullOptions{RegistryAuth: encodedAuth})
	if err != nil {
		return err
	}
	defer pull.Close()
	return pull.Wait(ctx)
}
