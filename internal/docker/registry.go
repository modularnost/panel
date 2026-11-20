package docker

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/registry"
)

func RegistryHost(image string) string {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return ""
	}
	return reference.Domain(ref)
}

func RegistryAddress(host string) string {
	if host == "docker.io" {
		return "https://index.docker.io/v1/"
	}
	return host
}

func writeRegistryConfig(dir string, auth *registry.AuthConfig) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
	config, err := json.Marshal(map[string]any{
		"auths": map[string]map[string]string{auth.ServerAddress: {"auth": encoded}},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), config, 0o600)
}

func registryEnv(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "DOCKER_CONFIG") {
			out = append(out, entry)
		}
	}
	return append(out, "DOCKER_CONFIG="+dir)
}
