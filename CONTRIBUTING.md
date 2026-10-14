# Contributing

Read [docs/architecture.md](docs/architecture.md) before proposing a feature: it
lists what the project deliberately does not do.

## Workflow

```
issue → branch from main → PR → green CI → squash merge → edge → check on a VPS
```

- Work starts from an issue. A typo or a docs fix may skip it.
- Branch `type/short-name`, with the same type as the commit: `fix/login-redirect`.
- One PR, one task; aim for under 300 changed lines.
- `main` takes no direct pushes. CI must pass and the PR is squash-merged, so
  the PR title becomes the commit. Close the issue with `Closes #N`.
- To undo a change, `git revert` it in a new PR; `main` is never rewritten.

## Commits and PR titles

`type(scope): subject`

- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `ci`, `chore`, `perf`.
  `!` after the type marks a breaking change.
- Scope: the area, e.g. `docker`, `web`, `store`, `agent`, `modules`, `install`.
- English, imperative, up to 72 characters, no full stop. No body unless the
  reason is not obvious from the subject. No attribution trailers.

CI rejects a PR whose title does not match.

## Code

- `gofmt`, `go vet` and `golangci-lint` ([.golangci.yml](.golangci.yml)) run in CI.
- Write code that needs no comments: rename or split first. Comment only a
  reason the code cannot show, in at most three lines. No section banners, no
  restating a name, no TODO notes: open an issue with the `tech-debt` label.
- Wrap errors with context (`%w`); no `panic` outside `main` and package init.
  Never log secrets.
- Tests use the standard `testing` package, table-driven. Branching logic and
  security checks need a test; a bug fix starts with a test that reproduces it.
  There is no coverage threshold.
- Standard library first. A new dependency needs a reason in the PR, and
  `THIRD-PARTY.md` is regenerated with `scripts/licenses.sh`.
- JSON field and parameter names are `snake_case` everywhere; a test enforces it.
- A change to `/api/v1` regenerates the spec: `UPDATE_OPENAPI=1 go test
  ./internal/web`. CI fails when `api/openapi.json` is stale.
- The frontend is in `web/` (Bun, Vite, React, TypeScript). `bun install` and
  `bun run dev` there, with the panel running on :8080; the API types are
  generated from `api/openapi.json` on every build and are not committed.
- Docker only through `internal/docker`, the database only through
  `internal/store`. A migration is a new file; an applied one is never edited.
- Interface text rules are in [docs/architecture.md](docs/architecture.md).

## Done means

- CI is green.
- Branching logic has a test.
- A user-visible change is in the README or `docs/`; a new environment variable
  is in the README table.
- The change is checked against the non-goals in `docs/architecture.md`.

## Changing the architecture

Open an issue first. A change to the decisions, the feature order or the
non-goals reaches `main` only with the maintainer's explicit approval. For a
large feature, the issue says what is built, what is not, and how it is checked.

## Releases

Only the maintainer tags. `v0.Y.0` brings features or breaking changes,
`v0.Y.Z` only fixes; `1.0.0` comes when the module contract, the schema and the
upgrade path are fixed and tried on a multi-node cluster.

Before a tag:

1. `main` is green and `edge` runs on the test VPS.
2. A clean install with `scripts/install.sh` works and prints the admin password.
3. Upgrading an existing database from the previous release keeps its data.
4. Sign-in, the service list, one module and one webhook work.
5. A migration or a breaking change is called out in the release, using
   [docs/release-template.md](docs/release-template.md).

`git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z` publishes `X.Y.Z`,
`X.Y` and `latest` and creates the GitHub release, which is the changelog. A tag
is never moved or deleted: the fix is the next patch release. Migrations only go
forward, so rolling back after one means restoring a copy of the database.
