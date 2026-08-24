// Package web is the panel's HTTP face: HTMX pages plus the webhook endpoint.
package web

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
)

// Version is stamped by main at build time and shown in the UI and on /healthz,
// so an admin can tell what is actually deployed.
var Version = "dev"

//go:embed templates/*.html
var files embed.FS

// Everything the browser needs is bundled: a panel on an air-gapped cluster
// must not depend on a CDN.
//
//go:embed static
var staticFS embed.FS

// staticVersion busts the browser cache exactly when the bundle changes.
var staticVersion = func() string {
	sum := sha256.New()
	fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := staticFS.ReadFile(path)
		sum.Write(b)
		return err
	})
	return hex.EncodeToString(sum.Sum(nil))[:12]
}()

// staticHandler serves the bundled CSS and htmx without authentication: they
// are not secrets, and module pages link the stylesheet from their own frame.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An hour is enough for modules linking the plain URL; our own pages
		// carry ?v=<hash> and refresh the moment the bundle changes.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	}))
}

var tpl = template.Must(template.New("").Funcs(template.FuncMap{
	"sv":      func() string { return staticVersion },
	"version": func() string { return Version },
	"human": func(b uint64) string {
		const unit = 1024
		if b < unit {
			return fmt.Sprintf("%d B", b)
		}
		div, exp := uint64(unit), 0
		for n := b / unit; n >= unit; n /= unit {
			div *= unit
			exp++
		}
		return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
	},
	"short": func(digest string) string {
		if len(digest) > 19 {
			return digest[7:19]
		}
		return digest
	},
}).ParseFS(files, "templates/*.html"))

type Server struct {
	Docker  *docker.Client
	Metrics *agent.Collector
	DB      *store.DB
	Modules *modules.Registry
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// Webhooks authenticate with their own secret and modules with their HMAC
	// token, so the role model does not apply to either.
	mux.HandleFunc("POST /hook/{token}", s.hook)
	mux.HandleFunc("GET /internal/services", s.moduleAuth("service:read", s.apiServices))
	mux.HandleFunc("GET /internal/stats", s.moduleAuth("stats:read", s.apiStats))
	mux.HandleFunc("POST /internal/services/{name}/update", s.moduleAuth("service:update", s.apiUpdateService))

	mux.Handle("GET /static/", staticHandler())
	// Unauthenticated on purpose: this is what a load balancer, an uptime check
	// or a HEALTHCHECK asks, and it reveals nothing a visitor cannot see anyway.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok %s\n", Version)
	})

	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)

	const (
		viewer   = store.RoleViewer
		operator = store.RoleOperator
		admin    = store.RoleAdmin
	)
	// Read-only: viewer.
	mux.HandleFunc("GET /{$}", s.require(viewer, s.index))
	mux.HandleFunc("GET /deploys", s.require(viewer, s.deploys))
	mux.HandleFunc("GET /resources", s.require(viewer, s.resources))
	mux.HandleFunc("GET /resources/table", s.require(viewer, s.resourcesTable))
	mux.HandleFunc("GET /services/{id}", s.require(viewer, s.serviceTab("overview")))
	mux.HandleFunc("GET /services/{id}/stats", s.require(viewer, s.serviceStats))
	mux.HandleFunc("GET /services/{id}/logs", s.require(viewer, s.serviceTab("logs")))
	mux.HandleFunc("GET /services/{id}/logs/stream", s.require(viewer, s.logsStream))
	mux.HandleFunc("GET /services/{id}/deploys", s.require(viewer, s.serviceTab("deploys")))
	mux.HandleFunc("GET /modules", s.require(viewer, s.modulesPage))
	mux.HandleFunc("GET /nav", s.require(viewer, s.nav))

	// Rolling things out: operator.
	mux.HandleFunc("POST /services/{id}/update", s.require(operator, s.updateService))
	mux.HandleFunc("POST /stacks/{name}/redeploy", s.require(operator, s.redeployStack))
	mux.HandleFunc("GET /services/new", s.require(admin, s.newServiceForm))
	mux.HandleFunc("POST /services", s.require(admin, s.createService))
	mux.HandleFunc("POST /services/{id}/remove", s.require(admin, s.removeService))
	mux.HandleFunc("GET /stacks/new", s.require(admin, s.newStackForm))
	mux.HandleFunc("POST /stacks", s.require(admin, s.deployStack))
	mux.HandleFunc("GET /stacks/{name}/edit", s.require(admin, s.editStackForm))
	mux.HandleFunc("POST /stacks/{name}", s.require(admin, s.deployStack))
	mux.HandleFunc("POST /stacks/{name}/remove", s.require(admin, s.removeStack))
	// ponytail: a module's whole UI sits behind operator — the core has no idea
	// what buttons are in there. If read-only modules appear, the manifest can
	// declare a minimum role.
	mux.HandleFunc("/modules/{name}/", s.require(operator, s.moduleProxy))

	// Changing configuration: admin.
	mux.HandleFunc("GET /services/{id}/traefik", s.require(admin, s.serviceTab("traefik")))
	mux.HandleFunc("POST /services/{id}/traefik", s.require(admin, s.saveTraefik))
	mux.HandleFunc("GET /services/{id}/webhooks", s.require(admin, s.serviceTab("webhooks")))
	mux.HandleFunc("POST /services/{id}/webhooks", s.require(admin, s.serviceCreateHook))
	mux.HandleFunc("POST /services/{id}/webhooks/{hook}/delete", s.require(admin, s.serviceDeleteHook))
	mux.HandleFunc("GET /webhooks", s.require(admin, s.webhooks))
	mux.HandleFunc("POST /webhooks", s.require(admin, s.createWebhook))
	mux.HandleFunc("POST /webhooks/{id}/delete", s.require(admin, s.deleteWebhook))
	mux.HandleFunc("GET /modules/install", s.require(admin, s.installForm))
	mux.HandleFunc("POST /modules/install", s.require(admin, s.installModule))
	mux.HandleFunc("POST /modules/{name}/uninstall", s.require(admin, s.uninstallModule))
	mux.HandleFunc("GET /nodes", s.require(admin, s.nodes))
	mux.HandleFunc("POST /nodes/tokens/{role}/rotate", s.require(admin, s.rotateJoinToken))
	mux.HandleFunc("POST /nodes/{id}/availability", s.require(admin, s.setNodeAvailability))
	mux.HandleFunc("GET /users", s.require(admin, s.usersPage))
	mux.HandleFunc("POST /users", s.require(admin, s.createUser))
	mux.HandleFunc("POST /users/{id}/delete", s.require(admin, s.deleteUser))
	mux.HandleFunc("POST /users/{id}/role", s.require(admin, s.setUserRole))
	mux.HandleFunc("POST /users/{id}/password", s.require(admin, s.setUserPassword))
	mux.HandleFunc("POST /tokens", s.require(admin, s.createAPIToken))
	mux.HandleFunc("POST /tokens/{id}/delete", s.require(admin, s.deleteAPIToken))
	return mux
}

// render buffers the page, so a template that fails halfway through produces an
// error instead of a truncated page served with 200.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error, see the panel log", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadGateway)
}

type stackGroup struct {
	Name     string
	Services []docker.Service
}

func (s *Server) servicesGrouped(ctx context.Context) ([]stackGroup, error) {
	svcs, err := s.Docker.Services(ctx)
	if err != nil {
		return nil, err
	}
	byStack := map[string][]docker.Service{}
	for _, sv := range svcs {
		stack := cmp.Or(sv.Stack, "(no stack)")
		byStack[stack] = append(byStack[stack], sv)
	}
	var out []stackGroup
	for name, list := range byStack {
		slices.SortFunc(list, func(a, b docker.Service) int { return cmp.Compare(a.Name, b.Name) })
		out = append(out, stackGroup{name, list})
	}
	slices.SortFunc(out, func(a, b stackGroup) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.renderServices(w, r, "index.html")
}

// serviceList renders the table alone, for the htmx swap after an action.
func (s *Server) serviceList(w http.ResponseWriter, r *http.Request) {
	s.renderServices(w, r, "services")
}

func (s *Server) renderServices(w http.ResponseWriter, r *http.Request, tmpl string) {
	groups, err := s.servicesGrouped(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, tmpl, groups)
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) {
	sv, ok := s.findService(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		ServiceName: sv.Name, StackName: sv.Stack, Source: "ui", TriggeredBy: "ui"})
	d, err := s.Docker.ForceUpdate(r.Context(), sv.ID)
	finish(d.OldDigest, d.NewDigest, err)
	logWarnings(d)
	if err != nil {
		fail(w, err)
		return
	}
	s.serviceList(w, r)
}

// logWarnings: Swarm warnings (an image with no digest in the registry, say)
// are not errors — the deploy happened. ponytail: log only; surface them in the
// UI if anyone actually asks.
func logWarnings(ds ...docker.Deploy) {
	for _, d := range ds {
		for _, warn := range d.Warnings {
			log.Printf("swarm warning [%s]: %s", d.Service, warn)
		}
	}
}

func (s *Server) redeployStack(w http.ResponseWriter, r *http.Request) {
	stack := r.PathValue("name")
	me := userFrom(r)
	finish, _ := s.DB.StartDeploy(store.DeployEvent{
		StackName: stack, Source: "ui", TriggeredBy: me.Username})

	// With a compose file on hand this is a real `docker stack deploy`, which
	// also picks up changes to the file. Without one — an adopted stack, or one
	// deployed before the panel existed — fall back to force-updating what runs.
	var err error
	if st, ok := s.DB.Stack(stack); ok {
		_, err = s.Docker.DeployStack(r.Context(), stack, st.Compose)
	} else {
		var done []docker.Deploy
		done, err = s.Docker.RedeployStack(r.Context(), stack)
		logWarnings(done...)
	}
	finish("", "", err)
	if err != nil {
		fail(w, err)
		return
	}
	s.serviceList(w, r)
}

// --- Traefik labels ---

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

// traefikFrom reads the current Traefik labels back into form fields.
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

func (s *Server) saveTraefik(w http.ResponseWriter, r *http.Request) {
	sv, ok := s.findService(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	host, port := r.FormValue("host"), r.FormValue("port")
	ep, cr := r.FormValue("entrypoint"), r.FormValue("certresolver")

	labels := map[string]string{
		"traefik.enable":                                 "true",
		router(sv.Name) + ".rule":                        "",
		router(sv.Name) + ".entrypoints":                 ep,
		router(sv.Name) + ".tls.certresolver":            cr,
		router(sv.Name) + ".tls":                         "",
		lbService(sv.Name) + ".loadbalancer.server.port": port,
	}
	if host != "" {
		labels[router(sv.Name)+".rule"] = "Host(" + backtick + host + backtick + ")"
	}
	if cr != "" {
		labels[router(sv.Name)+".tls"] = "true"
	}
	if host == "" && port == "" {
		labels["traefik.enable"] = "" // nothing set: take the service off Traefik
	}
	if err := s.Docker.SetLabels(r.Context(), sv.ID, labels); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/services/"+sv.ID+"/traefik")
	w.WriteHeader(http.StatusNoContent)
}

// --- Webhooks ---

// webhooks renders the whole page; hookList is the htmx fragment after a
// create or delete.
func (s *Server) webhooks(w http.ResponseWriter, r *http.Request) { s.renderHooks(w, "webhooks.html") }

func (s *Server) hookList(w http.ResponseWriter) { s.renderHooks(w, "hooklist") }

func (s *Server) renderHooks(w http.ResponseWriter, tmpl string) {
	hooks, err := s.DB.Webhooks()
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, tmpl, hooks)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	action := r.FormValue("action_type")
	if action != "force_update" && action != "stack_redeploy" {
		http.Error(w, "bad action_type", http.StatusBadRequest)
		return
	}
	if action == "force_update" && r.FormValue("service_name") == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}
	if action == "stack_redeploy" && r.FormValue("stack_name") == "" {
		http.Error(w, "stack name required", http.StatusBadRequest)
		return
	}
	_, err := s.DB.CreateWebhook(store.Webhook{
		ServiceName:  r.FormValue("service_name"),
		StackName:    r.FormValue("stack_name"),
		ActionType:   action,
		ImagePattern: r.FormValue("image_pattern"),
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.hookList(w)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.DB.DeleteWebhook(id); err != nil {
		fail(w, err)
		return
	}
	s.hookList(w)
}

// hook is the entry point for CI and registries. The secret is in the path and
// the body is ignored.
// ponytail: image_pattern is not matched against the payload yet — GHCR and
// Docker Hub disagree on the format. Add parsing once a specific registry shows up.
func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	hook, err := s.DB.WebhookByToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.DB.TouchWebhook(hook.ID)
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

func (s *Server) deploys(w http.ResponseWriter, r *http.Request) {
	events, err := s.DB.RecentDeploys(100)
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, "deploys.html", events)
}

// --- Logs ---

// logsStream is the SSE stream. It ends when the client goes away (ctx is cancelled).
func (s *Server) logsStream(w http.ResponseWriter, r *http.Request) {
	tail, err := strconv.Atoi(r.FormValue("tail"))
	if err != nil || tail < 1 || tail > 5000 {
		tail = 200
	}
	lines, err := s.Docker.Logs(r.Context(), r.PathValue("id"), r.FormValue("task"), tail)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	for line := range lines {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return
		}
		rc.Flush()
	}
}
