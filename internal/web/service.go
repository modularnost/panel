package web

import (
	"net/http"
	"strconv"

	"modularnost/internal/docker"
	"modularnost/internal/store"
)

// servicePage backs every tab of a single service's page. One struct and one
// template: the tabs differ by a few blocks, not by a whole page.
type servicePage struct {
	Me      store.User
	Service docker.Service
	Tab     string // overview | logs | traefik | webhooks | deploys
	Tasks   []docker.Task
	Traefik traefikForm
	Hooks   []store.Webhook
	Deploys []store.DeployEvent
	NewHook string // shown once, right after a hook is created
}

// service loads the service behind the URL and the data its tab needs.
func (s *Server) service(w http.ResponseWriter, r *http.Request, tab string) (servicePage, bool) {
	sv, ok := s.findService(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return servicePage{}, false
	}
	p := servicePage{Me: userFrom(r), Service: sv, Tab: tab}

	var err error
	switch tab {
	case "logs":
		p.Tasks, err = s.Docker.Tasks(r.Context(), sv.ID)
	case "traefik":
		p.Traefik = traefikFrom(sv)
	case "webhooks":
		p.Hooks, err = s.DB.WebhooksFor(sv.Name, sv.Stack)
	case "deploys":
		p.Deploys, err = s.DB.DeploysFor(sv.Name, sv.Stack, 50)
	}
	if err != nil {
		fail(w, err)
		return servicePage{}, false
	}
	return p, true
}

func (s *Server) serviceTab(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.service(w, r, tab)
		if !ok {
			return
		}
		s.render(w, "service.html", p)
	}
}

// serviceStats is loaded separately: sampling CPU takes a second (two readings
// are needed for a delta), and the page should not wait for it.
func (s *Server) serviceStats(w http.ResponseWriter, r *http.Request) {
	p, ok := s.service(w, r, "stats")
	if !ok {
		return
	}
	s.render(w, "stats-table", s.statsView(r, p.Service.Name, false))
}

// --- webhooks scoped to one service ---

func (s *Server) serviceCreateHook(w http.ResponseWriter, r *http.Request) {
	p, ok := s.service(w, r, "webhooks")
	if !ok {
		return
	}
	hook := store.Webhook{ActionType: r.FormValue("action_type")}
	switch hook.ActionType {
	case "force_update":
		hook.ServiceName = p.Service.Name
	case "stack_redeploy":
		if p.Service.Stack == "" {
			http.Error(w, "service is not part of a stack", http.StatusBadRequest)
			return
		}
		hook.StackName = p.Service.Stack
	default:
		http.Error(w, "bad action_type", http.StatusBadRequest)
		return
	}
	created, err := s.DB.CreateWebhook(hook)
	if err != nil {
		fail(w, err)
		return
	}
	p.Hooks, err = s.DB.WebhooksFor(p.Service.Name, p.Service.Stack)
	if err != nil {
		fail(w, err)
		return
	}
	p.NewHook = created.SecretToken
	s.render(w, "svc-hooks", p)
}

func (s *Server) serviceDeleteHook(w http.ResponseWriter, r *http.Request) {
	p, ok := s.service(w, r, "webhooks")
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("hook"), 10, 64)
	if err := s.DB.DeleteWebhook(id); err != nil {
		fail(w, err)
		return
	}
	var err error
	if p.Hooks, err = s.DB.WebhooksFor(p.Service.Name, p.Service.Stack); err != nil {
		fail(w, err)
		return
	}
	s.render(w, "svc-hooks", p)
}
