package docker

import (
	"maps"
	"strings"
	"testing"
)

func TestParseStackEnvironment(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"literal values", "TAG=1.28\r\n\r\nEMPTY=\nTEXT= a=b $C ", map[string]string{"TAG": "1.28", "EMPTY": "", "TEXT": " a=b $C "}},
		{"invalid name", "BAD-NAME=value", nil},
		{"missing equals", "TAG", nil},
		{"duplicate name", "TAG=one\nTAG=two", nil},
		{"null byte", "TAG=a\x00b", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStackEnvironment(tt.input)
			if (err == nil) != (tt.want != nil) || !maps.Equal(got, tt.want) {
				t.Fatalf("ParseStackEnvironment() = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestStackComposeEnvironment(t *testing.T) {
	const compose = `services:
  app:
    image: nginx:${TAG:-latest}
    environment:
      DOCKER_HOST: $DOCKER_HOST
      ${TAG}: unchanged-key
      ESCAPED: $$TAG
      FALLBACK: ${OTHER:-fallback}
`
	values := map[string]string{"TAG": "1.28", "DOCKER_HOST": "literal=$X", "UNUSED": "value"}
	got, env, err := stackComposeEnvironment(compose, values)
	if err != nil || len(env) != 2 {
		t.Fatalf("stackComposeEnvironment() env = %v, err = %v", env, err)
	}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		parts := strings.SplitN(key, "_", 4)
		if len(parts) != 4 || !strings.HasPrefix(key, "PANEL_STACK_") {
			t.Fatalf("variable %q was passed to Docker directly", key)
		}
		name := parts[3]
		if expected, ok := values[name]; !ok || expected != value {
			t.Fatalf("variable %q has unexpected value %q", name, value)
		}
		got = strings.ReplaceAll(got, key, name)
	}
	if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(compose), " ") {
		t.Fatalf("rewriting changed Compose semantics:\n%s", got)
	}
	if got, env, err := stackComposeEnvironment(compose, nil); got != compose || env != nil || err != nil {
		t.Fatal("deployment without variables must pass through the original file")
	}
}
