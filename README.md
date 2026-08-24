# modularnost

A web panel for Docker Swarm: one binary, SQLite, no frontend build step.

For people who already build images in CI and want a UI on top of a running
cluster — check status, redeploy a service, edit Traefik labels, read logs.

**Not** a PaaS: it doesn't build images, doesn't deploy databases in one click,
and doesn't support plain Docker without Swarm. Metrics and graphs belong in
Grafana or in a module, not in the core.

Status: works, but the API and DB schema may still change.

## Running it

Requires Go 1.26+ and Docker with Swarm active (`docker swarm init` — even on a
single node). Tested on Docker 28.3.

```sh
go run .
```

The first admin's password comes from `PANEL_ADMIN_PASSWORD`; without it one is
generated and printed to the log once.

On Swarm — [stack.yml](stack.yml) already pins the panel to a manager node and
mounts a volume for the database:

```sh
PANEL_ADMIN_PASSWORD=<password> PANEL_MODULE_SECRET=<secret> \
  docker stack deploy -c stack.yml panel
```

That pulls `ghcr.io/modularnost/panel:latest`, published by CI for amd64 and
arm64. To run your own build instead:

```sh
docker build -t modularnost:latest .
PANEL_IMAGE=modularnost:latest PANEL_ADMIN_PASSWORD=<password> \
  PANEL_MODULE_SECRET=<secret> docker stack deploy -c stack.yml panel
```

On a multi-node cluster the image must come from a registry every node can
reach — a locally built one exists only on the machine that built it, and the
agent will not start anywhere else.

That brings up the panel on a manager plus one metrics agent per node.

The panel needs `/var/run/docker.sock` (read-only) on a manager node. Never
expose that socket directly — the panel is the authenticated layer in front of
it.

| Variable | Default | |
| --- | --- | --- |
| `PANEL_ADDR` | `:8080` | |
| `PANEL_DB` | `modularnost.db` | SQLite file |
| `PANEL_ADMIN_USER` | `admin` | first start only |
| `PANEL_ADMIN_PASSWORD` | generated | first start only |
| `PANEL_MODULE_SECRET` | new on every start | signs module and agent tokens; pin it if you run agents |
| `PANEL_AGENT_SERVICE` | unset | agent service name, e.g. `panel_agent`; unset means single-node |
| `PANEL_AGENT_PORT` | `8080` | port the agents listen on |
| `PANEL_MODE` | unset | `agent` runs this binary as a node agent instead of the panel |

## What it does

- **Services** — stacks, replicas, image and digest. Read live from the Docker
  API and never mirrored into the database, so the UI can't lie about the
  cluster. Each service has its own page: overview, logs, deploy history,
  Traefik labels and webhooks.
- **Update** — force-update that re-pulls the image from the registry, so a CI
  push is enough to roll a service forward.
- **Redeploy stack** — the same across every service in a stack.
- **Logs** — SSE stream, filter by replica, pause. Lines from different
  replicas are tagged with their slot: `[1]`, `[2]`.
- **Traefik** — a form instead of hand-editing labels. The panel only writes
  labels; Traefik picks them up on its own.
- **Webhooks** — `POST /hook/<secret>` from CI.
- **Create** — run a single service from a form, or deploy a stack from a
  compose file (stored, so it can be edited and redeployed).
- **Deploy history** — the one thing Swarm itself doesn't keep.
- **Resources** — current CPU and memory of every replica, cluster-wide and per
  service. Current numbers only: history, graphs and alerts belong in Grafana or
  in a module.
- **Roles and tokens** — see below.

SQLite holds only webhooks, deploy history, users, tokens and compose files.
Everything else lives in Swarm and is queried directly.

## Metrics across nodes

Docker has no cluster-wide stats API: a manager's socket only sees its own
containers. So the same binary also runs as a node agent — a `global` service
that reports its node and nothing else. The panel finds every replica through
`tasks.<agent service>` and merges what they send.

[stack.yml](stack.yml) already includes it. Two things it needs:

- `PANEL_MODULE_SECRET` must be **the same fixed value** for the panel and the
  agents (the per-start default cannot be shared), and
- `PANEL_AGENT_SERVICE` on the panel must name the agent service.

Leave `PANEL_AGENT_SERVICE` unset on a single node and the panel reads its own
socket, exactly as before — no agent to deploy.

The agent is the panel's own code, not a module: `docker.sock` never leaves our
binary (see ADR 3 in [docs/architecture.md](docs/architecture.md)). It is
read-only, listens only
inside the overlay network, and requires the shared-secret token.

## Roles

| | viewer | operator | admin |
| --- | --- | --- | --- |
| services, logs, history | ✔ | ✔ | ✔ |
| update a service, redeploy a stack | | ✔ | ✔ |
| module UIs | | ✔ | ✔ |
| Traefik, webhooks, users, tokens | | | ✔ |

CI tokens are created on the Users page and sent as
`Authorization: Bearer <token>`. A token's role can only be weaker than its
owner's, never stronger.

## Webhook from CI

Create a webhook in the UI, then call its URL after pushing an image:

```yaml
- run: curl -fsS -X POST https://panel.example.com/hook/${{ secrets.PANEL_HOOK }}
```

The request body is ignored — the secret in the URL is the authentication.

## Modules

A module is a separate service in Swarm, not a plugin inside the panel: it
crashes and upgrades independently, and can be written in any language. The
core discovers it by labels and talks to it over HTTP on the overlay network.

```text
panel.module=true
panel.module.name=hello
panel.module.version=0.1.0
panel.module.port=8080        # optional
```

A module answers `GET /manifest`:

```json
{"name":"hello","version":"0.1.0",
 "nav_entries":[{"title":"Hello","path":"/"}],
 "permissions":["service:read","service:update"]}
```

Modules get no access to `docker.sock` — they ask the core instead:

| | permission |
| --- | --- |
| `GET /internal/services` | `service:read` |
| `GET /internal/stats` | `stats:read` |
| `POST /internal/services/{name}/update` | `service:update` |

The core sends `X-Panel-Module` and `X-Panel-Token` (an HMAC of the module
name) with every proxied request; the module presents those headers back. The
permission must be declared in the manifest, otherwise the call gets a 403.

Writing one: [docs/modules.md](docs/modules.md). What exists:
[docs/catalog.md](docs/catalog.md). The bundled examples double as skeletons to
copy:

- [examples/hello-module](examples/hello-module) — manifest, page, one action
- [examples/metrics-module](examples/metrics-module) — CPU and memory per replica

## Development

```sh
go test ./...
```

The version shown in the UI and on `/healthz` is stamped at build time; a plain
`go build` says `dev`:

```sh
go build -ldflags "-X main.version=$(git describe --tags --always)"
docker build --build-arg VERSION=v1.2.3 -t modularnost:latest .
```

`GET /healthz` needs no authentication and answers `ok <version>` — that is what
an uptime check or a load balancer should ask.

Layout: [internal/docker](internal/docker) — the only place that touches the
Docker API; [internal/store](internal/store) — SQLite and versioned migrations;
[internal/web](internal/web) — handlers and HTML templates;
[internal/modules](internal/modules) — module discovery.

Project boundaries, the decisions behind them and the order of work live in
[docs/architecture.md](docs/architecture.md).

## License

[Apache License 2.0](LICENSE). Dependencies and their licenses are listed in
[THIRD-PARTY.md](THIRD-PARTY.md); attributions that other licenses require are
carried in [NOTICE](NOTICE).
