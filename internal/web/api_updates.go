package web

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/store"
	"modularnost/internal/updates"
)

type apiRelease struct {
	Tag         string    `json:"tag"`
	Body        string    `json:"body" doc:"The release notes as written on GitHub: Markdown source, to be shown as text"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
}

type apiUpdates struct {
	Current      string       `json:"current"`
	ReleaseBuild bool         `json:"release_build" doc:"False for edge and dev builds, which never list releases"`
	Enabled      bool         `json:"enabled" doc:"False when PANEL_UPDATE_CHECK=off"`
	CheckedAt    *time.Time   `json:"checked_at,omitempty" doc:"The last successful check; absent before the first one"`
	Error        string       `json:"error,omitempty" doc:"Why the last check failed; releases are from the check before it"`
	Releases     []apiRelease `json:"releases" doc:"Releases newer than the running version, newest first" nullable:"false"`
}

type updatesOutput struct{ Body apiUpdates }

func toAPIUpdates(st updates.Status) apiUpdates {
	out := apiUpdates{
		Current: st.Current, ReleaseBuild: st.ReleaseBuild, Enabled: st.Enabled,
		Error: st.Error, Releases: make([]apiRelease, 0, len(st.Newer)),
	}
	if !st.CheckedAt.IsZero() {
		out.CheckedAt = &st.CheckedAt
	}
	for _, r := range st.Newer {
		out.Releases = append(out.Releases, apiRelease{r.Tag, r.Body, r.Prerelease, r.PublishedAt, r.URL})
	}
	return out
}

func (s *Server) updateStatus() updates.Status {
	if s.Updates == nil {
		return updates.Status{Current: Version}
	}
	return s.Updates.Status()
}

func (s *Server) registerUpdates(api huma.API) {
	admin := huma.Middlewares{s.humaAuth(api, store.RoleAdmin)}

	huma.Register(api, huma.Operation{
		OperationID: "get-updates", Method: http.MethodGet, Path: "/api/v1/updates",
		Summary: "Releases newer than the running version, with their notes", Middlewares: admin, Security: signedIn,
	}, func(context.Context, *struct{}) (*updatesOutput, error) {
		return &updatesOutput{Body: toAPIUpdates(s.updateStatus())}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "check-updates", Method: http.MethodPost, Path: "/api/v1/updates/check",
		Summary: "Ask GitHub for releases now", Middlewares: admin, Security: signedIn,
	}, func(ctx context.Context, _ *struct{}) (*updatesOutput, error) {
		if s.Updates != nil {
			if err := s.Updates.Refresh(ctx); err != nil {
				return nil, problem(http.StatusBadGateway, "updates_unavailable", err.Error())
			}
		}
		return &updatesOutput{Body: toAPIUpdates(s.updateStatus())}, nil
	})
}
