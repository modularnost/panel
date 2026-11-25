package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/containerd/errdefs"

	"modularnost/internal/docker"
)

type catalogSecretInput struct {
	Existing string       `json:"existing,omitempty" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$"`
	Create   *secretValue `json:"create,omitempty"`
}

func (s *Server) bindCatalogSecrets(ctx context.Context, compose string, input map[string]catalogSecretInput) (string, []string, error) {
	names := make(map[string]string, len(input))
	for alias, selected := range input {
		if (selected.Existing == "") == (selected.Create == nil) {
			return "", nil, problem(http.StatusUnprocessableEntity, "invalid_input", "Select an existing secret or create one for each alias")
		}
		if selected.Create != nil {
			names[alias] = selected.Create.Name
		} else {
			secret, err := s.Docker.SecretName(ctx, selected.Existing)
			if err != nil {
				return "", nil, resourceProblem(err)
			}
			names[alias] = secret
		}
	}
	rendered, err := docker.BindCatalogSecrets(compose, names)
	if err != nil {
		return "", nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
	}

	var created []string
	aliases := make([]string, 0, len(input))
	for alias := range input {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if value := input[alias].Create; value != nil {
			if err := ctx.Err(); err != nil {
				return "", created, resourceProblem(err)
			}
			createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			id, err := s.Docker.CreateSecret(createCtx, value.Name, value.Data)
			cancel()
			if err != nil {
				return "", created, resourceProblem(err)
			}
			created = append(created, id)
		}
	}
	return rendered, created, nil
}

func (s *Server) removeCatalogSecrets(ctx context.Context, stack string, created []string) {
	for _, id := range created {
		if err := s.Docker.RemoveSecret(ctx, id); err != nil && !errors.Is(err, errdefs.ErrNotFound) {
			log.Printf("rolling back module stack %s: could not remove secret ID %s", stack, id)
		}
	}
}
