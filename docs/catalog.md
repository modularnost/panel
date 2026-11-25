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

The catalog offers installation for supported Swarm service entries and links
to guides for manual entries. The panel skips entries that use an installation
kind it does not support so administrators can still install other modules.
Installed modules are discovered from service labels in the cluster.

For approved Builder stack entries, administrators can run installation
preflight through the API. It checks registry encryption, finds the panel's
Swarm overlay and reads the deployment bundle from the catalog image digest.
It reports these prerequisites before any components are deployed.

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
