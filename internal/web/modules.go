package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

// moduleAuth admits only a module with a valid token and the required permission.
func (s *Server) moduleAuth(perm string, next func(http.ResponseWriter, *http.Request, modules.Module)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, token := r.Header.Get("X-Panel-Module"), r.Header.Get("X-Panel-Token")
		if name == "" || !s.Modules.ValidToken(name, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		m, ok := s.Modules.Get(r.Context(), name)
		if !ok {
			http.Error(w, "module not found in the cluster", http.StatusUnauthorized)
			return
		}
		if perm != "" && !m.Allowed(perm) {
			// Valid token, but the manifest never declared this permission.
			http.Error(w, "permission "+perm+" not granted", http.StatusForbidden)
			return
		}
		next(w, r, m)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) apiServices(w http.ResponseWriter, r *http.Request, _ modules.Module) {
	svcs, err := s.Docker.Services(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, svcs)
}

func (s *Server) apiStats(w http.ResponseWriter, r *http.Request, _ modules.Module) {
	// An optional ?service= narrows it to one service.
	stats, err := s.Metrics.Stats(r.Context(), r.FormValue("service"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, stats)
}

func (s *Server) apiUpdateService(w http.ResponseWriter, r *http.Request, m modules.Module) {
	sv, ok := s.findService(r.Context(), r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: sv.Name, StackName: sv.Stack,
		Source: "module", TriggeredBy: "module:" + m.Name,
	})
	d, err := s.Docker.ForceUpdate(r.Context(), sv.ID)
	finish(d.OldDigest, d.NewDigest, err)
	logWarnings(d)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, d)
}

type moduleInstall struct {
	Name, Image, Port, Service, Network string
	Env                                 []string
}

var envRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// validate refuses what would end up in a label, a service name or the
// environment of a new service.
func (m moduleInstall) validate() error {
	if !docker.ValidName(m.Name) {
		return errors.New("name: letters, digits, dot, dash and underscore only")
	}
	if strings.TrimSpace(m.Image) == "" {
		return errors.New("image is required")
	}
	if m.Port != "" {
		if n, err := strconv.Atoi(m.Port); err != nil || n < 1 || n > 65535 {
			return errors.New("port: a number from 1 to 65535")
		}
	}
	if m.Service != "" && !docker.ValidName(m.Service) {
		return errors.New("service: letters, digits, dot, dash and underscore only")
	}
	if m.Network != "" && !docker.ValidName(m.Network) {
		return errors.New("network: letters, digits, dot, dash and underscore only")
	}
	for _, e := range m.Env {
		if !envRe.MatchString(e) {
			return errors.New("env: each entry is KEY=value, got " + strconv.Quote(e))
		}
	}
	return nil
}

// corePort is the port of a listen address such as ":9000"; 8080 when it has none.
func corePort(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return cmp.Or(port, "8080")
}

// deployModule creates the service that makes a module appear in the panel: the
// labels, the overlay network and the core's address are filled in here.
func (s *Server) deployModule(ctx context.Context, m moduleInstall) error {
	m.Name, m.Image = strings.TrimSpace(m.Name), strings.TrimSpace(m.Image)
	m.Port, m.Service = strings.TrimSpace(m.Port), strings.TrimSpace(m.Service)
	m.Network = strings.TrimSpace(m.Network)
	if err := m.validate(); err != nil {
		return err
	}

	self, _ := s.Docker.Self(ctx)
	env := slices.Clone(m.Env)
	if self.Service != "" {
		env = append(env, "PANEL_CORE_URL=http://"+net.JoinHostPort(self.Service, corePort(s.Addr)))
	}
	_, err := s.Docker.CreateService(ctx, docker.NewService{
		Name:     cmp.Or(m.Service, m.Name+"_module"),
		Image:    m.Image,
		Replicas: 1,
		Env:      env,
		Networks: []string{cmp.Or(m.Network, self.Network)},
		Labels: map[string]string{
			modules.LabelEnabled: "true",
			modules.LabelName:    m.Name,
			modules.LabelPort:    cmp.Or(m.Port, "8080"),
		},
	})
	if err != nil {
		return err
	}
	// The module is not up yet, but the list must stop showing the old state.
	s.Modules.Forget()
	return nil
}
