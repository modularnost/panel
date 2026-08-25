# Writing a module

A module is an ordinary Swarm service that the panel discovers by labels and
renders as one of its own pages. It is not a plugin inside the binary: it
crashes and upgrades separately, and can be written in any language as long as
it answers over HTTP.

The panel gives a module two things: a place in the menu, and the right to ask
the core to do something to Swarm. Modules never get direct access to
`docker.sock` — that boundary is what limits the damage a third-party module
can do.

## Quick start

Copy [examples/hello-module](../examples/hello-module) — everything you need is
there in ~120 lines. [examples/metrics-module](../examples/metrics-module) is
the other skeleton: it asks for a permission (`stats:read`) and calls the core
for data. Note that the panel already shows those numbers on its own Resources
page — that module is a demonstration, not a feature to install. Then:

```sh
docker build -t my-module:latest .
docker service create --name my_module \
  --network panelnet \
  --label panel.module=true \
  --label panel.module.name=my \
  --label panel.module.version=0.1.0 \
  --env PANEL_CORE_URL=http://panel:8080 \
  my-module:latest
```

The module shows up in the panel's menu within 15 seconds. If it doesn't, open
the Modules page — the reason is printed there instead of the `ok` status.

**The network is not optional.** The module must sit on the same overlay
network as the panel (`panelnet` in the examples), or neither side can reach
the other.

## The contract

### Service labels

| Label | | |
| --- | --- | --- |
| `panel.module` | `true` | required, or the module stays invisible |
| `panel.module.name` | `hello` | the name in the URL: `/modules/hello/` |
| `panel.module.version` | `0.1.0` | display only |
| `panel.module.port` | `8080` | optional, defaults to 8080 |

The core connects to `http://<swarm service name>:<port>`, resolved by Swarm's
DNS. Don't publish that port — from the outside the module is reachable only
through the panel, behind its authentication.

### `GET /manifest`

The only required endpoint. The core fetches it during discovery, every 15
seconds, with `X-Panel-Module` and `X-Panel-Token` set — that is how a module
receives its token, before any user request and again after the core restarts
with a new secret. Keep the latest one and use it to call back.

```json
{
  "name": "hello",
  "version": "0.1.0",
  "nav_entries": [{"title": "Hello", "path": "/"}],
  "permissions": ["service:read", "service:update"]
}
```

| Field | |
| --- | --- |
| `nav_entries` | menu items; `path` is appended to `/modules/<name>` |
| `permissions` | what the module may ask the core for, see below |
| `name`, `version` | informational; routing uses the label values |

If `/manifest` is missing or returns anything but 200, the module is listed as
broken with the error text and stays out of the menu.

### `GET /health`

The core does **not** poll it — it judges a module by its service replicas and
by the `/manifest` response. Add `/health` if you want a `HEALTHCHECK` in your
Dockerfile.

## What a user request looks like

```text
browser → GET /modules/hello/page   (panel checks for the operator role)
        → panel proxies: GET /page to http://hello_module:8080
```

Along the way the core:

- strips the `/modules/hello` prefix, so the module lives at its own `/`;
- adds the `X-Panel-Module` and `X-Panel-Token` headers;
- drops `Authorization` — the user's panel password never reaches the module.

The module renders the whole page itself, and the panel shows it in a frame
inside its own layout: sidebar on the left, your page on the right. The core
never parses or rewrites your HTML, so the styling is yours — and so is the
responsibility to return a **complete document** with a `<!doctype html>`.
Without it the browser falls into quirks mode and the panel cannot measure the
frame's height correctly.

Two things follow from living in a frame: your page is as wide as the content
area, not the whole window, and links inside it stay inside the frame (which is
what you want — the panel chrome stays put).

## Asking the core to act

The module identifies itself with the same headers the core sent it in the
proxied request:

```http
GET /internal/stats HTTP/1.1
Host: panel:8080
X-Panel-Module: metrics
X-Panel-Token: <whatever arrived in X-Panel-Token>
```

The token is an HMAC of the module name keyed with the core's secret. The core
stores nothing, and the token can't be forged without that secret.

| Endpoint | Permission | Returns |
| --- | --- | --- |
| `GET /internal/services` | `service:read` | services with their status |
| `GET /internal/stats` | `stats:read` | CPU, memory, network and block I/O counters per replica; `?service=<name>` narrows it |
| `POST /internal/services/{name}/update` | `service:update` | force-update, recorded in deploy history |

`/internal/stats` returns one object per replica:

```json
{"node":"worker-2","service":"api","stack":"shop","slot":1,
 "cpu":3.7,"mem":74190848,"mem_limit":2147483648,
 "net_rx":123456,"net_tx":654321,"block_read":789,"block_write":456}
```

`cpu` is percent of one core, the way `docker stats` reports it, so a busy
4-core replica reads above 100. `mem` excludes page cache. `mem_limit` is 0 when
the service sets no limit, and the node's total memory otherwise.

`net_rx`/`net_tx` and `block_read`/`block_write` are cumulative bytes since the
container started. Calculate a rate from two samples; a restarted container
resets its counters.

`node` is the node the replica runs on. A node whose agent cannot be reached
comes back as a single row with `err` set instead of numbers — render it, do not
drop it, or a broken node looks like an idle one. On a single-node install
without agents every row carries that node's own name.

Responses are JSON. Errors:

- **401** — no token, a bad token, or the module's service is gone from the
  cluster;
- **403** — valid token, but the permission isn't declared in the manifest.

Add `?service=<name>` to `/internal/stats` when you only care about one
service. The panel then samples that service alone instead of opening a stats
stream to every container in the cluster — on a large cluster that is the
difference between a fast page and a slow one. Each call takes about a second
either way: CPU percent needs two samples to produce a delta, so poll on a
timer, do not block your page render on it.

Ask for the minimum. The list is visible on the Modules page, and it's what a
cluster admin will judge your module by before installing it.

## Gotchas

**The token arrives with the manifest request, not at startup.** Remember it
from every request the core makes (see `remember()` in
[metrics-module](../examples/metrics-module/main.go)) — a module that only reads
it from page requests cannot work while nobody is watching.

**Discovery is cached for 15 seconds.** Changed your manifest? Wait.

**Changing `panel.module.name` changes both the URL and the token.** Existing
links break.

**The panel's stylesheet is available to you.** `/static/panel.css` is served
without authentication and your page is same-origin, so one `<link>` gives your
module the panel's look and its dark theme. It is not a stable API yet — class
names may change before the first release.

**A module handles its own authorization.** The core lets any `operator` into a
module's UI, so if your module exposes dangerous actions, it has to draw that
line itself — the core won't do it for you.

## Checklist before publishing

Modules live in their own repositories, with their own CI and their own image —
that is the point of them being separate services. When yours works, add a row
to [catalog.md](catalog.md) so people can find it.

- [ ] `/manifest` returns valid JSON and a 200
- [ ] `permissions` lists only what is actually used
- [ ] the port is not published outside the cluster
- [ ] the service is on the panel's overlay network
- [ ] the image is built by your CI, not from a laptop
- [ ] `X-Panel-Token` never reaches the logs
