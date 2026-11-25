package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

type catalogCompose struct {
	Version  string                         `yaml:"version,omitempty"`
	Services map[string]catalogService      `yaml:"services"`
	Configs  map[string]catalogConfigSource `yaml:"configs,omitempty"`
}

type catalogService struct {
	Configs     []catalogConfigMount `yaml:"configs,omitempty"`
	Image       string               `yaml:"image,omitempty"`
	Command     any                  `yaml:"command,omitempty"`
	Entrypoint  any                  `yaml:"entrypoint,omitempty"`
	Environment map[string]string    `yaml:"environment,omitempty"`
	Healthcheck *struct {
		Test        any    `yaml:"test,omitempty"`
		Interval    string `yaml:"interval,omitempty"`
		Timeout     string `yaml:"timeout,omitempty"`
		StartPeriod string `yaml:"start_period,omitempty"`
		Retries     int    `yaml:"retries,omitempty"`
	} `yaml:"healthcheck,omitempty"`
}

type catalogConfigSource struct {
	File string `yaml:"file"`
}

type catalogConfigMount struct {
	Source string  `yaml:"source"`
	Target string  `yaml:"target"`
	UID    string  `yaml:"uid,omitempty"`
	GID    string  `yaml:"gid,omitempty"`
	Mode   *uint32 `yaml:"mode,omitempty"`
}

func catalogComposeFragment(data []byte) ([]byte, error) {
	var fragment catalogCompose
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fragment); err != nil {
		return nil, fmt.Errorf("unsupported module Compose file: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || len(fragment.Services) == 0 {
		return nil, errors.New("module Compose files must contain one document with services")
	}
	for name := range fragment.Services {
		if !ValidName(name) || len(name) > 32 {
			return nil, fmt.Errorf("invalid module service name %q", name)
		}
	}
	for alias, config := range fragment.Configs {
		if !ValidName(alias) || !bundlePath.MatchString(config.File) {
			return nil, errors.New("catalog configs must use relative bundle file paths")
		}
	}
	for _, service := range fragment.Services {
		for _, ref := range service.Configs {
			if !ValidName(ref.Source) || !validConfigTarget(ref.Target) {
				return nil, errors.New("catalog config references require a source alias and an absolute target")
			}
		}
	}
	fragment.Version = "3.9"
	return yaml.Marshal(fragment)
}

type CatalogStack struct {
	Image, PanelNetwork, CoreURL string
	Labels                       map[string]string
	ConfigNames                  map[string]string
}

// RenderCatalogStack compiles the supported subset without resolving the panel's environment.
func RenderCatalogStack(ctx context.Context, bundle ModuleBundle, spec CatalogStack) (string, int, error) {
	dir, err := os.MkdirTemp("", "panel-catalog-stack-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)
	args := []string{"stack", "config", "--skip-interpolation"}
	for i, file := range bundle.StackFiles {
		content, err := catalogComposeFragment(bundle.Files[file])
		if err != nil {
			return "", 0, fmt.Errorf("module Compose file %q: %w", file, err)
		}
		content, err = externalCatalogConfigs(content, spec.ConfigNames)
		if err != nil {
			return "", 0, err
		}
		dest := filepath.Join(dir, fmt.Sprintf("%d.yml", i))
		if err := os.WriteFile(dest, content, 0600); err != nil {
			return "", 0, err
		}
		args = append(args, "--compose-file", dest)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	rendered, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("merge module stack: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return protectCatalogStack(rendered, spec)
}

func protectCatalogStack(rendered []byte, spec CatalogStack) (string, int, error) {
	var document struct {
		Services map[string]map[string]any `yaml:"services"`
		Configs  map[string]map[string]any `yaml:"configs"`
	}
	if err := yaml.Unmarshal(rendered, &document); err != nil {
		return "", 0, err
	}
	primary := document.Services["module"]
	if primary == nil {
		return "", 0, errors.New("module stack requires a service named module")
	}
	for name, service := range document.Services {
		if name != "module" {
			image, _ := service["image"].(string)
			if !digestPinnedImage(image) {
				return "", 0, fmt.Errorf("module service %q needs an image pinned by sha256 digest", name)
			}
		}
		service["networks"] = []string{"default"}
	}
	primary["image"] = spec.Image
	primary["networks"] = []string{"default", "panel"}
	primary["deploy"] = map[string]any{"labels": spec.Labels}
	env, _ := primary["environment"].(map[string]any)
	if env == nil {
		env = make(map[string]any)
	}
	env["PANEL_CORE_URL"] = spec.CoreURL
	primary["environment"] = env
	protected := map[string]any{
		"version": "3.9", "services": document.Services,
		"networks": map[string]any{"panel": map[string]any{"external": true, "name": spec.PanelNetwork}},
	}
	if len(document.Configs) > 0 {
		protected["configs"] = document.Configs
	}
	out, err := yaml.Marshal(protected)
	return strings.ReplaceAll(string(out), "$", "$$"), len(document.Services), err
}

func (c *Client) RemoveCatalogStack(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	services, err := c.stackServices(ctx, name)
	if err != nil {
		return err
	}
	for _, service := range services {
		_, err := c.api.ServiceRemove(ctx, service.ID, client.ServiceRemoveOptions{})
		if err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("remove module service %s: %w", service.Spec.Name, err)
		}
	}
	filters := make(client.Filters).Add("label", stackLabel+"="+name)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		networks, err := c.api.NetworkList(ctx, client.NetworkListOptions{Filters: filters})
		if err != nil {
			return err
		}
		if len(networks.Items) == 0 {
			return nil
		}
		for _, network := range networks.Items {
			_, err = c.api.NetworkRemove(ctx, network.ID, client.NetworkRemoveOptions{})
			if err != nil && !errdefs.IsNotFound(err) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("remove module stack networks: %w", errors.Join(ctx.Err(), err))
		case <-ticker.C:
		}
	}
}
