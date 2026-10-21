package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"modularnost/internal/modules"
	"modularnost/internal/store"
)

type apiDocument struct {
	UI     int              `json:"ui" doc:"The vocabulary version of the document"`
	Blocks []map[string]any `json:"blocks" nullable:"false" doc:"What the page is made of; each block has a type, and one the panel does not know is shown as unsupported"`
}

type pageOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         apiDocument
}

type apiModuleAction struct {
	Page   string            `json:"page" doc:"The contribution's path the button is on"`
	Action string            `json:"action" doc:"A path below /action/ of the module"`
	Values map[string]string `json:"values,omitempty" doc:"The fields of a form, or the fixed values of a button"`
}

type apiActionResult struct {
	Message string `json:"message" doc:"What the module said about it; may be empty"`
}

type actionOutput struct{ Body apiActionResult }

type apiPageChanged struct {
	Path string `json:"path"`
}

// minPageGap keeps a module that reports every second from making every open
// browser refetch every second.
const minPageGap = time.Second

// uiPage finds the ui page a viewer asks for and checks they may see it.
func (s *Server) uiPage(ctx context.Context, name, p string) (modules.Module, store.User, string, error) {
	u, _ := ctx.Value(ctxKey{}).(store.User)
	m, ok := s.Modules.Get(ctx, name)
	if !ok {
		return m, u, "", problem(http.StatusNotFound, "not_found", "no such module")
	}
	if !m.OK() {
		return m, u, "", problem(http.StatusBadGateway, "module_unavailable", "module unavailable: "+m.Err)
	}
	p = path.Clean("/" + p)
	if _, ok := m.UIPage(p); !ok {
		return m, u, "", problem(http.StatusNotFound, "not_found", "the module has no ui page at "+p)
	}
	if need := m.RoleFor(p); !store.AtLeast(u.Role, need) {
		return m, u, "", problem(http.StatusForbidden, "forbidden", "role "+need+" required, you have "+u.Role)
	}
	return m, u, p, nil
}

func (s *Server) registerPages(api huma.API, viewer huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "get-module-page", Method: http.MethodGet, Path: "/api/v1/modules/{name}/page",
		Summary: "The document of a module's ui page", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name  string `path:"name"`
		Path  string `query:"path" required:"true" doc:"The contribution's path"`
		Query string `query:"query" doc:"What the module is told as its query string: the context of a tab, the range of a chart"`
	}) (*pageOutput, error) {
		m, u, p, err := s.uiPage(ctx, in.Name, in.Path)
		if err != nil {
			return nil, err
		}
		doc, err := s.Modules.FetchPage(ctx, m, p, in.Query, modules.Viewer{Username: u.Username, Role: u.Role})
		if err != nil {
			return nil, problem(http.StatusBadGateway, "module_error", err.Error())
		}
		return &pageOutput{CacheControl: "no-store", Body: apiDocument{UI: doc.UI, Blocks: doc.Blocks}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "run-module-action", Method: http.MethodPost, Path: "/api/v1/modules/{name}/action",
		Summary: "Run an action of a module's ui page: what a button or a form sends", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
		Body apiModuleAction
	}) (*actionOutput, error) {
		m, u, _, err := s.uiPage(ctx, in.Name, in.Body.Page)
		if err != nil {
			return nil, err
		}
		action, ok := modules.ActionPath(in.Body.Action)
		if !ok || len(in.Body.Values) > modules.MaxActionValues {
			return nil, problem(http.StatusBadRequest, "bad_request", "action: a path below /action/, with at most 50 values")
		}
		if need := m.RoleFor(action); !store.AtLeast(u.Role, need) {
			return nil, problem(http.StatusForbidden, "forbidden", "role "+need+" required, you have "+u.Role)
		}
		msg, err := s.Modules.Act(ctx, m, action, in.Body.Values, modules.Viewer{Username: u.Username, Role: u.Role})
		if err != nil {
			return nil, problem(http.StatusBadGateway, "module_error", err.Error())
		}
		return &actionOutput{Body: apiActionResult{Message: msg}}, nil
	})

	sse.Register(api, huma.Operation{
		OperationID: "stream-module-page", Method: http.MethodGet, Path: "/api/v1/modules/{name}/events",
		Summary: "Tells when a ui page changed, so the browser refetches it", Middlewares: viewer, Security: signedIn,
	}, map[string]any{
		"changed": apiPageChanged{},
		"problem": apiProblem{},
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
		Path string `query:"path" required:"true" doc:"The contribution's path"`
	}, send sse.Sender) {
		m, _, p, err := s.uiPage(ctx, in.Name, in.Path)
		if err != nil {
			if pr, ok := err.(*apiProblem); ok {
				_ = send.Data(pr)
			}
			return
		}
		changed, stop := s.Pages.Subscribe(m.Name, p)
		defer stop()
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-changed:
			}
			if wait := minPageGap - time.Since(last); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			last = time.Now()
			if send.Data(apiPageChanged{Path: p}) != nil {
				return
			}
		}
	})
}

// uiChanged is how a module says that one of its pages changed. A page it did
// not declare is refused: a module has no business waking another's viewers.
func (s *Server) uiChanged(w http.ResponseWriter, r *http.Request, m modules.Module) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, `send {"path": "/page"}`, http.StatusBadRequest)
		return
	}
	if _, ok := m.UIPage(in.Path); !ok {
		http.Error(w, "not a ui page of "+m.Name, http.StatusNotFound)
		return
	}
	s.Pages.Notify(m.Name, path.Clean("/"+in.Path))
	w.WriteHeader(http.StatusNoContent)
}
