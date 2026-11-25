# Module catalogue

Administrators can browse the curated
[module catalog](https://github.com/modularnost/module-catalog) under
**Modules → Catalog**. A module is an ordinary Swarm service. See
[modules.md](modules.md) for the module contract and manual installation.

Open **Module details** on a catalog card to review its maintainer, catalog
version, permissions, source, and documentation. On the **Installed** tab, the
same control shows the installed version, Swarm service, and permissions. A
ready module with a menu page has an **Open** action. Administrators see the
catalog description on both tabs when the installed module is listed there.

The catalog offers installation for supported Swarm services and stacks and
links to guides for manual entries. The panel skips entries that use an installation
kind it does not support so administrators can still install other modules.
Installed modules are discovered from service labels in the cluster.

Administrators can run preflight for a `swarm_stack` entry through the API. The
panel finds its Swarm overlay, validates the bundle manifest and file paths
from the approved image digest, and checks any registry credential requirement
declared by the bundle. The preflight response reports the panel's overlay
network.

The catalog stack entry point is a service named `module` listening on port
`8080`. The panel assigns the approved catalog image to that service, joins it
to the panel overlay, and waits for all services and the module manifest. Sidecar images use SHA-256
digests. Supported service settings are `image`, `command`, `entrypoint`,
`environment` with literal values, and `healthcheck`.

## Builder registry

An administrator can provision the registry and its pull proxy for an approved
Builder stack before deploying Builder. Configure encrypted registry credentials
on the panel. Create an internal Swarm overlay for build traffic and a separate
private overlay for the pull route. Supply the Builder bundle's external TLS
and bcrypt-user secrets using the
[Builder installation guide](https://github.com/modularnost/builder_module/blob/main/docs/install.md).
The panel installs the bundled mTLS registry and the TLS pull proxy. Both stay
on the original manager, and the registry uses the named volume
`modularnost-builder-registry_data`.

Use `PUT /api/v1/module-catalog/builder/registry` with the catalog `version` and
`revision`, the exact pull `endpoint` (host with optional port), and the
`build_network` and `pull_network` names. For Traefik's Swarm provider, supply
the private `pull_entrypoint` and the panel configures an SNI route with TLS
passthrough. With an external router, configure TCP passthrough to
`builder-registry-pull:443` on the pull network. Set up private DNS to reach the
chosen router. The registry and proxy publish no host ports. Install the pull
CA for the exact endpoint on each eligible Docker daemon if its certificate
uses a private CA.

Seed a small, prebuilt image through the internal mTLS registry. Use
`POST /api/v1/module-catalog/builder/registry/verify` with the same fields plus
the `worker` node ID, the digest-pinned `probe_image` on that endpoint, and
`username` and `token`. Choose a clean active worker with a panel agent for the
first check. Repeat the check on eligible workers to confirm their routing and
trust. The worker must see an authentication failure without a credential and
success with it. Only then does the panel save the encrypted pull credential
for the exact endpoint. Retry provisioning with the original inputs after a
failed attempt. A retry preserves existing services and registry data.

## Bundled examples

These ship in the panel's own repository because they are documentation: they
are tested against the current core and updated in the same commit whenever the
module contract changes.

| Module | Permissions | What it does |
| --- | --- | --- |
| [hello](../examples/hello-module) | `service:read`, `service:update` | The smallest working module: manifest, one page, one action |
| [metrics](../examples/metrics-module) | `stats:read` | Demonstrates `stats:read`. The panel already shows these numbers on its Resources page — copy this as a skeleton, do not install it expecting a feature |

## Community modules

| Module | Repository | Image | Requires | Permissions | Status |
| --- | --- | --- | --- | --- | --- |
| Metrics | [modularnost/metrics_module](https://github.com/modularnost/metrics_module) | `ghcr.io/modularnost/metrics-module:edge` (rolling) | panel with contract 3 | `stats:read` | community |
| Builder | [modularnost/builder_module](https://github.com/modularnost/builder_module) | Build from source | panel with contract 3, `service:create`, and the `code` block | `service:read`, `service:create` | community |

## Adding your module

For inclusion in the panel's Catalog, open a pull request in
[modularnost/module-catalog](https://github.com/modularnost/module-catalog).
Its README documents the catalog schema, the approved image digest, and the
option to link to a manual installation guide.

When publishing a module:

- the repository is public and the image is published somewhere anyone can pull
  from;
- the `README` says what the module does and which permissions it asks for;
- `permissions` in the catalog entry match the module's manifest exactly. Administrators see those permissions before installing.

Keep the description to one line. This is an index, not a showcase.

## `community` vs `verified`

Every new entry starts as **community**. The manifest sets its permissions
and the minimum role for each UI contribution.

**verified** is for a module maintained by Modularnost or whose source has been
reviewed by its maintainers. Use a release tag; use an image digest for
third-party modules whenever one is available.

## Before you install anything

A module you install is code running inside your cluster. Check:

- **the permissions it asks for.** They are visible on the panel's Modules page
  after installation, and should be in its README before. A metrics module that
  wants `service:update` is asking for more than its job needs;
- **who publishes the image**, and whether the tag is pinned. A moving `:latest`
  from a stranger means whatever they push next runs on your servers;
- **that the port is not published.** A module belongs on the overlay network,
  reachable only through the panel.

A module never gets `docker.sock`. Everything it can do to the cluster goes
through the core's internal API, limited to the permissions in its manifest —
which is what makes reading that list worth the minute it takes.

## Swarm Secrets

Administrators can list metadata and attachments at `GET /api/v1/secrets`
and create an immutable version with `POST /api/v1/secrets`. Send a name and
base64-encoded `data`; the response contains the ID. Secret bytes are accepted
only when creating a version.

Use `PUT /api/v1/services/{id}/secrets` with a `target` filename under
`/run/secrets` and a `resource` name or ID. To rotate, create a new
version and set the same target to its ID on each consumer. Omit `resource`
to detach. `DELETE /api/v1/secrets/{id}` requires the secret to be unused.

For a catalog stack, the digest-pinned bundle contains Compose files with
a service named `module`. Declare secret aliases under top-level `secrets`
with `external: true` and reference them only on the services that need them.
In the confirmed catalog install request, send a `secrets` mapping keyed by
those aliases. For each alias give either `{"existing":"secret-id-or-name"}`
or `{"create":{"name":"new-version","data":"base64-value"}}`. Docker mounts
the selected secret. The panel sends only references to Compose and removes
unused secrets it created if deployment fails. Uninstall leaves secrets
available for explicit deletion.

## Named local volumes

List local volumes and service attachments at `GET /api/v1/nodes/{node}/volumes`.
Inspect, create or explicitly delete a volume by name on that node using
`GET`, `PUT` or `DELETE /api/v1/nodes/{node}/volumes/{name}`. Deletion
requires a volume with no service or container consumers.

Mount an existing volume using `POST /api/v1/services/{id}/volumes` with
`node`, `name`, an absolute `target`, and optional `read_only`. The panel
pins the service to the selected node to keep the same data after redeploy.
A worker must have a reachable panel agent to manage its local volumes.

A catalog stack declares aliases under top-level `volumes`, for example
`volumes: {data: {}}`. Attach the alias using the long form
`{type: volume, source: data, target: /data}`. The confirmed install can
include `volumes: {"data":{"node":"node-id","name":"existing-data"}}`.
Omit the volume name to provision the catalog default on that node. The same
service must use volumes on the same node. For a single-service catalog
entry with persistence, use the singular `volume` selection.

Subsequent installs reuse the recorded node and volume name. If a recorded
node is unavailable or its volume is missing, installation stops. Volumes
survive uninstall and failed installation and can be deleted explicitly once
unused.

## Swarm Configs

Administrators can list configs at `GET /api/v1/configs`, inspect a config at
`GET /api/v1/configs/{id}`, and create an immutable version with
`POST /api/v1/configs`. Creation accepts a name and base64-encoded `data`.
Inspecting a config returns its contents and consumer services.

Use `PUT /api/v1/services/{id}/configs` with an absolute container `target`
and the `resource` name or ID. Set the same target to a new version to swap
the config. Omit `resource` to detach it. Use `DELETE /api/v1/configs/{id}`
after all consumers have detached.

A catalog bundle can declare `configs: {caddy: {file: assets/Caddyfile}}`
and use `configs: [{source: caddy, target: /etc/caddy/Caddyfile}]` on a
service. Paths are relative to the root of the approved bundle. The panel
creates a version from that file and passes only its reference to Compose.
On an unsuccessful install it removes unused configs created by the attempt.
