# modularnost

Modularnost is a web panel for deploying and operating stacks and services in
Docker Swarm. The core deploys Compose files and ready-made images, shows
service status and logs, and handles redeploys. Install [modules](#modules)
when you need extra features such as image builds or metrics history.

## Installation

- [New server](#new-server): install Docker, Swarm and Traefik with the panel.
- [Existing Swarm](#existing-swarm): deploy the panel as a separate stack and
  use your existing reverse proxy.

### New server

One command on a fresh Ubuntu/Debian host with a domain pointed at it. It
installs Docker, initialises a one-node swarm, and brings up Traefik with a
Let's Encrypt certificate and the panel behind it:

```sh
curl -fsSL https://raw.githubusercontent.com/modularnost/panel/main/scripts/install.sh | sh
```

It asks for the domain and for an email for Let's Encrypt; an empty email skips
TLS and serves plain HTTP — for an internal network Let's Encrypt cannot reach.
The admin password is printed once at the end.

Run it as root.

Set the variables instead and it asks nothing, which is what an unattended run
needs:

```sh
curl -fsSL https://raw.githubusercontent.com/modularnost/panel/main/scripts/install.sh |
  DOMAIN=panel.example.com EMAIL=you@example.com sh
```

Re-running it reuses the Swarm, network and stack. The generated Compose file
and keys live in `/etc/modularnost`.

| Variable | Default | |
| --- | --- | --- |
| `DOMAIN` | asked | |
| `EMAIL` | asked | Let's Encrypt account; empty means plain HTTP |
| `PANEL_VERSION` | `latest` | image tag, see the table below |
| `PANEL_IMAGE` | `ghcr.io/modularnost/panel:$PANEL_VERSION` | your own build |
| `PANEL_STACK` | `panel` | stack name |
| `ADVERTISE_ADDR` | detected | swarm address, if the host has several |
| `FIREWALL` | `on` | `off` leaves the host's firewall as it was, see [Ports](#ports) |
| `FIREWALL_ALLOW` | | more ports to keep reachable, `10050/tcp,53/udp`; read on the first install only, later use the Ports page |

### Ports

The installer closes the host: from the internet only SSH, 80 and 443 answer,
and the swarm's own ports (2377, 7946, 4789 and ESP) answer only to the nodes of
the cluster. A small service on every node, `firewall`, keeps one iptables chain
of its own, `MODULARNOST`, in step with the panel; it holds `NET_ADMIN` and the
host's network and nothing else, and listens on nothing.

| Port | Open to | |
| --- | --- | --- |
| SSH (the ports `sshd` listens on, and 22) | everyone | never closed by this |
| 80, 443 | everyone | Traefik and the certificate challenge |
| 2377, 7946, 4789, ESP | the nodes of the cluster and servers allowed on the Nodes page | the swarm |
| what listened here before the install | everyone, as before | listed at the end of the install; copied to the Ports page once |
| a port opened on the **Ports** page | everyone | on every node |
| anything else, published ports included | nobody | |

What was there before stays reachable, so that an install does not break what a
server already did; the list is written down at the first install and then lives
on the Ports page, where it can be changed. `FIREWALL_ALLOW` adds to it.

**A port a service publishes is closed too.** Docker redirects it before the
host's firewall sees the packet, so a second chain, `MODULARNOST-FWD`, sits in
`DOCKER-USER` and drops what Docker redirected unless the port is open. A service
that publishes 5432 is therefore unreachable from outside until an admin opens
5432 on **Ports**, which lists every published port and whether it is open.
Containers reach each other and the outside as before. Prefer routing HTTP
through Traefik over publishing a port.

To add a server, open **Nodes**, **Add a server**, give its address and run the
command it shows on that server. The address is allowed for a day or until the
node appears in the swarm. Between servers of different providers a private
network or WireGuard (`ADVERTISE_ADDR`) is still the better way. The overlay
network is created encrypted, which costs nothing on one node. That needs the
kernel's IPsec (`esp4`); some images switch it off, and the installer then creates
the network plain and says so. Traffic between servers is then not encrypted: use a
private network or WireGuard (`ADVERTISE_ADDR`) there.

The rules live in the kernel: a stopped service leaves them in place, a reboot
clears them until the service is back (some seconds), and `uninstall.sh` removes
the chain on the node it runs on.

To remove it again — the swarm and other stacks are left alone:

```sh
sudo sh scripts/uninstall.sh            # keeps the database and certificates
sudo sh scripts/uninstall.sh --purge    # deletes them too
```

### Existing Swarm

For direct HTTP access over a trusted LAN or VPN, run on a manager as root:

```sh
curl -fsSL https://raw.githubusercontent.com/modularnost/panel/main/scripts/install.sh |
  PANEL_INSTALL_MODE=existing-swarm sh
```

The installer prints the address and initial password. It pins SQLite to
this manager and publishes port `8080` on this node. Set `PANEL_PORT` to
choose another port. The installer leaves existing stacks and firewall
settings alone. Limit access to your LAN or VPN.

Re-run the same command on the same manager to update. Use the original
`PANEL_STACK`, `PANEL_CONFIG` and `PANEL_PORT` if you set them. If the stack
name or configuration directory is occupied, choose a free one before the
first install. To remove the stack and keep its data, run
`docker stack rm <stack-name>`.

#### Manual deployment or an existing reverse proxy

Deploy [stack.yml](stack.yml) as a new stack from a manager or through
Portainer. Set `PANEL_ADMIN_PASSWORD` and a shared `PANEL_MODULE_SECRET`.
From a manager, run:

```sh
export PANEL_ADMIN_PASSWORD PANEL_MODULE_SECRET
docker stack deploy -c stack.yml panel
```

If you use an existing Traefik, remove the panel's `8080:8080` port mapping,
attach the panel to Traefik's overlay network and add a router for the panel's
hostname targeting container port `8080`. Keep the panel and agents on their
shared stack network. Set `PANEL_AGENT_SERVICE` to `<stack-name>_agent` if
the stack is named anything other than `panel`.

On a cluster with multiple managers, pin the panel to the manager that holds
its SQLite volume. To update a stack managed outside the panel, change the
image tag for the panel and agents in that stack's Compose and redeploy it.
Back up the database volume first. The panel's **Updates** page changes the
running services and can rewrite the installer-managed Compose when
`PANEL_STACK_FILE` is mounted. It cannot rewrite the Compose stored in
Portainer.

**Updates** shows the latest self-update, its Swarm rollout status and the
path of the pre-update database copy. If Swarm rolls the service back,
check whether the database needs restoring before retrying.

### Image tags and settings

The default image is `ghcr.io/modularnost/panel:latest` for amd64 and arm64.

| Tag | |
| --- | --- |
| `latest` | the last release; what you want on a server |
| `0.1.0`, `0.1` | a specific release, or the latest patch of a minor |
| `edge` | the last commit on `main`; for trying a fix before it is released |
| `sha-abc1234` | one exact commit |

To run your own build instead:

```sh
docker build -t modularnost:latest .
PANEL_IMAGE=modularnost:latest PANEL_ADMIN_PASSWORD=<password> \
  PANEL_MODULE_SECRET=<secret> docker stack deploy -c stack.yml panel
```

On a multi-node cluster the image must come from a registry every node can
reach — a locally built one exists only on the machine that built it, and the
agent will not start anywhere else.

The panel needs `/var/run/docker.sock` on a manager. A read-only mount
still permits Docker API calls. Keep the socket private.

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
| `PANEL_UPDATE_CHECK` | on | `off` stops the panel from asking GitHub for new releases |
| `PANEL_STACK_FILE` | unset | the installer's compose file, mounted into the panel: an update from the panel retags its image lines |
| `PANEL_REGISTRY_KEY_FILE` | unset | path to a file with a 32-byte hex key for saved private registry tokens |
| `PANEL_TRUSTED_PROXIES` | unset | addresses or CIDR ranges of the proxies in front of the panel, comma separated; see below |

Sign-in attempts are slowed down per client address and per user name: after five
wrong passwords the wait doubles, up to a minute, and the answer is `429` with
`Retry-After`. Behind a proxy the panel sees the proxy's address for every
client, so it reads the client from `X-Forwarded-For` only when the request
comes from an address in `PANEL_TRUSTED_PROXIES`; the installer sets it to its
own network. For HTTPS terminated by a proxy, configure the same setting so
the panel uses `X-Forwarded-Proto: https` to set secure session cookies.
Do not put a range there that clients can reach the panel from.

Expired sessions and API tokens are cleaned up at startup and every six hours.

### Webhooks

Copy the URL when you create a webhook and send a `POST` to trigger it.
The panel stores a hash and returns the secret path only at creation.
Existing webhook URLs keep working after upgrade. Older database backups and
proxy access logs can contain usable URLs, so protect those files and replace
any webhook whose URL was exposed. The hook endpoint allows 60 requests per
minute per client IP and returns `429` with `Retry-After` above that limit.

### Private registry images

In **Settings → Registries**, save a pull token for the registry hostname
(for example `ghcr.io` or `registry.example.net:5000`). Select that registry
when deploying a stack or installing a module, and use image references
from the same host. A stack can select one registry and still use public
images. Deploys and webhooks reuse the saved selection. Paste new credentials
under the same host to rotate the token.

The installer creates `/etc/modularnost/registry-key` on install or re-run.
For a manual installation, generate a key with `openssl rand -hex 32`,
mount the file into the panel and set `PANEL_REGISTRY_KEY_FILE`. Back up
the key separately from SQLite. Both are needed to restore saved tokens.
Use HTTPS for the panel and registry. Removing credentials in the panel
does not revoke tokens already passed to Swarm. Revoke or rotate the token
at the registry when removing access.

## What it does

- **Services** — stacks, replicas, image and digest. Read live from the Docker
  API and never mirrored into the database, so the UI can't lie about the
  cluster. Each service has its own page: overview, logs, deploy history,
  Traefik labels and webhooks.
- **Update** — force-update that re-pulls the image from the registry, so a CI
  push is enough to roll a service forward.
- **Stacks** — deploy, edit and redeploy saved Compose files. Browse services
  in existing stacks and see the last deployment recorded by the panel and
  the latest Swarm service update.
- **Logs** — SSE stream, filter by replica, pause. Lines from different
  replicas are tagged with their slot: `[1]`, `[2]`.
- **Traefik** — a form instead of hand-editing labels. The panel only writes
  labels; Traefik picks them up on its own.
- **Webhooks** — `POST /hook/<secret>` from CI.
- **Deploy history** — the one thing Swarm itself doesn't keep.
- **Resources** — current CPU and memory of every replica, cluster-wide and per
  service. Current numbers only: history, graphs and alerts belong in Grafana or
  in a module.
- **Roles and tokens** — see below.

SQLite holds panel-owned data such as Compose files, deploy history, users,
webhooks, firewall settings and encrypted registry credentials.

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

The built-in agent reads local Docker stats. Its endpoint stays on the
overlay network and requires a token derived from the shared secret.

## Roles

| | viewer | operator | admin |
| --- | --- | --- | --- |
| services, logs, history | ✔ | ✔ | ✔ |
| update a service, redeploy a stack | | ✔ | ✔ |
| Traefik, webhooks, users, tokens | | | ✔ |

Module access depends on each contribution's minimum role.

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
 "contract":3,"contributions":[{"slot":"nav","title":"Hello","path":"/"}],
 "permissions":["service:read","service:update"]}
```

Modules call the core with the permissions declared in their manifests.
They have no direct access to the Docker socket.

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
[internal/web](internal/web) — HTTP handlers and the embedded UI entry point;
[internal/modules](internal/modules) — module discovery.

Project scope and design decisions live in
[docs/architecture.md](docs/architecture.md); how to contribute is in
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache License 2.0](LICENSE). Dependencies and their licenses are listed in
[THIRD-PARTY.md](THIRD-PARTY.md); attributions that other licenses require are
carried in [NOTICE](NOTICE).
