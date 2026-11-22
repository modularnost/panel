package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

const registryLabel = "panel.registry"

type apiRegistry struct {
	Host     string `json:"host"`
	Username string `json:"username"`
}

type registryListOutput struct {
	Body struct {
		Configured bool          `json:"configured"`
		Registries []apiRegistry `json:"registries" nullable:"false"`
	}
}

type registryInput struct {
	Host string `path:"host" maxLength:"255"`
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"255"`
		Token    string `json:"token" minLength:"1" maxLength:"4096" writeOnly:"true"`
	}
}

func validRegistryHost(host string) bool {
	return host == strings.ToLower(host) && !strings.ContainsAny(host, "/@") &&
		docker.RegistryHost(host+"/probe") == host && host != ""
}

func (s *Server) registerRegistries(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}
	path := "/api/v1/registries"
	huma.Register(api, huma.Operation{
		OperationID: "list-registries", Method: http.MethodGet, Path: path,
		Summary: "List saved private registries without their tokens", Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, _ *struct{}) (*registryListOutput, error) {
		rows, err := s.DB.Registries()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", "could not list registries")
		}
		out := &registryListOutput{}
		out.Body.Configured = s.RegistryEncryption != nil
		out.Body.Registries = make([]apiRegistry, 0, len(rows))
		for _, row := range rows {
			out.Body.Registries = append(out.Body.Registries, apiRegistry(row))
		}
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "save-registry", Method: http.MethodPut, Path: path + "/{host}",
		Summary: "Save or rotate a registry pull token", DefaultStatus: http.StatusNoContent,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *registryInput) (*struct{}, error) {
		if !validRegistryHost(in.Host) {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "registry host must be a Docker registry hostname with an optional port")
		}
		username := strings.TrimSpace(in.Body.Username)
		if username == "" || strings.ContainsAny(username, ":\x00\r\n") || strings.TrimSpace(in.Body.Token) == "" ||
			strings.ContainsAny(in.Body.Token, "\x00\r\n") {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "enter a valid username and pull token")
		}
		if s.RegistryEncryption == nil {
			return nil, problem(http.StatusServiceUnavailable, "registry_not_configured", "registry encryption is not configured")
		}
		if err := s.DB.SaveRegistry(store.Registry{Host: in.Host, Username: username}, in.Body.Token, s.RegistryEncryption); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", "could not save registry")
		}
		return nil, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "delete-registry", Method: http.MethodDelete, Path: path + "/{host}",
		Summary: "Remove saved registry credentials from the panel", DefaultStatus: http.StatusNoContent,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct {
		Host string `path:"host"`
	}) (*struct{}, error) {
		if err := s.DB.DeleteRegistry(in.Host); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", "could not remove registry")
		}
		return nil, nil
	})
}

func (s *Server) registryCredentials(host string) (*registry.AuthConfig, error) {
	if host == "" {
		return nil, nil
	}
	if s.RegistryEncryption == nil {
		return nil, problem(http.StatusServiceUnavailable, "registry_not_configured", "registry encryption is not configured")
	}
	username, token, err := s.DB.RegistryCredentials(host, s.RegistryEncryption)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, problem(http.StatusUnprocessableEntity, "registry_not_found", "save credentials for the selected registry first")
	}
	if err != nil {
		return nil, problem(http.StatusInternalServerError, "registry_unavailable", "could not decrypt or load registry credentials")
	}
	return &registry.AuthConfig{Username: username, Password: token, ServerAddress: docker.RegistryAddress(host)}, nil
}

func (s *Server) serviceRegistryAuth(image string, labels map[string]string) (string, error) {
	if labels[labelOwner] != "" {
		return "", nil
	}
	host := labels[registryLabel]
	if stack := labels["com.docker.stack.namespace"]; stack != "" {
		stored, _ := s.DB.Stack(stack)
		host = stored.RegistryHost
	}
	return s.encodeRegistryAuth(host, image)
}

func (s *Server) encodeRegistryAuth(host, image string) (string, error) {
	if host == "" || host != docker.RegistryHost(image) {
		return "", nil
	}
	auth, err := s.registryCredentials(host)
	if err != nil {
		return "", err
	}
	return authconfig.Encode(*auth)
}
