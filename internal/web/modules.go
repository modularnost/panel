package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
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

// labelOwner marks a service a module created: only that module may move it to
// another image, and a module cannot set it (or any panel.* label) itself.
const labelOwner = "panel.owner"

type moduleService struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Env      []string          `json:"env"`
	Labels   map[string]string `json:"labels"`
	Replicas uint64            `json:"replicas"`
}

func (m moduleService) validate() error {
	if !docker.ValidName(m.Name) {
		return errors.New("name: letters, digits, dot, dash and underscore only")
	}
	if strings.TrimSpace(m.Image) == "" {
		return errors.New("image is required")
	}
	for k := range m.Labels {
		if strings.HasPrefix(k, "panel.") {
			return errors.New("labels: panel.* is the panel's, got " + strconv.Quote(k))
		}
	}
	for _, e := range m.Env {
		if !envRe.MatchString(e) {
			return errors.New("env: each entry is KEY=value, got " + strconv.Quote(e))
		}
	}
	return nil
}

// apiCreateService runs a service for a module on the panel's network. It
// publishes no port: traffic comes in through the labels, by Traefik.
func (s *Server) apiCreateService(w http.ResponseWriter, r *http.Request, m modules.Module) {
	var in moduleService
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "send {name, image, env, labels, replicas}", http.StatusBadRequest)
		return
	}
	if err := in.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	self, _ := s.Docker.Self(r.Context())
	if self.Network == "" {
		http.Error(w, "the panel does not know its network", http.StatusBadGateway)
		return
	}
	labels := maps.Clone(in.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[labelOwner] = m.Name

	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: in.Name, Source: "module", TriggeredBy: "module:" + m.Name,
	})
	_, err := s.Docker.CreateService(r.Context(), docker.NewService{
		Name: in.Name, Image: strings.TrimSpace(in.Image), Replicas: in.Replicas,
		Env: in.Env, Networks: []string{self.Network}, Labels: labels,
	})
	finish("", "", err)
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"name": in.Name})
}

var tagRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// apiSetServiceImage moves a service the module created to another tag of its
// image, a new service version being a new tag.
func (s *Server) apiSetServiceImage(w http.ResponseWriter, r *http.Request, m modules.Module) {
	var in struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || !tagRe.MatchString(in.Tag) {
		http.Error(w, `send {"tag": "<image tag>"}`, http.StatusBadRequest)
		return
	}
	sv, ok := s.findService(r.Context(), r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if sv.Labels[labelOwner] != m.Name {
		http.Error(w, "the service was not created by "+m.Name, http.StatusForbidden)
		return
	}
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: sv.Name, StackName: sv.Stack,
		Source: "module", TriggeredBy: "module:" + m.Name,
	})
	d, err := s.Docker.RollOut(r.Context(), sv.ID, in.Tag)
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
