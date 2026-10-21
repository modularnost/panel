# Writing a module

A module is an ordinary Swarm service that the panel discovers by labels and
renders as one of its own pages. It is not a plugin inside the binary: it
crashes and upgrades separately, and can be written in any language as long as
it answers over HTTP.

The panel gives a module two things: places in its own screens (a menu entry,
a tab on a service, an action on the services list), and the right to ask the
core to do something to Swarm. Modules never get direct access to
`docker.sock` — that boundary is what limits the damage a third-party module
can do.

## Quick start

Start from [examples/metrics-module](../examples/metrics-module): a `ui` page,
the only kind there is. It sends blocks (numbers, a table,
charts with a range to pick), asks for a permission (`stats:read`), samples the
core on a timer and tells the panel when its pages changed. Note that the panel
already shows those numbers on its own Resources page — that module is a
demonstration, not a feature to install.
[examples/hello-module](../examples/hello-module) is the smaller one: a page
with a form whose action asks the core to redeploy a service. Then:

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
| `panel.module.name` | `hello` | the name of the module; changing it changes the token too |
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
  "contract": 3,
  "name": "hello",
  "version": "0.1.0",
  "contributions": [
    {"slot": "nav", "title": "Hello", "path": "/", "role": "viewer"}
  ],
  "permissions": ["service:read", "service:update"]
}
```

| Field | |
| --- | --- |
| `contract` | required, `3`. A manifest without it, or with another number, makes the module show as failed |
| `contributions` | what the module adds to the panel, see below |
| `permissions` | what the module may ask the core for, see below |
| `name`, `version` | informational; routing uses the label values |

If `/manifest` is missing, returns anything but 200, or does not pass the
checks above, the module is listed as broken with the error text and stays out
of the menu.

### Contributions

A contribution is one thing the module puts into the panel. Every one has a
`slot` (where), a `title` (what the user reads), a `path` (the page of yours
it asks for), and optionally a `role`.

| Slot | Where it appears |
| --- | --- |
| `nav` | an entry in the side menu |
| `service.tab` | a tab on a service's page; `match` (labels the service must all carry) narrows it to the services that are yours |
| `services.action` | an action on the services list |

The panel shows the page: `nav` and `services.action` open it as a screen,
`service.tab` shows it inside the service's page and passes the service as
`?service=<name>&stack=<stack>&id=<id>`. A slot the panel does not know is
ignored, so a manifest written for a newer panel still loads on an older one.
The context is a convenience, not proof: anybody can write that query, so a
module that acts on it checks the caller's role (the headers below) like on any
other request.

`role` is the lowest role that sees the contribution **and may reach its path**;
it defaults to `operator`. The panel checks it on every request to the module,
by the longest declared `path` that is a prefix of the request path (at a
`/` boundary). So `{"path": "/", "role": "viewer"}` opens the whole module to
viewers, and `{"path": "/settings", "role": "admin"}` keeps `/settings` for
admins. A path no contribution covers needs `operator`.

### `GET /health`

The core does **not** poll it — it judges a module by its service replicas and
by the `/manifest` response. Add `/health` if you want a `HEALTHCHECK` in your
Dockerfile.

## UI pages

A contribution answers `GET <path>` with JSON, a document of blocks the panel
draws, in its own look, theme and phone layout:

```json
{"ui": 1, "blocks": [
  {"type": "choice", "param": "range", "value": "6h",
   "options": [{"value": "1h", "label": "1h"}, {"value": "6h", "label": "6h"}]},
  {"type": "chart", "title": "CPU", "unit": "percent", "sync": "time",
   "from": 1760500000, "to": 1760521600,
   "series": [{"name": "CPU", "points": [[1760500000, 3.1], [1760500600, 4.0]]}]},
  {"type": "table",
   "columns": [{"key": "name", "title": "Service"}, {"key": "mem", "title": "Memory", "type": "bytes"}],
   "rows": [{"name": {"text": "shop_web", "href": {"service": "shop_web"}}, "mem": 74190848}]}
]}
```

The panel asks as the signed-in viewer (the headers below) and only for a path
the manifest declared; the role of the contribution applies.
For a tab the context is in the query (`?service=<name>`), and
so is what a `choice` block set (`?range=24h`): the panel keeps it in its own
URL and asks again when it changes.

`ui` is the version of the vocabulary: `1`. A block the panel does not know is
shown as unsupported and the rest of the page still draws, so a module may use
blocks of a newer panel on an older one. Text is only text: no HTML, no styles,
no scripts. A document is refused over 1 MiB, 200 blocks, 8 series in a chart or
2000 points in a series.

| Block | Fields |
| --- | --- |
| `heading` | `text`, `level` (`1` or `2`, default `2`) |
| `text` | `text`, `tone` (`muted`) |
| `banner` | `text`, `tone` (`info`, `warning`, `error`; default `info`) |
| `code` | `text` kept as written in a monospace box that scrolls, e.g. a build log; `title` |
| `stat` | `label`, `value`, `unit`, `caption` |
| `facts` | `items`: a list of `{label, value, unit}` |
| `table` | `columns`: `{key, title, type, align}`; `rows`: an object per row, keyed by column; `empty`: the text of an empty table |
| `meter` | `label`, `value`, `max`, `unit`, `tone` (`ok`, `bad`) |
| `chart` | `title`, `unit`, `from`, `to` (unix seconds), `series`: `{name, points: [[time, value], ...]}`, `sync`, `empty` |
| `choice` | `param`, `value`, `options`: `{value, label}`; picking one asks for the page again with `?<param>=<value>` |
| `link` | `text`, `href` |
| `button` | `label`, `action` (a path below `/action/`), `values` (fixed strings), `confirm` (a question the panel asks first), `tone` (`danger`) |
| `form` | `fields`, `action`, `submit` (the button's text), `confirm`; a field is `{name, label, type, value, options}` with `type` `text` (default), `select` (`options`: `{value, label}`) or `switch` |

A value is a string or a number, and a table cell may also be
`{"text": "...", "href": ..., "tone": "ok"}`. A column `type` is `text` (the
default), `number`, `bytes`, `percent`, `time` (unix seconds, shown in the
viewer's time zone) or `chip`. `unit` is `percent`, `bytes`, `bytes/s`, `count`
or `seconds`; the panel formats the number, so send it raw. Charts with the same
`sync` share one time axis and one cursor.

An `href` is one of `{"path": "/service?name=x"}` (a page of the module),
`{"service": "shop_web"}` (the panel's page of that service) or
`{"url": "https://..."}`. Nothing else is a link.

### Actions

A `button` or a `form` makes the panel send `POST <action>` to the module, as
the signed-in viewer, with the fixed `values` of the button or the fields of the
form as a JSON object of strings (a switch is `"true"` or `"false"`):

```http
POST /action/redeploy HTTP/1.1
Content-Type: application/json
X-Panel-User: ann
...

{"service": "shop_web"}
```

Answer `2xx`, optionally with `{"message": "redeployed"}`, which the panel shows
under the block; anything else is shown as the error, with the body as the
reason (up to 4 KiB). The panel then asks for the page again. The role is the
one of the longest declared `path` that covers the action, as for a page: give a
dangerous action its own `path` in the manifest with a higher `role`. An action
outside `/action/` is refused, and so is one from a page the module did not
declare as `ui`.

### Telling the panel a page changed

The panel does not poll. When what a page shows changes, say so:

```http
POST /internal/ui/changed HTTP/1.1
Host: panel:8080
X-Panel-Module: metrics
X-Panel-Token: <your token>
Content-Type: application/json

{"path": "/service"}
```

It answers `204`. Every browser that has that page open asks for it again, at
most once a second, and as its own viewer. No permission is needed, but the path
must be a `ui` contribution of the module that is calling: anything else is
`404`. An event says "ask again", not what changed. Nobody looking costs
nothing, so report after every update.

## Who is looking

Every request the panel makes to the module, for a page or for an action, carries
`X-Panel-Module`, `X-Panel-Token` and who is looking, signed:

```http
X-Panel-User: ann
X-Panel-Role: viewer
X-Panel-Time: 1792000000
X-Panel-User-Sig: 3f9c…
```

The signature is an HMAC-SHA256, keyed with the module's own token (the one it
already holds), over `v1\n<user>\n<role>\n<time>\n<module name>`, hex-encoded.
A module that makes decisions by user or role verifies it and, if it cares
about replay, the time:

```go
mac := hmac.New(sha256.New, []byte(token))
fmt.Fprintf(mac, "v1\n%s\n%s\n%s\n%s", user, role, ts, moduleName)
ok := hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig))
```

The core already refused a request below the contribution's `role`; the headers
are for a module that wants finer lines than that.

## Asking the core to act

The module identifies itself with the same headers the core sent it:

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
| `POST /internal/services` | `service:create` | creates a service from `{name, image, env, labels, replicas}`; `201` |
| `POST /internal/services/{name}/image` | `service:create` | moves a service **this module created** to the tag `{"tag": "..."}`, new task first; `403` for any other |

A service made through `POST /internal/services` joins the panel's network and
publishes no port: route traffic to it with Traefik labels. Labels that start
with `panel.` are refused, and the core adds `panel.owner=<module>` itself, which
is what lets only that module move the service to another image. Both calls are
recorded in deploy history as `module:<name>`.

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

**The core enforces the role of the contribution, not what your page does.**
It lets a user in when their role is at least the one of the longest matching
`path`, and everything else needs `operator`. If one page exposes both a view
and a dangerous action, give the action its own path with a higher `role`, or
check `X-Panel-Role` yourself.

## Checklist before publishing

Modules live in their own repositories, with their own CI and their own image —
that is the point of them being separate services. When yours works, add a row
to [catalog.md](catalog.md) so people can find it.

- [ ] `/manifest` returns valid JSON and a 200, with `"contract": 3`
- [ ] a page answers `{"ui": 1, "blocks": [...]}`; there is no HTML
- [ ] every contribution has the lowest `role` that is safe, and dangerous
      paths are covered by a higher one
- [ ] `permissions` lists only what is actually used
- [ ] the port is not published outside the cluster
- [ ] the service is on the panel's overlay network
- [ ] the image is built by your CI, not from a laptop
- [ ] `X-Panel-Token` never reaches the logs
