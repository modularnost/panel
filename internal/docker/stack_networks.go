package docker

import (
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

func yamlField(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func yamlString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlAdd(n *yaml.Node, key string, value *yaml.Node) {
	n.Content = append(n.Content, yamlString(key), value)
}

func EditStackNetworks(compose, stack, service string, add, remove []Network) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(compose), &doc); err != nil || len(doc.Content) != 1 {
		return "", fmt.Errorf("invalid Compose YAML")
	}
	root := doc.Content[0]
	svc := yamlField(yamlField(root, "services"), service)
	if svc == nil || svc.Kind != yaml.MappingNode {
		return "", fmt.Errorf("open the Compose editor to change networks for %s", service)
	}
	attached := yamlField(svc, "networks")
	if attached == nil {
		if yamlField(svc, "<<") != nil || yamlField(svc, "extends") != nil || yamlField(svc, "network_mode") != nil {
			return "", fmt.Errorf("open the Compose editor to change inherited networks or network_mode")
		}
		attached = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		yamlAdd(svc, "networks", attached)
	} else if attached.Kind != yaml.SequenceNode && attached.Kind != yaml.MappingNode {
		return "", fmt.Errorf("open the Compose editor to change this networks declaration")
	}
	if attached.Anchor != "" || svc.Anchor != "" {
		return "", fmt.Errorf("open the Compose editor to change a shared YAML anchor")
	}
	definitions := yamlField(root, "networks")
	if definitions == nil {
		definitions = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlAdd(root, "networks", definitions)
	}
	if definitions.Kind != yaml.MappingNode || definitions.Anchor != "" || yamlField(definitions, "<<") != nil {
		return "", fmt.Errorf("open the Compose editor to change shared network definitions")
	}
	step := 1
	if attached.Kind == yaml.MappingNode {
		step = 2
	}
	appendNetwork := func(key string) {
		if step == 2 {
			yamlAdd(attached, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
			return
		}
		attached.Content = append(attached.Content, yamlString(key))
	}
	if len(attached.Content) == 0 {
		appendNetwork("default")
	}
	resolved := func(key string) string {
		def := yamlField(definitions, key)
		if name := yamlField(def, "name"); name != nil {
			return name.Value
		}
		if ext := yamlField(def, "external"); ext != nil {
			if name := yamlField(ext, "name"); name != nil {
				return name.Value
			}
			if ext.Value == "true" {
				return key
			}
		}
		return stack + "_" + key
	}
	for _, n := range remove {
		found := false
		for i := 0; i < len(attached.Content); i += step {
			if resolved(attached.Content[i].Value) == n.Name {
				attached.Content = slices.Delete(attached.Content, i, i+step)
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("open the Compose editor to remove %s", n.Name)
		}
	}
	for _, n := range add {
		key := ""
		for i := 0; i < len(definitions.Content); i += 2 {
			candidate := definitions.Content[i].Value
			if resolved(candidate) == n.Name {
				key = candidate
				break
			}
		}
		if key == "" && n.Name == stack+"_default" && n.Stack == stack {
			key = "default"
		}
		if key == "" {
			if n.Stack != "" {
				return "", fmt.Errorf("open the Compose editor to select network %s", n.Name)
			}
			key = n.Name
			for yamlField(definitions, key) != nil || key == "default" {
				key += "_external"
			}
			def := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			yamlAdd(def, "external", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			yamlAdd(def, "name", yamlString(n.Name))
			yamlAdd(definitions, key, def)
		}
		appendNetwork(key)
	}
	if len(attached.Content) == 0 {
		if slices.ContainsFunc(remove, func(n Network) bool { return n.Name == resolved("default") }) {
			return "", fmt.Errorf("connect another network before removing the default Compose network")
		}
		appendNetwork("default")
	}
	data, err := yaml.Marshal(&doc)
	return string(data), err
}
