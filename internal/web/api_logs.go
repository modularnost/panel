package web

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"modularnost/internal/docker"
)

type apiTask struct {
	ID    string `json:"id"`
	Slot  int    `json:"slot" doc:"Replica number; 0 for a global service"`
	State string `json:"state"`
}

type apiLogLine struct {
	Text string `json:"text" doc:"Timestamp, [replica] and the line, as docker prints it"`
}

func toAPITasks(in []docker.Task) []apiTask {
	out := make([]apiTask, 0, len(in))
	for _, t := range in {
		out = append(out, apiTask{ID: t.ID, Slot: t.Slot, State: t.State})
	}
	return out
}

type taskListOutput struct {
	Body []apiTask `nullable:"false"`
}

func (s *Server) registerLogs(api huma.API, viewer huma.Middlewares) {
	huma.Register(api, huma.Operation{
		OperationID: "list-service-tasks", Method: http.MethodGet, Path: "/api/v1/services/{id}/tasks",
		Summary: "The service's live replicas, for filtering its logs", Middlewares: viewer, Security: signedIn,
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*taskListOutput, error) {
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		tasks, err := s.Docker.Tasks(ctx, sv.ID)
		if err != nil {
			return nil, problem(http.StatusBadGateway, "docker_unavailable", err.Error())
		}
		return &taskListOutput{Body: toAPITasks(tasks)}, nil
	})

	// A stream cannot change its status once it has started, so a failure
	// before the first line arrives as a "problem" event, then the stream ends.
	sse.Register(api, huma.Operation{
		OperationID: "stream-service-logs", Method: http.MethodGet, Path: "/api/v1/services/{id}/logs",
		Summary: "Follow a service's logs, or one replica's", Middlewares: viewer, Security: signedIn,
	}, map[string]any{
		"line":    apiLogLine{},
		"problem": apiProblem{},
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Task string `query:"task" doc:"A replica id from the tasks list; empty for all replicas"`
		Tail int    `query:"tail" default:"200" minimum:"1" maximum:"5000" doc:"Lines of history first"`
	}, send sse.Sender) {
		fail := func(code, msg string) {
			_ = send.Data(apiProblem{Detail: problemBody{Code: code, Message: msg}})
		}
		sv, err := s.apiFindService(ctx, in.ID)
		if err != nil {
			p, _ := err.(*apiProblem)
			if p != nil {
				fail(p.Detail.Code, p.Detail.Message)
			}
			return
		}
		lines, err := s.Docker.Logs(ctx, sv.ID, in.Task, in.Tail)
		if err != nil {
			fail("docker_error", err.Error())
			return
		}
		for line := range lines {
			if send.Data(apiLogLine{Text: line}) != nil {
				return
			}
		}
	})
}
