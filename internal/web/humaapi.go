package web

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"modularnost/internal/store"
)

type problemBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// apiProblem is the error body of the whole API: {"error": {"code", "message"}}.
type apiProblem struct {
	status int
	Detail problemBody `json:"error"`
}

func (p *apiProblem) Error() string  { return p.Detail.Message }
func (p *apiProblem) GetStatus() int { return p.status }

func problem(status int, code, msg string) error {
	return &apiProblem{status: status, Detail: problemBody{Code: code, Message: msg}}
}

func init() {
	// Errors the framework raises itself (validation, malformed JSON).
	huma.NewError = func(status int, msg string, _ ...error) huma.StatusError {
		code := "bad_request"
		switch status {
		case http.StatusUnauthorized:
			code = "unauthorized"
		case http.StatusForbidden:
			code = "forbidden"
		case http.StatusNotFound:
			code = "not_found"
		case http.StatusUnsupportedMediaType:
			code = "unsupported_media_type"
		case http.StatusUnprocessableEntity:
			code = "invalid_input"
		}
		return &apiProblem{status: status, Detail: problemBody{Code: code, Message: msg}}
	}
}

var signedIn = []map[string][]string{{"cookie": {}}, {"bearer": {}}}

func (s *Server) humaAuth(api huma.API, need string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r, _ := humago.Unwrap(ctx)
		u, status, msg := s.authorize(r, need)
		if status != 0 {
			_ = huma.WriteErr(api, ctx, status, msg)
			return
		}
		next(huma.WithValue(ctx, ctxKey{}, u))
	}
}

type reqKey struct{}

type apiLogin struct {
	Username string `json:"username" minLength:"1"`
	Password string `json:"password" minLength:"1"`
}

type loginInput struct{ Body apiLogin }

type loginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      apiUser
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

type meOutput struct{ Body apiUser }
type serviceListOutput struct {
	Body []apiService `nullable:"false"`
}
type serviceOutput struct{ Body apiService }

func (s *Server) registerAPI(mux *http.ServeMux) huma.API {
	cfg := huma.DefaultConfig("modularnost", "v1")
	cfg.OpenAPIPath = "/api/v1/openapi"
	// The docs page pulls its renderer from a CDN, which an air-gapped panel
	// cannot reach; the spec itself is enough.
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"cookie": {Type: "apiKey", In: "cookie", Name: sessionCookie},
		"bearer": {Type: "http", Scheme: "bearer"},
	}
	// Types are named api*; the spec should not say so.
	cfg.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		return strings.TrimPrefix(huma.DefaultSchemaNamer(t, hint), "Api")
	})
	api := humago.New(mux, cfg)

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		r, _ := humago.Unwrap(ctx)
		// A plain HTML form cannot send application/json, which makes this the
		// CSRF guard for cookie sessions.
		switch ctx.Method() {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if mt, _, _ := mime.ParseMediaType(ctx.Header("Content-Type")); mt != "application/json" {
				_ = huma.WriteErr(api, ctx, http.StatusUnsupportedMediaType, "send application/json")
				return
			}
		}
		next(huma.WithValue(ctx, reqKey{}, r))
	})

	viewer := huma.Middlewares{s.humaAuth(api, store.RoleViewer)}

	huma.Register(api, huma.Operation{
		OperationID: "login", Method: http.MethodPost, Path: "/api/v1/auth/login",
		Summary: "Sign in; the session cookie is set on the response",
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		u, err := s.DB.Authenticate(in.Body.Username, in.Body.Password)
		if errors.Is(err, store.ErrNoUser) {
			return nil, problem(http.StatusUnauthorized, "invalid_credentials", err.Error())
		}
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		r, _ := ctx.Value(reqKey{}).(*http.Request)
		c, err := s.sessionCookie(r, u.ID)
		if err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &loginOutput{SetCookie: *c, Body: toAPIUser(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/api/v1/auth/logout",
		Summary: "End the session", DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		r, _ := ctx.Value(reqKey{}).(*http.Request)
		if err := s.revokeSession(r); err != nil {
			return nil, problem(http.StatusInternalServerError, "internal", err.Error())
		}
		return &logoutOutput{SetCookie: *clearedCookie()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-me", Method: http.MethodGet, Path: "/api/v1/me",
		Summary: "The signed-in user", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		u, _ := ctx.Value(ctxKey{}).(store.User)
		return &meOutput{Body: toAPIUser(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-services", Method: http.MethodGet, Path: "/api/v1/services",
		Summary: "Services with their replica counts", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*serviceListOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		out := make([]apiService, 0, len(svcs))
		for _, sv := range svcs {
			out = append(out, toAPIService(sv, false))
		}
		return &serviceListOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-service", Method: http.MethodGet, Path: "/api/v1/services/{id}",
		Summary: "One service, with its labels", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*serviceOutput, error) {
		svcs, err := s.Docker.Services(ctx)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		for _, sv := range svcs {
			if sv.ID == in.ID || sv.Name == in.ID {
				return &serviceOutput{Body: toAPIService(sv, true)}, nil
			}
		}
		return nil, problem(http.StatusNotFound, "not_found", "no such service")
	})
	s.registerStacks(api, viewer)
	s.registerLogs(api, viewer)
	s.registerCluster(api, viewer)
	s.registerModules(api, viewer)
	s.registerUsers(api)
	s.registerServiceActions(api)
	s.registerStackActions(api)
	s.registerManage(api)
	s.registerUserActions(api)
	s.registerNodeActions(api)
	s.registerUpdates(api)
	return api
}
