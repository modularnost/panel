# Release notes template

The release is created by CI with generated notes: a list of merged pull
requests. The panel shows the release body as the changelog on its Updates page,
so write the part above that list by hand right after the tag is pushed, with
`gh release edit vX.Y.Z --notes-file <file>`, and keep the generated
`## What's Changed` list below it.

Delete a section that has nothing to say. Do not leave it empty.

```markdown
## Highlights

- What a user can do now that they could not before, one line each.

## Breaking changes

- A URL, an environment variable, a label or an API field that changed or
  went away, and what to use instead.

## Upgrading

- Migrations only go forward: take a copy of the database volume first.
- Anything the user must do by hand: re-run `scripts/install.sh`, a new
  variable, a placement constraint.

## What's Changed
<the generated list>
```

`Breaking changes` is checklist item 5 in [CONTRIBUTING.md](../CONTRIBUTING.md):
a release with a migration or a breaking change does not go out without it.
