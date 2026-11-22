// Package web is the panel's HTTP face: the JSON API, the embedded SPA, the
// module proxy and the webhook endpoint.
package web

import (
	"cmp"
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
	"modularnost/internal/updates"
	webui "modularnost/web"
)

// Version is set by main at build time.
var Version = "dev"

type Server struct {
	Docker             *docker.Client
	Metrics            *agent.Collector
	DB                 *store.DB
	RegistryEncryption cipher.AEAD
	Modules            *modules.Registry
	// Secret is the shared module secret: the firewall services present a token made from it.
	Secret   string
	Firewall FirewallConfig
	fw       firewallStates
	Pages    PageHub
	Updates  *updates.Checker // nil: no release check
	Shutdown context.Context  // cancellation closes event streams so other requests can drain
	// Addr is the listen address; modules are told its port to reach the core.
	Addr string

	// AgentService is the swarm service of the node agents; empty on a single node.
	AgentService string
	// StackFile is the compose file the installer wrote, when it is mounted here.
	StackFile  string
	updating   atomic.Bool
	stackMu    sync.Mutex
	stackLocks map[string]*stackLock

	// TrustedProxies are the peers allowed to supply forwarding headers.
	TrustedProxies []netip.Prefix
	logins         loginLimiter
	hookRequests   webhookLimiter
}

func (s *Server) streamContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if s.Shutdown == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(s.Shutdown, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// Webhooks and modules authenticate themselves; roles do not apply.
	mux.HandleFunc("POST /hook/{token}", s.hook)
	mux.HandleFunc("GET /internal/services", s.moduleAuth("service:read", s.apiServices))
	mux.HandleFunc("GET /internal/stats", s.moduleAuth("stats:read", s.apiStats))
	mux.HandleFunc("POST /internal/services/{name}/update", s.moduleAuth("service:update", s.apiUpdateService))
	mux.HandleFunc("POST /internal/services", s.moduleAuth("service:create", s.apiCreateService))
	mux.HandleFunc("POST /internal/services/{name}/image", s.moduleAuth("service:create", s.apiSetServiceImage))
	// The node firewalls ask for their rules and say what they applied.
	mux.HandleFunc("GET /internal/firewall", s.firewallSpec)
	mux.HandleFunc("POST /internal/firewall/applied", s.firewallApplied)
	// A module needs no permission to tell that its own page changed.
	mux.HandleFunc("POST /internal/ui/changed", s.moduleAuth("", s.uiChanged))

	// Unauthenticated: load balancers and HEALTHCHECK call it.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok %s\n", Version)
	})

	s.registerAPI(mux)
	mux.HandleFunc("/api/v1/", apiNotFound)

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
	http.Error(w, backendMessage("docker_error", err), http.StatusBadGateway)
}

func backendMessage(code string, err error) string {
	var exit *exec.ExitError
	var request *url.Error
	if errors.As(err, &exit) {
		// CLI output can include values from the submitted Compose file.
		err = exit
	} else if errors.As(err, &request) {
		err = fmt.Errorf("%s: %w", request.Op, request.Err)
	}
	log.Printf("%s: %v", code, err)
	switch code {
	case "docker_unavailable":
		return "Docker is unavailable"
	case "docker_error":
		return "Docker operation failed"
	case "metrics_unavailable":
		return "could not load metrics"
	case "deploy_failed":
		return "deployment failed"
	case "module_error":
		return "module request failed"
	case "updates_unavailable":
		return "could not check for updates"
	case "backup_failed":
		return "could not back up the database"
	default:
		return "internal server error"
	}
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
	if wait := s.hookRequests.take(clientIP(r, s.TrustedProxies), time.Now()); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
		http.Error(w, "too many webhook requests", http.StatusTooManyRequests)
		return
	}
	hook, err := s.DB.WebhookByToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.DB.TouchWebhook(hook.ID); err != nil {
		log.Printf("webhook %d: %v", hook.ID, err)
	}
	if hook.ActionType == "stack_redeploy" {
		defer s.lockStack(hook.StackName)()
	}
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: hook.ServiceName, StackName: hook.StackName,
		Source: "webhook", TriggeredBy: "webhook#" + strconv.FormatInt(hook.ID, 10),
	})

	var d docker.Deploy
	if hook.ActionType == "stack_redeploy" {
		var done []docker.Deploy
		done, err = s.Docker.RedeployStack(r.Context(), hook.StackName, s.serviceRegistryAuth)
		logWarnings(done...)
	} else {
		d, err = s.Docker.ForceUpdate(r.Context(), hook.ServiceName, s.serviceRegistryAuth)
		logWarnings(d)
	}
	finish(d.OldDigest, d.NewDigest, err)
	if err != nil {
		// Never log the secret, only the webhook id.
		http.Error(w, backendMessage("deploy_failed", fmt.Errorf("webhook %d: %w", hook.ID, err)), http.StatusBadGateway)
		return
	}
	w.Write([]byte("ok\n"))
}
