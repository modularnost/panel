package web

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/store"
)

// apiToken is a token's metadata. The token itself is shown once, when it is
// created, and only a hash is stored: there is nothing else to list.
type apiToken struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Username  string     `json:"username"`
	Name      string     `json:"name"`
	Role      string     `json:"role,omitempty" doc:"The token's own role; empty means it inherits the user's"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func toAPITokens(in []store.Token) []apiToken {
	out := make([]apiToken, 0, len(in))
	for _, t := range in {
		tok := apiToken{ID: t.ID, UserID: t.UserID, Username: t.Username, Name: t.Name, Role: t.Role}
		if t.ExpiresAt.Valid {
			tok.ExpiresAt = &t.ExpiresAt.Time
		}
		out = append(out, tok)
	}
	return out
}

type userListOutput struct {
	Body []apiUser `nullable:"false"`
}
type tokenListOutput struct {
	Body []apiToken `nullable:"false"`
}

func (s *Server) registerUsers(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "list-users", Method: http.MethodGet, Path: "/api/v1/users",
		Summary: "All users", Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, _ *struct{}) (*userListOutput, error) {
		users, err := s.DB.Users()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		out := make([]apiUser, 0, len(users))
		for _, u := range users {
			out = append(out, toAPIUser(u))
		}
		return &userListOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-tokens", Method: http.MethodGet, Path: "/api/v1/tokens",
		Summary: "API tokens, without their values", Middlewares: admin, Security: signedIn,
	}, func(_ context.Context, _ *struct{}) (*tokenListOutput, error) {
		tokens, err := s.DB.Tokens()
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &tokenListOutput{Body: toAPITokens(tokens)}, nil
	})
}
