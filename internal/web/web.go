// Package web is the panel's HTTP face: the JSON API, the embedded SPA, the
// module proxy and the webhook endpoint.
package web

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
	"modularnost/internal/updates"
	webui "modularnost/web"
)

// Version is set by main at build time.
var Version = "dev"

// Bundled so that an air-gapped cluster needs no CDN. Modules link panel.css
// from their frames, so /static/ is part of the module contract.
//
//go:embed static
var staticFS embed.FS

// staticHandler serves the bundle without auth: module frames link the stylesheet.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Plain URLs (modules) may be an hour stale.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	}))
}

type Server struct {
	Docker  *docker.Client
	Metrics *agent.Collector
	DB      *store.DB
	Modules *modules.Registry
	Updates *updates.Checker // nil: no release check
	// Addr is the listen address; modules are told its port to reach the core.
	Addr string

	// AgentService is the swarm service of the node agents; empty on a single node.
	AgentService string
	// StackFile is the compose file the installer wrote, when it is mounted here.
	StackFile string
	updating  atomic.Bool
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// Webhooks and modules authenticate themselves; roles do not apply.
	mux.HandleFunc("POST /hook/{token}", s.hook)
	mux.HandleFunc("GET /internal/services", s.moduleAuth("service:read", s.apiServices))
	mux.HandleFunc("GET /internal/stats", s.moduleAuth("stats:read", s.apiStats))
	mux.HandleFunc("POST /internal/services/{name}/update", s.moduleAuth("service:update", s.apiUpdateService))

	mux.Handle("GET /static/", staticHandler())
	// Unauthenticated: load balancers and HEALTHCHECK call it.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok %s\n", Version)
	})

	s.registerAPI(mux)
	mux.HandleFunc("/api/v1/", apiNotFound)

	// A module's whole UI needs operator: the core cannot tell what is inside.
	mux.HandleFunc("/modules/{name}/", s.require(store.RoleOperator, s.moduleProxy))

	// Every other GET is a client-side route of the SPA.
	if dist, err := fs.Sub(webui.Dist, "dist"); err == nil {
		mux.HandleFunc("GET /app/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/"+strings.TrimPrefix(r.URL.RequestURI(), "/app/"), http.StatusMovedPermanently)
		})
		// No method in the pattern: it would conflict with /api/v1/.
		spa := spaHandler(dist)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			spa(w, r)
		})
	}
	return mux
}

func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadGateway)
}

// logWarnings: Swarm warnings are not errors, the deploy happened.
func logWarnings(ds ...docker.Deploy) {
	for _, d := range ds {
		for _, warn := range d.Warnings {
			log.Printf("swarm warning [%s]: %s", d.Service, warn)
		}
	}
}

type traefikForm struct {
	Service    docker.Service
	Host       string
	Port       string
	Entrypoint string
	CertResolv string
}

func router(name string) string    { return "traefik.http.routers." + name }
func lbService(name string) string { return "traefik.http.services." + name }

func (s *Server) findService(ctx context.Context, id string) (docker.Service, bool) {
	svcs, err := s.Docker.Services(ctx)
	if err != nil {
		return docker.Service{}, false
	}
	for _, sv := range svcs {
		if sv.ID == id || sv.Name == id {
			return sv, true
		}
	}
	return docker.Service{}, false
}

func traefikFrom(sv docker.Service) traefikForm {
	l := sv.Labels
	return traefikForm{
		Service:    sv,
		Host:       hostFromRule(l[router(sv.Name)+".rule"]),
		Port:       l[lbService(sv.Name)+".loadbalancer.server.port"],
		Entrypoint: cmp.Or(l[router(sv.Name)+".entrypoints"], "websecure"),
		CertResolv: l[router(sv.Name)+".tls.certresolver"],
	}
}

const backtick = "`"

// hostFromRule pulls the domain out of Host(`example.com`); anything fancier
// goes through labels by hand.
func hostFromRule(rule string) string {
	rule = strings.TrimPrefix(rule, "Host("+backtick)
	return strings.TrimSuffix(rule, backtick+")")
}

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9.-]*$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)
)

// validateTraefik keeps what lands in a Traefik rule to host-name characters:
// a backtick in the host would let the form rewrite the whole rule.
func validateTraefik(host, port, entrypoint, resolver string) error {
	if len(host) > 253 || !hostRe.MatchString(host) {
		return errors.New("host: letters, digits, dots and hyphens only")
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return errors.New("port: a number from 1 to 65535")
		}
	}
	if len(entrypoint) > 64 || !nameRe.MatchString(entrypoint) {
		return errors.New("entrypoint: letters, digits, hyphens and underscores only")
	}
	if len(resolver) > 64 || !nameRe.MatchString(resolver) {
		return errors.New("certresolver: letters, digits, hyphens and underscores only")
	}
	return nil
}

// traefikLabels is what the Traefik form writes; an empty value removes a label.
func traefikLabels(service, host, port, entrypoint, resolver string) map[string]string {
	labels := map[string]string{
		"traefik.enable":                                 "true",
		router(service) + ".rule":                        "",
		router(service) + ".entrypoints":                 entrypoint,
		router(service) + ".tls.certresolver":            resolver,
		router(service) + ".tls":                         "",
		lbService(service) + ".loadbalancer.server.port": port,
	}
	if host != "" {
		labels[router(service)+".rule"] = "Host(" + backtick + host + backtick + ")"
	}
	if resolver != "" {
		labels[router(service)+".tls"] = "true"
	}
	if host == "" && port == "" {
		labels["traefik.enable"] = "" // nothing set: take the service off Traefik
	}
	return labels
}

// hook is the entry point for CI and registries: the secret is in the path, the
// body is ignored.
func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	hook, err := s.DB.WebhookByToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.DB.TouchWebhook(hook.ID); err != nil {
		log.Printf("webhook %d: %v", hook.ID, err)
	}
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: hook.ServiceName, StackName: hook.StackName,
		Source: "webhook", TriggeredBy: "webhook#" + strconv.FormatInt(hook.ID, 10),
	})

	var d docker.Deploy
	if hook.ActionType == "stack_redeploy" {
		var done []docker.Deploy
		done, err = s.Docker.RedeployStack(r.Context(), hook.StackName)
		logWarnings(done...)
	} else {
		d, err = s.Docker.ForceUpdate(r.Context(), hook.ServiceName)
		logWarnings(d)
	}
	finish(d.OldDigest, d.NewDigest, err)
	if err != nil {
		// Never log the secret, only the webhook id.
		log.Printf("webhook %d: %v", hook.ID, err)
		http.Error(w, "deploy failed", http.StatusBadGateway)
		return
	}
	w.Write([]byte("ok\n"))
}
