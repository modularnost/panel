package web

import (
	"net/http"
	"strconv"
	"strings"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

// lines splits a textarea into trimmed, non-empty lines.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// --- single service ---

func (s *Server) newServiceForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "new-service.html", nil)
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	replicas, _ := strconv.ParseUint(r.FormValue("replicas"), 10, 32)
	n := docker.NewService{
		Name:     strings.TrimSpace(r.FormValue("name")),
		Image:    strings.TrimSpace(r.FormValue("image")),
		Replicas: replicas,
		Env:      lines(r.FormValue("env")),
		Ports:    lines(r.FormValue("ports")),
		Networks: lines(r.FormValue("networks")),
	}
	me := userFrom(r)
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: n.Name, Source: "ui", TriggeredBy: me.Username})
	id, err := s.Docker.CreateService(r.Context(), n)
	finish("", "", err)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Redirect", "/services/"+id)
	w.WriteHeader(http.StatusNoContent)
}

// --- stacks from compose ---

type stackForm struct {
	Name    string
	Compose string
	IsNew   bool
	Output  string
}

const sampleCompose = `services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 2
    networks: [appnet]

networks:
  appnet:
    driver: overlay
`

func (s *Server) newStackForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stack.html", stackForm{Compose: sampleCompose, IsNew: true})
}

func (s *Server) editStackForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, ok := s.DB.Stack(name)
	if !ok {
		// The stack exists in Swarm but was not deployed from here, so there is
		// no compose file to show — offer to adopt it by pasting one.
		s.render(w, "stack.html", stackForm{Name: name, Compose: sampleCompose})
		return
	}
	s.render(w, "stack.html", stackForm{Name: st.Name, Compose: st.Compose})
}

func (s *Server) deployStack(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = r.PathValue("name")
	}
	compose := r.FormValue("compose")
	me := userFrom(r)

	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		StackName: name, Source: "ui", TriggeredBy: me.Username})
	out, err := s.Docker.DeployStack(r.Context(), name, compose)
	finish("", "", err)
	if err != nil {
		s.render(w, "stack-result", stackForm{Name: name, Compose: compose, Output: err.Error()})
		return
	}
	// Only store a file that actually deployed.
	if err := s.DB.SaveStack(name, compose, me.Username); err != nil {
		fail(w, err)
		return
	}
	s.render(w, "stack-result", stackForm{Name: name, Compose: compose, Output: out})
}

// removeStack tears the stack down and forgets its compose file.
func (s *Server) removeStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.Docker.RemoveStack(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.DB.DeleteStack(name); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeService(w http.ResponseWriter, r *http.Request) {
	sv, ok := s.findService(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.Docker.RemoveService(r.Context(), sv.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusNoContent)
}
