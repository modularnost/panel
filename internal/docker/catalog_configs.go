package docker

import (
	"errors"
	"fmt"
	"sort"

	"go.yaml.in/yaml/v3"
)

type BundleConfig struct {
	Alias string
	Data  []byte
}

func (b ModuleBundle) CatalogConfigFiles() ([]BundleConfig, error) {
	configs := make(map[string]catalogConfigSource)
	for _, file := range b.StackFiles {
		content, err := catalogComposeFragment(b.Files[file])
		if err != nil {
			return nil, err
		}
		var fragment catalogCompose
		if err := yaml.Unmarshal(content, &fragment); err != nil {
			return nil, err
		}
		for alias, source := range fragment.Configs {
			if prior, ok := configs[alias]; ok && prior.File != source.File {
				return nil, fmt.Errorf("catalog config %q has conflicting file declarations", alias)
			}
			configs[alias] = source
		}
	}
	out := make([]BundleConfig, 0, len(configs))
	for alias, source := range configs {
		data, ok := b.Files[source.File]
		if !ok || len(data) == 0 {
			return nil, fmt.Errorf("catalog config %q has a missing or empty bundle file", alias)
		}
		out = append(out, BundleConfig{alias, data})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out, nil
}

func externalCatalogConfigs(raw []byte, names map[string]string) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	configs, _ := doc["configs"].(map[string]any)
	for alias := range configs {
		name := names[alias]
		if !ValidName(name) {
			return nil, errors.New("missing catalog config reference")
		}
		configs[alias] = map[string]any{"external": true, "name": name}
	}
	return yaml.Marshal(doc)
}
