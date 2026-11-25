package docker

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

var stackVariableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func ParseStackEnvironment(input string) (map[string]string, error) {
	values := make(map[string]string)
	for i, line := range strings.Split(input, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || !stackVariableName.MatchString(name) || strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("environment variables: line %d must be NAME=value", i+1)
		}
		if _, exists := values[name]; exists {
			return nil, fmt.Errorf("environment variables: duplicate name on line %d", i+1)
		}
		values[name] = value
	}
	return values, nil
}

var stackVariableReference = regexp.MustCompile(`\$\$|\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::?[-?+][^}]*)?\}|\$([a-zA-Z_][a-zA-Z0-9_]*)`)

func stackComposeEnvironment(compose string, values map[string]string) (string, []string, error) {
	if len(values) == 0 {
		return compose, nil, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(compose), &document); err != nil {
		return "", nil, errors.New("invalid Compose YAML")
	}

	// Give Docker private variable names so stack values cannot change CLI settings
	// such as DOCKER_HOST. Docker still evaluates the Compose expressions.
	prefix := "PANEL_STACK_" + rand.Text() + "_"
	aliases := make(map[string]string)
	rewriteComposeValues(&document, func(value string) string {
		return stackVariableReference.ReplaceAllStringFunc(value, func(reference string) string {
			groups := stackVariableReference.FindStringSubmatch(reference)
			name := groups[1] + groups[2]
			if variable, ok := values[name]; ok {
				aliases[prefix+name] = variable
				return strings.Replace(reference, name, prefix+name, 1)
			}
			return reference
		})
	})
	if len(aliases) == 0 {
		return compose, nil, nil
	}
	data, err := yaml.Marshal(&document)
	if err != nil {
		return "", nil, errors.New("could not encode Compose YAML")
	}
	env := make([]string, 0, len(aliases))
	for key, value := range aliases {
		env = append(env, key+"="+value)
	}
	return string(data), env, nil
}

func rewriteComposeValues(node *yaml.Node, rewrite func(string) string) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			rewriteComposeValues(child, rewrite)
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			rewriteComposeValues(node.Content[i], rewrite)
		}
	case yaml.ScalarNode:
		if node.ShortTag() == "!!str" {
			node.Value = rewrite(node.Value)
		}
	}
}
