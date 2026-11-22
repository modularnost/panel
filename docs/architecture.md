# Architecture

Why this project is shaped the way it is: the decisions, their reasons, and the
things it deliberately will not do. Read this before proposing a feature — it
answers most "why not just…" questions, and says which answers are settled.

## Project scope

The core provides the tools needed to deploy and operate stacks and services
in Docker Swarm. Install modules when you need a separate workflow, such as
building images or storing metrics history. The core runs without modules.

The panel supports a new server with its own Traefik and an existing Swarm
with its own reverse proxy. See the [installation guide](../README.md#installation).

## Technology stack

- **Language**: Go (the only backend language of the core; static build, single
  binary)
- **Orchestration**: the Docker Swarm API via the `docker/docker` client SDK —
  **Swarm only**, with no support for plain Docker/docker-compose as a separate
  backend (see the ADR below)
- **Database**: SQLite (see the "Database" section)
- **Frontend**: a React + TypeScript SPA (Vite, Bun for install and scripts)
  over the JSON API, embedded in the binary (see ADR 6)
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
GET  /manifest   -> {contract, name, version, contributions[], permissions[]}
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

### 6. A SPA over a JSON API (decided 2026-10-14)

The UI is a React + TypeScript single-page app talking to `/api/v1`. The reason
is the work ahead, not the current screens: live status, logs and later
features want client state, and one API serves scripts and CI as well.

- the built bundle is embedded in the binary: still one binary, one image
- the SPA is served at `/`; the server renders no HTML of its own. A
  module's page is the SPA route `/module/<name>/`, drawn from the module's
  document (ADR 8)
- `/api/v1` is the contract for the UI and external clients, authenticated as
  today (session cookie or token); `/internal/*` stays the module API
- the SPA draws what modules contribute to its own screens (see ADR 7)
- the API is written with Huma: `api/openapi.json` is generated from the
  handlers and committed, and the TypeScript types are generated from it
- state-changing requests accept only `application/json`, which a plain HTML
  form cannot send: that is the CSRF guard

### 7. A module is a set of contributions (decided 2026-10-14)

A module is more than a page of its own: it needs a tab on a service, an action
on the services list, a menu entry. The manifest says where, and the panel draws
it.

**Trust.** A module is code the administrator installed, so it is trusted. Even so
it never sees the user's session: the panel asks the module itself, server to
server, and nothing the browser sent, cookies included, goes along. A module
draws nothing in the browser, so it shares no origin with the panel.

**The manifest uses contract 3.** See [the module contract](modules.md#the-contract)
for an example and the complete schema.

- A contribution names a **slot**, a title, a path on the module, a
  minimum role and, for a tab, labels the service must carry. The slots are
  `nav`, `service.tab` and `services.action`; the list grows as real modules
  need more.
- A slot or a field the core does not know is **ignored**, so a module written
  for a newer panel loads on an older one. A change that cannot be made that way
  raises `contract`; a manifest of another contract is shown as failed, with the
  reason. We break rather than carry two shapes before 1.0.
- The **role belongs to the contribution** and is enforced by the core, by the
  longest declared path that is a prefix at a `/` boundary; a path nothing
  covers needs `operator`. The SPA hides what a role cannot use, but only as a
  courtesy.
- **Who is looking** is told to the module in signed headers (`X-Panel-User`,
  `X-Panel-Role`, `X-Panel-Time`, `X-Panel-User-Sig`), an HMAC keyed with the
  module's own token: the module can verify it and holds no shared secret.
- **Context** (the service a tab is on) comes from the slot, as query
  parameters of the request. It is a convenience: anybody can write that
  query, so the role check, not the context, is what protects an action.

**Rendering.** A contribution is a document the panel draws: see ADR 8. Until
2026-10-22 it could also be a page in a frame; that mode is gone (ADR 8).

**Not chosen.** A JavaScript bundle per module loaded into the panel's page:
it would run with the panel's privileges and bind modules to the SPA's
libraries, which is a different trust model from "any language, a separate
service" (ADR 3).

**Open.** Core-to-module events and more rights on `/internal` come with the
first module that needs them.

### 8. A module may describe its page and let the panel draw it (decided 2026-10-15)

Most module pages are a few numbers, a table, a chart and a form. The module
describes the page and the panel draws it: in its own look, with its seed color,
dark scheme and phone layout, and with no module code in the browser.

- **The only mode** (changed 2026-10-22, #96). The first version kept framed
  HTML pages beside this one, for what the vocabulary cannot say. Nobody needed
  it: heavy tools such as Grafana are linked to (ADR 5), not framed. Removing it
  took the HTML proxy, `/static/` and the `render` field with it, and raised
  `contract` to 3.
- **The document.** A `GET` on the contribution's path answers
  `{"ui": 1, "blocks": [...]}`; the role, the signed identity and the context are as in
  ADR 7. The first vocabulary: text, stat, key-value list, table, meter, chart,
  banner, link, code, button, form (text, select, switch), and a select that
  re-requests the page with a query parameter, for the range of a chart. Text
  is only text: no HTML, styles or scripts. A link points inside the panel or to
  `https`. Size, block and point counts are limited. A block the panel does not
  know is shown as unsupported and the rest still draws, the same rule as an
  unknown slot.
- **Charts.** The panel draws the points a module sends. The module keeps the
  history (ADR 5).
- **Actions.** A button or form `POST`s to the module's `/action/*` with the
  signed headers and the role of that path. A destructive one asks first, in the
  panel's dialog.
- **Updates come from the module, not from polling.** The module tells the core
  that a page changed (`POST /internal/ui/changed`, with its token, for its own
  pages only), the core relays it over a stream to open browsers, and each
  browser refetches the visible page as its own viewer, so a role never gets a
  page it may not see. It is also fetched on open and on reconnect. An event
  says "refetch", not what changed: the document stays the one source of truth.
  This is the opposite direction to the core-to-module events of ADR 7.

**Not chosen.** An expression or data-binding language: the module puts the
data in the document. Module-supplied HTML, CSS or JavaScript in the panel's
page (ADR 7). A vocabulary ahead of the modules: a block is added with a real
module that needs it.

See [the module contract](modules.md) for block schemas and change events.

### 9. A node firewall of our own (decided 2026-10-15)

A fresh install left the swarm's ports (2377, 7946, 4789) on the public
interface: `docker swarm init` listens everywhere, and nothing restricted it.
The panel holds the cluster, so the host it starts on is not left that way.

- **A service with the host's network.** `PANEL_MODE=firewall`, the panel's
  binary, global, with `network: host` and `cap_add: NET_ADMIN` and no Docker
  socket. A Swarm service cannot be privileged, and the host's `iptables` need
  the host's network namespace. It is separate from the agent on purpose: the
  agent reads the socket and is on the overlay; this one changes the host and
  must not be. It listens on nothing, so the host gains no port: it asks the
  panel at its public address (`PANEL_CORE_URL`, a host-network container is off
  the overlay), authenticated with the module secret as the agents are, applies
  what it is told and reports back.
- **One mechanism, ours.** One chain, `MODULARNOST`, hooked into `INPUT`, rebuilt
  whole and atomically with `iptables-restore`, for IPv4 and IPv6. Not `ufw`:
  Debian has none, and two firewalls on one host confuse each other. In order:
  established traffic, loopback and the Docker bridges, SSH (the port or ports
  `sshd` listens on, and 22 always, whatever the panel says), the allowed ports,
  the swarm's ports for the nodes' addresses and ESP, then drop. A rule set that
  cannot be applied leaves the previous one; a stopped service leaves its rules
  in the kernel, which fails closed.
- **Closes what we opened, breaks nothing else.** The swarm's ports are closed
  to everyone but the nodes, whose addresses come from Swarm itself, and to a
  server that was allowed to join, which the Nodes page asks for before the
  join. What the host already listened on before the install is allowed and
  listed, and it is the starting point of the Ports page.
- **The overlay is created encrypted, when the kernel can.** It cannot be
  encrypted afterwards, and on one node it costs nothing. Docker does it with
  IPsec, which needs the `esp4` module; some images switch it off, and an
  encrypted overlay then carries nothing between servers. The installer
  creates the network plain there and says so (WireGuard instead is #94).
- **Published ports are closed by a second chain.** A port a container
  publishes is redirected (DNAT) before `INPUT`, so `MODULARNOST-FWD`, hooked
  into `DOCKER-USER`, drops what was redirected unless its original port is
  allowed; traffic from the docker bridges is not touched. Docker keeps
  `DOCKER-USER` across restarts and the service restores the jump every 30
  seconds if it is gone.
- **Allowed ports live in the database,** not as service labels, which a
  `docker stack deploy` would remove. The installer's list of earlier
  listeners is copied in once; the Ports page changes it from there.

**Not chosen.** `ufw`. Listening on loopback: a worker has to reach 2377.
Putting the rules in the panel or the agent: they would need the host's network.
## Scope boundaries

- Docker Swarm is the deployment backend.
- The core deploys ready-made images. Image builds and metrics history are
  optional module features.
- Modules run through the panel's HTTP contract and permissions. They have
  no direct access to the Docker socket.

## Database (SQLite)

SQLite stores panel-owned data: saved Compose, deploy history, webhooks,
users and tokens, firewall rules, and encrypted registry credentials.
Swarm remains the source for live state. The [migrations](../internal/store/migrations)
define the schema.

Run one panel replica. If the database volume is local to a node, pin the
panel there. Back up the volume before upgrading and the registry encryption
key separately if you use saved credentials.

## Development and releases

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the workflow, verification, coding
rules and release steps.

## Interface text

Use short labels and sensible defaults. Explain consequences that are hard
to infer, such as which services a deploy will remove. Put tutorials and
background information in the documentation.
