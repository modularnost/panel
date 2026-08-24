package web

import (
	"net/http"

	"modularnost/internal/docker"
)

type nodesPage struct {
	Nodes []docker.Node
	Join  docker.JoinInfo
	Role  string // which join command is on screen: worker | manager
}

// nodes lists the cluster and hands out the join command.
//
// Join tokens are secrets — anyone holding one can add a machine to the
// cluster. Hence admin only, and never written to the log.
func (s *Server) nodes(w http.ResponseWriter, r *http.Request) {
	role := r.FormValue("role")
	if role != "manager" {
		role = "worker"
	}
	list, err := s.Docker.Nodes(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	join, err := s.Docker.JoinInfo(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, "nodes.html", nodesPage{Nodes: list, Join: join, Role: role})
}

func (s *Server) rotateJoinToken(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if err := s.Docker.RotateJoinToken(r.Context(), role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Redirect", "/nodes?role="+role)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setNodeAvailability(w http.ResponseWriter, r *http.Request) {
	err := s.Docker.SetAvailability(r.Context(), r.PathValue("id"), r.FormValue("availability"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Redirect", "/nodes")
	w.WriteHeader(http.StatusNoContent)
}

// resources shows the whole cluster's CPU and memory. Current numbers only:
// history, charts and alerts stay out of the core (ADR 5).
func (s *Server) resources(w http.ResponseWriter, r *http.Request) {
	s.render(w, "resources.html", nil)
}

// resourcesTable is fetched separately — sampling CPU costs a second.
func (s *Server) resourcesTable(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stats-table", s.statsView(r, "", true))
}

// statsView is what the shared stats table renders. A failing collector becomes
// a message in place of the table, not a failed page.
type statsView struct {
	Stats       []docker.TaskStats
	Err         string
	WithService bool
}

func (s *Server) statsView(r *http.Request, service string, withService bool) statsView {
	v := statsView{WithService: withService}
	stats, err := s.Metrics.Stats(r.Context(), service)
	if err != nil {
		v.Err = err.Error()
	}
	v.Stats = stats
	return v
}
