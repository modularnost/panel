package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/store"
)

type apiUserCreate struct {
	Username string `json:"username" minLength:"1" maxLength:"64"`
	Password string `json:"password" minLength:"8" maxLength:"256"`
	Role     string `json:"role" enum:"admin,operator,viewer"`
}

type apiUserPatch struct {
	Role     string `json:"role,omitempty" enum:"admin,operator,viewer"`
	Password string `json:"password,omitempty" minLength:"8" maxLength:"256" doc:"Also ends the user's browser sessions"`
}

type apiTokenCreate struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name" minLength:"1" maxLength:"64"`
	Role   string `json:"role,omitempty" enum:"admin,operator,viewer" doc:"Narrows the user's role; empty inherits it"`
	Days   int    `json:"days,omitempty" minimum:"0" maximum:"3650" doc:"Validity in days; 0 never expires"`
}

type apiTokenCreated struct {
	Token     string     `json:"token" doc:"Shown once: only a hash is stored"`
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Name      string     `json:"name"`
	Role      string     `json:"role,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type userOutput struct{ Body apiUser }
type tokenCreatedOutput struct{ Body apiTokenCreated }

func (s *Server) registerUserActions(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "create-user", Method: http.MethodPost, Path: "/api/v1/users",
		Summary: "Create a user", DefaultStatus: http.StatusCreated, Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct{ Body apiUserCreate }) (*userOutput, error) {
		u, err := s.DB.CreateUser(in.Body.Username, in.Body.Password, in.Body.Role)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return nil, problem(http.StatusConflict, "username_taken", "that username is taken")
			}
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		return &userOutput{Body: toAPIUser(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-user", Method: http.MethodPatch, Path: "/api/v1/users/{id}",
		Summary: "Change a user's role or password", Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct {
		ID   int64 `path:"id"`
		Body apiUserPatch
	}) (*userOutput, error) {
		u, ok := s.DB.UserByID(in.ID)
		if !ok {
			return nil, problem(http.StatusNotFound, "not_found", "no such user")
		}
		if in.Body.Role == "" && in.Body.Password == "" {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "send a role, a password or both")
		}
		if in.Body.Role != "" {
			if err := s.DB.SetRole(u.ID, in.Body.Role); err != nil {
				if errors.Is(err, store.ErrLastAdmin) {
					return nil, problem(http.StatusConflict, "last_admin", err.Error())
				}
				return nil, backendProblem(http.StatusInternalServerError, "internal", err)
			}
			u.Role = in.Body.Role
		}
		if in.Body.Password != "" {
			if err := s.changePassword(u.ID, in.Body.Password); err != nil {
				return nil, backendProblem(http.StatusInternalServerError, "internal", err)
			}
		}
		return &userOutput{Body: toAPIUser(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-user", Method: http.MethodDelete, Path: "/api/v1/users/{id}",
		Summary: "Delete a user and their tokens", DefaultStatus: http.StatusNoContent,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct {
		ID int64 `path:"id"`
	}) (*struct{}, error) {
		if _, ok := s.DB.UserByID(in.ID); !ok {
			return nil, problem(http.StatusNotFound, "not_found", "no such user")
		}
		if err := s.DB.DeleteUser(in.ID); err != nil {
			if errors.Is(err, store.ErrLastAdmin) {
				return nil, problem(http.StatusConflict, "last_admin", err.Error())
			}
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-token", Method: http.MethodPost, Path: "/api/v1/tokens",
		Summary: "Create an API token; the value is in this response only", DefaultStatus: http.StatusCreated,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct{ Body apiTokenCreate }) (*tokenCreatedOutput, error) {
		b := in.Body
		user, ok := s.DB.UserByID(b.UserID)
		if !ok {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "no such user")
		}
		if b.Role != "" && !store.AtLeast(user.Role, b.Role) {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "token role cannot exceed the user's role")
		}
		ttl := time.Duration(b.Days) * 24 * time.Hour
		token, id, err := s.DB.CreateToken(b.UserID, b.Name, b.Role, ttl)
		if err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		out := apiTokenCreated{Token: token, ID: id, UserID: b.UserID, Name: b.Name, Role: b.Role}
		if ttl > 0 {
			exp := time.Now().Add(ttl)
			out.ExpiresAt = &exp
		}
		return &tokenCreatedOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-token", Method: http.MethodDelete, Path: "/api/v1/tokens/{id}",
		Summary: "Revoke an API token", DefaultStatus: http.StatusNoContent,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct {
		ID int64 `path:"id"`
	}) (*struct{}, error) {
		found, err := s.DB.DeleteNamedToken(in.ID)
		if err != nil {
			return nil, backendProblem(http.StatusInternalServerError, "internal", err)
		}
		if !found {
			return nil, problem(http.StatusNotFound, "not_found", "no such token")
		}
		return nil, nil
	})
}
