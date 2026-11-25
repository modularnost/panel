package docker

import (
	"errors"
	"slices"

	"go.yaml.in/yaml/v3"
)

type CatalogVolume struct{ Name, Node string }

func (b ModuleBundle) CatalogVolumeAliases() ([]string, error) {
	seen := make(map[string]bool)
	for _, name := range b.StackFiles {
		raw, err := catalogComposeFragment(b.Files[name])
		if err != nil {
			return nil, err
		}
		var doc catalogCompose
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		for alias := range doc.Volumes {
			seen[alias] = true
		}
	}
	aliases := make([]string, 0, len(seen))
	for alias := range seen {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	return aliases, nil
}

func externalCatalogVolumes(raw []byte, selected map[string]CatalogVolume) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	volumes, _ := doc["volumes"].(map[string]any)
	for alias := range volumes {
		resource, ok := selected[alias]
		if !ok || !ValidName(resource.Name) {
			return nil, errors.New("missing catalog volume reference")
		}
		volumes[alias] = map[string]any{"external": true, "name": resource.Name}
	}
	return yaml.Marshal(doc)
}

func pinCatalogVolumes(service map[string]any, selected map[string]CatalogVolume) error {
	refs, _ := service["volumes"].([]any)
	node := ""
	for _, raw := range refs {
		ref, ok := raw.(map[string]any)
		alias, _ := ref["source"].(string)
		target, _ := ref["target"].(string)
		selection, declared := selected[alias]
		if !ok || !declared || !validVolumeTarget(target) {
			return errors.New("invalid catalog volume attachment")
		}
		if selection.Node != "" {
			if node != "" && node != selection.Node {
				return errors.New("volumes used by one service must be on the same node")
			}
			node = selection.Node
		}
	}
	if node != "" {
		deploy, _ := service["deploy"].(map[string]any)
		if deploy == nil {
			deploy = make(map[string]any)
		}
		deploy["placement"] = map[string]any{"constraints": []string{"node.id==" + node}}
		service["deploy"] = deploy
	}
	return nil
}
