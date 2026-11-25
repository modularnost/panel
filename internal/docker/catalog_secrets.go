package docker

import (
	"errors"

	"go.yaml.in/yaml/v3"
)

func BindCatalogSecrets(compose string, bindings map[string]string) (string, error) {
	var document map[string]any
	if err := yaml.Unmarshal([]byte(compose), &document); err != nil {
		return "", err
	}
	secrets, _ := document["secrets"].(map[string]any)
	if len(secrets) != len(bindings) {
		return "", errors.New("select one secret for each declared alias")
	}
	for alias := range secrets {
		name := bindings[alias]
		if !ValidName(name) {
			return "", errors.New("select an existing secret or create one for each declared alias")
		}
		secrets[alias] = map[string]any{"external": true, "name": name}
	}
	raw, err := yaml.Marshal(document)
	return string(raw), err
}
