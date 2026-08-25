# Architecture

Why this project is shaped the way it is: the decisions, their reasons, and the
things it deliberately will not do. Read this before proposing a feature — it
answers most "why not just…" questions, and says which answers are settled.

It is also the working brief for AI assistants (`CLAUDE.md` points here).

## About the project

A lightweight web panel for managing Docker Swarm clusters (1-N nodes),
**deliberately narrow in scope**: not a PaaS, not a build system, not a
multi-cloud tool. The target user already knows Docker Swarm, uses CI/CD
(GitHub Actions and the like) to build images, and wants a small, predictable UI
on top of existing infrastructure — few screens, each doing an obvious thing,
with webhook-driven redeploys available out of the box.

Open source is planned. Positioning: **Swarm-native, lightweight, modular**.

## Technology stack

- **Language**: Go (the only backend language of the core; static build, single
  binary)
- **Orchestration**: the Docker Swarm API via the `docker/docker` client SDK —
  **Swarm only**, with no support for plain Docker/docker-compose as a separate
  backend (see the ADR below)
- **Database**: SQLite (see the "Database" section)
- **Frontend**: HTMX + a template engine (templ or html/template) — no separate
  SPA build unless there is an explicit reason to move to one
- **Docker API access**: only over the Unix socket on a manager node; never
  expose `docker.sock` externally without an authenticating layer in front

## Architectural decisions (ADR-style, do not revisit without explicit discussion)

### 1. Docker Swarm only, no plain Docker support

Even a single-node deployment initialises Swarm (`docker swarm init` with no
parameters). This gives one API contract (`ServiceList`, `ServiceUpdate`,
overlay networks, rolling updates, health checks out of the box) regardless of
node count. Supporting two backends (`DockerBackend` / `SwarmBackend`) is a
deliberately deferred idea — do not build it "just in case".

### 2. Docker Swarm is the source of truth; the DB holds only what Swarm lacks

Never duplicate in the database anything that a live query to the Docker API can
answer: the list of services, replicas, health, the current image/tag, labels
(including Traefik labels), the list of nodes. Cache in memory with a short TTL,
not in the database. Reason: avoid the UI drifting from the real cluster state.
A panel that reports a service as healthy after it has crashed is worse than no
panel — it is trusted and wrong.

### 3. Modules are separate Docker services, not Go plugins

Do not use the Go `plugin` package (`.so`) — it binds fragilely to the compiler
version, cannot be unloaded at runtime, and offers no fault isolation. A module
is a separate service in the Swarm stack that marks itself with labels:

```text
panel.module=true
panel.module.name=<name>
panel.module.version=<semver>
```

The core discovers modules by scanning services for those labels (the same
discovery mechanism the panel's own functionality uses) and talks to a module
over HTTP inside the overlay network:

```text
GET  /manifest   -> {name, version, nav_entries[], permissions[]}
GET  /health
POST /action/*   -> the module's own actions
```

A module gets **no** direct access to `docker.sock`. If a module needs to act on
Swarm (updating a service, say), it must ask the core through the core's
internal API with a token — the core remains the single point that holds Docker
API access. That limits the damage a buggy or dishonest third-party module can
do.

A module may be implemented in any language as long as it honours the HTTP
contract. The core being written in Go does not constrain that.

The **node agent** is not a module. Cluster-wide container stats need a reader
on every node, and that reader must hold `docker.sock` — which modules never
get. So the agent is the panel's own binary in `PANEL_MODE=agent`, deployed as a
`global` service. Keep it that way: if a future feature needs the socket on
every node, extend the agent rather than granting a module access.

### 4. Traefik is configured through labels, not integrated directly

The panel has no interaction of its own with the Traefik API. All the work
amounts to the panel offering a form or presets that translate into
`docker service update --label-add ...` on the target service. Traefik picks the
changes up through its own Docker provider. Never try to write Traefik
configuration directly (static files, a separate provider) — that is complexity
without a need at this stage.

### 5. Monitoring is linked to, not built in

Do not duplicate Grafana's functionality inside the panel. At most, store
`service_name -> grafana_dashboard_url` and render a deep link from the service
card. A full monitoring module (if there ever is one) is a separate Swarm
service following the module contract (see 3), not part of the core.

Where the line actually falls (refined 2026-08-24): **current numbers the core
already has are status, not monitoring**, and belong in the panel — the
Resources page and the per-service tab show CPU and memory the same way they
show replica counts. What stays out of the core is everything that needs
storage: history, charts, thresholds, alerting. That is what a monitoring module
or Grafana is for.

`GET /internal/stats` exposes the same numbers to modules (see the agent in 3).
`examples/metrics-module` is a skeleton demonstrating that permission, not a
feature — it renders what the panel already renders.

## Feature order (do not change without discussion)

1. **Core MVP**:
   - a list of stacks/services with status (replicas, health, image+tag) — read
     straight from the Docker API, not stored in the database
   - a Traefik labels screen: a form instead of editing labels by hand
   - a webhook endpoint: accept an event from a registry/CI → `ServiceUpdate`
     with a force pull, so pushing an image is enough to roll it out
   - a "redeploy stack" button — a wrapper over `docker stack deploy`
   - simple auth (basic auth or a single shared token), no full RBAC
2. **Logs** (second priority, do not defer for long):
   - stream `docker service logs` over SSE/WebSocket
   - filter by task/replica (a Swarm service has several tasks)
   - optionally: if the user turns out to run Loki, proxy queries to it instead
     of reading docker logs directly, so logs survive a container restart
3. **The module system**: discovery, manifests, a module catalogue (along the
   lines of the Traefik plugin catalogue), CLI scaffolding for module authors.
   Decided 2026-08-24 on how modules are distributed:
   - the bundled examples stay in this repository — they are documentation,
     tested against the current contract and fixed in the same commit when it
     changes
   - real modules live in their own repositories, with their own CI and image:
     a module inside this repository is not a module, it is part of the panel
   - the catalogue starts as `docs/catalog.md`, a table maintained by pull
     request. A catalogue repository with a machine-readable index is worth it
     only once there are several external modules — and only then should the
     panel read it and offer to install an entry through `docker stack deploy`
   - entries are `community` by default. `verified` means someone read the code
     and the entry pins an image digest; a badge nobody backs is worse than no
     badge
4. **RBAC and multiple users**:
   - a proper `users` table with roles (for example: admin / operator / viewer)
   - separated actions: who may edit webhooks and Traefik labels, who may only
     watch status and logs, who may redeploy
   - binding roles to specific stacks/services (not just a global role), if a
     real "different teams, different services" scenario appears
   - api_tokens limited by role/scope, not only per user
   - do not make it more complex than the start requires: 2-3 roles are enough,
     and a full policy engine (Casbin and friends) only if a clear need appears
5. **Creating services and stacks** (explicitly requested, implemented):
   - a single service through the Swarm API (`ServiceCreate`): image, replicas,
     ports, env, networks
   - a stack through `docker stack deploy` from a pasted compose file. Compose is
     a client-side format, not a daemon one, so the panel image ships
     `docker-cli` instead of a re-implemented compose loader
   - the compose file is kept in the `stacks` table: Swarm does not store it, and
     without it a stack can neither be edited nor genuinely redeployed (only
     force-updated)
   - creating and removing are `admin` only: a compose file is effectively access
     to the whole cluster
6. **A one-command install for a fresh VPS** (proposed 2026-08-24, not started):
   a script that takes a bare server to a working panel — `docker swarm init`,
   an overlay network, Traefik with an ACME resolver, and the panel behind it on
   a real domain with TLS. Points to settle when it is written:
   - the panel stops publishing 8080 and gets Traefik labels of its own, so the
     only ports open on the host are 80 and 443
   - ACME storage lives on a volume pinned to the same node as the SQLite file
   - the script asks for a domain and an email, and nothing else; the admin
     password is generated and printed once
   - it must be safe to re-run: an existing swarm, network or stack is reused,
     not recreated
   - Traefik itself stays a normal stack the user owns — the panel still only
     writes labels and never talks to its API (ADR 4)
7. **A metrics history module** (proposed 2026-08-24, not started): the first
   module that earns the contract — it needs storage and a lifecycle of its own,
   which is exactly what the core must not grow. Scope it as CPU and memory per
   service over a few days with a chart on the service card, not as a Grafana
   clone with datasources, a query language and alerting.
   - **Prerequisite, done 2026-08-25**: a module used to learn its token only
     from the first proxied user request, so it could not poll on a schedule
     with nobody watching. The manifest request now carries the token, and the
     panel refreshes discovery on a timer, so every module has one within a TTL
     of the core starting. Injecting it as an env var is no longer needed: a
     token that arrives on every refresh survives a new PANEL_MODULE_SECRET,
     which an injected one would not
   - the hard part is retention, not charts: downsampling, a retention window,
     and what happens to services that disappear. Sampling every 10s forever is
     millions of rows a week on a modest cluster
   - if several consumers start polling, teach the core to sample continuously
     and serve the last snapshot: every /internal/stats call costs a second,
     because CPU percent needs two readings for a delta
8. Everything else (a build system, git integration for building images) is out
   of the current scope — do not implement without an explicit request

## Deliberate non-goals (anti-scope)

- Do not turn the project into a PaaS: no git builds, no Nixpacks, no one-click
  databases, no catalogues of hundreds of one-click services. A compose form and
  a run-a-service form edit what the user wrote themselves — they do not build
  anything for them. That is the line.
- Do not drag in plain Docker as an alternative backend
- Do not embed graphs/metrics in the core — those belong in Grafana or in a
  separate module
- Do not give modules direct access to `docker.sock`
- Do not store in SQLite what already lives in Swarm (see ADR 2)

## Database (SQLite)

Store only:

```sql
-- webhooks for automatic service updates
webhooks (
  id, service_name, stack_name,
  secret_token, action_type,      -- 'force_update' | 'stack_redeploy'
  image_pattern,
  created_at, last_triggered_at
)

-- deploy history (the one thing Swarm itself does not keep)
deploy_events (
  id, service_name, stack_name,
  trigger_source,                 -- 'webhook' | 'manual' | 'ui'
  old_image_digest, new_image_digest,
  status,                         -- 'pending' | 'success' | 'failed'
  triggered_by,
  started_at, finished_at,
  error_message
)

-- users/tokens (minimal, no complex RBAC at the start)
users (id, username, password_hash, role, created_at)
api_tokens (id, user_id, token_hash, expires_at)

-- saved presets for the Traefik labels form
traefik_presets (id, name, middleware_config_json)

-- links to monitoring dashboards
service_grafana_links (service_name, dashboard_url)

-- stack compose files: Swarm has none, `docker stack deploy` is a client-side
-- operation and the daemon only ever sees the resulting services
stacks (name, compose, updated_by, updated_at)
```

If the panel ever migrates between Swarm nodes through placement, the SQLite
file must be pinned to a specific node by a placement constraint or live on a
shared volume. Consider moving to Postgres only if a real need for HA of the
panel itself appears (several instances writing at once) — do not do it ahead of
time "just in case".

## Releases

Pushing to `main` publishes `edge` and `sha-<commit>`. `latest` and the version
tags come from a git tag: `git tag v0.1.0 && git push --tags`. That separation
exists because the README tells people to deploy `latest` — it must mean a
release someone chose to cut, not the last thing that happened to be pushed.

The version compiled into the binary comes from `git describe --tags --always`,
so `/healthz` answers `v0.1.0` on a release and `v0.1.0-4-gabc1234` in between.

## Style and general rules

- Before adding a new feature, check it against the "anti-scope" section above
- All interaction with the Docker API goes through a single abstraction layer
  (`internal/docker` or equivalent); do not smear SDK calls across handlers
- Never log secrets (webhook tokens, `docker.sock` access)
- All DB migrations are versioned files; never edit an already-applied migration
  after the fact
- When in doubt whether a feature belongs in the MVP, look at the priority list
  above and ask rather than adding it "while we're here"

### Comments

Comment what is not obvious from the code, and nothing else. A comment earns its
place when it explains **why** — a workaround, a constraint from Docker's API, a
deliberate shortcut, a trap the next reader would otherwise fall into. Restating
what the line already says is noise that rots on the next edit.

Do not narrate: no `// loop over services`, no comment repeating a function name,
no section banners for a five-line function. If a comment is needed to explain
*what* code does, rename things or split the function first — that fixes the
cause.

### Interface text

The same rule, harder. Explanatory paragraphs under a form usually mean the form
is bad: a field that needs a sentence needs a better label, a placeholder, or a
sensible default instead. An interface that has to be narrated does not work.

Cut: descriptions of what a page obviously is, restatements of what a button
does, tutorials on how the panel works internally.

Keep: consequences the user cannot see and cannot undo (this deletes services
not in the file; this token lets anyone join the cluster), and short answers to
"why can I not do X here" — those save a support round-trip. One sentence, not a
paragraph.

Documentation belongs in `docs/`, not in the UI.
