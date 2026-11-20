package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/registry"
)

func TestRegistryConfigContainsOnlySelectedHost(t *testing.T) {
	dir := t.TempDir()
	auth := &registry.AuthConfig{Username: "user", Password: "token", ServerAddress: RegistryAddress("docker.io")}
	if err := writeRegistryConfig(dir, auth); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Auths map[string]struct{ Auth string }
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Auths) != 1 || cfg.Auths["https://index.docker.io/v1/"].Auth != "dXNlcjp0b2tlbg==" {
		t.Fatalf("unexpected Docker config: %s", data)
	}
	env := registryEnv([]string{"DOCKER_HOST=tcp://localhost:2375", "DOCKER_CONFIG=other"}, dir)
	if !slices.Equal(env, []string{"DOCKER_HOST=tcp://localhost:2375", "DOCKER_CONFIG=" + dir}) {
		t.Fatalf("unexpected Docker environment: %v", env)
	}
}
