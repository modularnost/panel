package web

import (
	"cmp"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

func (s *Server) modulesPage(w http.ResponseWriter, r *http.Request) {
	mods, err := s.Modules.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, "modules.html", struct {
		Me      store.User
		Modules []modules.Module
	}{userFrom(r), mods})
}

// nav is the menu fragment, fetched separately so handlers need not pass it.
func (s *Server) nav(w http.ResponseWriter, r *http.Request) {
	mods, _ := s.Modules.List(r.Context())
	s.render(w, "nav", struct {
		Me      store.User
		Modules []modules.Module
	}{userFrom(r), mods})
}

// moduleProxy forwards the request as is: the module renders its own page.
func (s *Server) moduleProxy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, ok := s.Modules.Get(r.Context(), name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !m.OK() {
		http.Error(w, "module unavailable: "+m.Err, http.StatusBadGateway)
		return
	}
	target, err := url.Parse(m.BaseURL)
	if err != nil {
		fail(w, err)
		return
	}
	// A top-level navigation gets the shell with the module in a frame; the
	// frame's own requests, and browsers without Sec-Fetch-Dest, get the bare page.
	if r.Header.Get("Sec-Fetch-Dest") == "document" {
		s.render(w, "module.html", struct {
			Module modules.Module
			Src    string
		}{m, r.URL.RequestURI()})
		return
	}

	prefix := "/modules/" + name
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, prefix), "/")
			// How the module calls the core back: its name plus an HMAC of it.
			pr.Out.Header.Set("X-Panel-Module", name)
			pr.Out.Header.Set("X-Panel-Token", s.Modules.Token(name))
			pr.Out.Header.Del("Authorization") // the module has no business with the panel password
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("module %s: %v", name, err)
			http.Error(w, "module not responding", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

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
		if !m.Allowed(perm) {
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

type installPage struct {
	Networks []string
	Self     docker.Self
	Error    string
}

func (s *Server) installForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "install.html", s.installPage(r, ""))
}

func (s *Server) installPage(r *http.Request, errMsg string) installPage {
	p := installPage{Error: errMsg}
	p.Self, _ = s.Docker.Self(r.Context())
	p.Networks, _ = s.Docker.OverlayNetworks(r.Context())
	return p
}

// installModule deploys a module image; the panel fills in the labels, the
// network and the callback address.
func (s *Server) installModule(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	image := strings.TrimSpace(r.FormValue("image"))
	port := cmp.Or(strings.TrimSpace(r.FormValue("port")), "8080")
	network := r.FormValue("network")

	self, _ := s.Docker.Self(r.Context())
	if network == "" {
		network = self.Network
	}
	env := lines(r.FormValue("env"))
	if self.Service != "" {
		env = append(env, "PANEL_CORE_URL=http://"+self.Service+":8080")
	}

	_, err := s.Docker.CreateService(r.Context(), docker.NewService{
		Name:     cmp.Or(strings.TrimSpace(r.FormValue("service")), name+"_module"),
		Image:    image,
		Replicas: 1,
		Env:      env,
		Networks: []string{network},
		Labels: map[string]string{
			modules.LabelEnabled: "true",
			modules.LabelName:    name,
			modules.LabelPort:    port,
		},
	})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, "install.html", s.installPage(r, err.Error()))
		return
	}
	// The module is not up yet, but the list must stop showing the old state.
	s.Modules.Forget()
	w.Header().Set("HX-Redirect", "/modules")
	w.WriteHeader(http.StatusNoContent)
}

// uninstallModule removes the module's service only, not its image or data.
func (s *Server) uninstallModule(w http.ResponseWriter, r *http.Request) {
	m, ok := s.Modules.Get(r.Context(), r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.Docker.RemoveService(r.Context(), m.Service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Modules.Forget()
	w.Header().Set("HX-Redirect", "/modules")
	w.WriteHeader(http.StatusNoContent)
}
