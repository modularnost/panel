package web

import (
	"context"
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

func (s *Server) userByID(id int64) (store.User, bool) {
	users, err := s.DB.Users()
	if err != nil {
		return store.User{}, false
	}
	for _, u := range users {
		if u.ID == id {
			return u, true
		}
	}
	return store.User{}, false
}

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
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
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
		u, ok := s.userByID(in.ID)
		if !ok {
			return nil, problem(http.StatusNotFound, "not_found", "no such user")
		}
		if in.Body.Role == "" && in.Body.Password == "" {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "send a role, a password or both")
		}
		if in.Body.Role != "" && in.Body.Role != store.RoleAdmin {
			if err := s.guardLastAdmin(u.ID); err != nil {
				return nil, problem(http.StatusConflict, "last_admin", err.Error())
			}
		}
		if in.Body.Role != "" {
			if err := s.DB.SetRole(u.ID, in.Body.Role); err != nil {
				return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
			}
			u.Role = in.Body.Role
		}
		if in.Body.Password != "" {
			if err := s.changePassword(u.ID, in.Body.Password); err != nil {
				return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
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
		if _, ok := s.userByID(in.ID); !ok {
			return nil, problem(http.StatusNotFound, "not_found", "no such user")
		}
		if err := s.guardLastAdmin(in.ID); err != nil {
			return nil, problem(http.StatusConflict, "last_admin", err.Error())
		}
		if err := s.DB.DeleteUser(in.ID); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-token", Method: http.MethodPost, Path: "/api/v1/tokens",
		Summary: "Create an API token; the value is in this response only", DefaultStatus: http.StatusCreated,
		Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, in *struct{ Body apiTokenCreate }) (*tokenCreatedOutput, error) {
		b := in.Body
		if _, ok := s.userByID(b.UserID); !ok {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", "no such user")
		}
		ttl := time.Duration(b.Days) * 24 * time.Hour
		token, err := s.DB.CreateToken(b.UserID, b.Name, b.Role, ttl)
		if err != nil {
			return nil, problem(http.StatusUnprocessableEntity, "invalid_input", err.Error())
		}
		out := apiTokenCreated{Token: token, UserID: b.UserID, Name: b.Name, Role: b.Role}
		if ttl > 0 {
			exp := time.Now().Add(ttl)
			out.ExpiresAt = &exp
		}
		if tokens, err := s.DB.Tokens(); err == nil {
			for _, t := range tokens {
				if t.UserID == b.UserID && t.Name == b.Name {
					out.ID = t.ID
				}
			}
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
		tokens, err := s.DB.Tokens()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		found := false
		for _, t := range tokens {
			found = found || t.ID == in.ID
		}
		if !found {
			return nil, problem(http.StatusNotFound, "not_found", "no such token")
		}
		if err := s.DB.DeleteToken(in.ID); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, nil
	})
}
