# CLAUDE.md

The architecture, the decisions behind it and the rules for code and interface
text live in [docs/architecture.md](docs/architecture.md). The workflow,
commit format and release process are in [CONTRIBUTING.md](CONTRIBUTING.md).
Read both and follow them — one source of truth instead of two that drift.

Rules for assistants:

- Work from an issue or an explicit request. Nothing "while we're here".
- Always a branch and a PR. Never push to `main`, never merge, never change
  repository settings, rulesets or tokens, never create tags or releases.
- Ask the maintainer before changing scope or architecture: the decisions, the
  feature order, the non-goals.
- Ask for a merge only when CI is green.
- No `Co-Authored-By` or "Generated with" lines in commits or PR descriptions.
- Comments only for a reason the code cannot show, in at most three lines.
