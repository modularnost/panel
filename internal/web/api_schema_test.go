package web

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// fill gives every settable field a non-zero value, so omitempty hides nothing.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0))
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k)
		fill(e)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Unix(1, 0)))
			return
		}
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				fill(f)
			}
		}
	}
}

// What a client reads in api/openapi.json has to be what the handlers send. A
// struct embedded by an unexported type once dropped out of the schema while
// still being in the JSON; this compares the two for every schema in the spec.
func TestSchemasMatchResponseTypes(t *testing.T) {
	types := map[string]any{
		"User":            apiUser{},
		"Service":         apiService{},
		"Stack":           apiStack{},
		"StackDetail":     apiStackDetail{},
		"Deploy":          apiDeploy{},
		"TaskStats":       apiTaskStats{},
		"Node":            apiNode{},
		"Join":            apiJoin{},
		"Module":          apiModule{},
		"NavEntry":        apiNavEntry{},
		"Webhook":         apiWebhook{},
		"Token":           apiToken{},
		"Redeploy":        apiRedeploy{},
		"Traefik":         apiTraefik{},
		"Task":            apiTask{},
		"LogLine":         apiLogLine{},
		"StackResult":     apiStackResult{},
		"StackCompose":    apiStackCompose{},
		"WebhookCreate":   apiWebhookCreate{},
		"ModuleInstall":   apiModuleInstall{},
		"ModuleInstalled": apiModuleInstalled{},
		"UserCreate":      apiUserCreate{},
		"UserPatch":       apiUserPatch{},
		"TokenCreate":     apiTokenCreate{},
		"TokenCreated":    apiTokenCreated{},
		"Availability":    apiAvailability{},
		"JoinRotate":      apiJoinRotate{},
		"Release":         apiRelease{},
		"Updates":         apiUpdates{},
		"UpdateApply":     apiUpdateApply{},
		"UpdateStarted":   apiUpdateStarted{},
		"Login":           apiLogin{},
		"Problem":         apiProblem{},
		"ProblemBody":     problemBody{},
	}

	rec := httptest.NewRecorder()
	(&Server{}).Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}

	for name, schema := range spec.Components.Schemas {
		zero, ok := types[name]
		if !ok {
			t.Errorf("schema %s has no Go type in this test: add it", name)
			continue
		}
		v := reflect.New(reflect.TypeOf(zero))
		fill(v.Elem())
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			t.Fatal(err)
		}
		var sent map[string]any
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatal(err)
		}
		var sentKeys, specKeys []string
		for k := range sent {
			sentKeys = append(sentKeys, k)
		}
		for k := range schema.Properties {
			specKeys = append(specKeys, k)
		}
		slices.Sort(sentKeys)
		slices.Sort(specKeys)
		if !slices.Equal(sentKeys, specKeys) {
			t.Errorf("%s: handlers send %v, the spec describes %v", name, sentKeys, specKeys)
		}
	}
}

// One naming convention for everything a client sees: snake_case, like the
// module API and the database.
func TestSpecNamesAreSnakeCase(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
			} `json:"parameters"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	snake := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	for name, schema := range spec.Components.Schemas {
		for prop := range schema.Properties {
			if !snake.MatchString(prop) {
				t.Errorf("%s.%s is not snake_case", name, prop)
			}
		}
	}
	for path, ops := range spec.Paths {
		for _, op := range ops {
			for _, p := range op.Parameters {
				if !snake.MatchString(p.Name) {
					t.Errorf("%s parameter %s is not snake_case", path, p.Name)
				}
			}
		}
	}
}

// Handlers always send [] for an empty list, so the spec must not promise null:
// the generated client types would force every caller to guard against it.
func TestSpecHasNoNullableTypes(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	if strings.Contains(rec.Body.String(), `"null"`) {
		t.Error(`the spec has a "null" type: add nullable:"false" to the slice field`)
	}
}
