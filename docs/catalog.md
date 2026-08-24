# Module catalogue

Known modules for the panel. A module is an ordinary Swarm service — see
[modules.md](modules.md) for what one is and how to write your own.

Nothing here is installed automatically. The panel does not read this file: it
discovers whatever is running in your cluster by labels, and this page only
answers "what exists".

## Bundled examples

These ship in the panel's own repository because they are documentation: they
are tested against the current core and updated in the same commit whenever the
module contract changes.

| Module | Permissions | What it does |
| --- | --- | --- |
| [hello](../examples/hello-module) | `service:read`, `service:update` | The smallest working module: manifest, one page, one action |
| [metrics](../examples/metrics-module) | `stats:read` | Demonstrates `stats:read`. The panel already shows these numbers on its Resources page — copy this as a skeleton, do not install it expecting a feature |

## Community modules

None yet. Open a pull request adding a row.

| Module | Repository | Image | Permissions | Status |
| --- | --- | --- | --- | --- |
| — | — | — | — | — |

## Adding your module

Send a pull request that adds one row. Requirements:

- the repository is public and the image is published somewhere anyone can pull
  from;
- the `README` says what the module does and which permissions it asks for;
- `permissions` in the row match the module's manifest exactly — that column is
  what an administrator decides by.

Keep the description to one line. This is an index, not a showcase.

## `community` vs `verified`

Every new entry is **community**: listed, not endorsed. Nobody has read the
code, and installing it means giving that module the permissions it asks for
plus a UI reachable by any `operator`.

**verified** is only set after someone has actually read the source and the
entry pins an image digest, not a moving tag. Until we have the capacity to do
that review honestly, the column stays `community` — a badge that means nothing
is worse than no badge.

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
