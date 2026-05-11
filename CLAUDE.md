# CLAUDE.md — em-dee

Operating contract for Claude Code sessions working in this repository.
This file is read by the harness on every session and is the source of
truth for how to behave here.

## Operating principles

These are load-bearing. Every change in this repo is made under them.

1. **Don't assume. Don't hide confusion. Surface tradeoffs.**
2. **Minimum code that solves the problem. Nothing speculative.**
3. **Touch only what you must. Clean up only your own mess.**
4. **Define success criteria. Loop until verified.**

## What this repo is

em-dee is a Go CLI that generates `CLAUDE.md` files for new projects
from a curated, embedded catalog of opinionated markdown blocks.

## Mechanical recipes

The templates filesystem lives at `internal/registry/templates/`
(inside the registry package because `//go:embed` cannot escape its
package directory). Every category folder is named `NN-<id>` with a
two-digit numeric prefix; that prefix dictates render order.

- **Add an option** to an existing category: drop
  `internal/registry/templates/<NN-cat>/<id>.md`, append one entry to
  that folder's `_index.yaml` (`{id, display_name, description, file}`),
  run `task verify`. The manifest hygiene validator will fail loudly
  if the `.md` file is missing, the option id collides, the option
  id isn't kebab, the file is zero bytes, or the `_index.yaml` is
  malformed.
- **Add a new top-level category**: `mkdir
  internal/registry/templates/<NN-name>/`, create `_index.yaml` with
  `display_name`, `pick: single|multi`, optional `default`, and at
  least one `options` entry. Add the `.md` files. Run `task verify`.
- **Add a new language**: `mkdir
  internal/registry/templates/10-language/<id>/`, create `<id>/base.md`,
  add the language to `internal/registry/templates/10-language/_index.yaml`
  with `file: <id>/base.md`, add nested sub-categories using the
  top-level-category recipe. Note: the language id must not collide
  with any top-level category id (the validator enforces this).
- **Reorder categories**: change the folder's `NN-` prefix. Do NOT
  edit `options` list order to reorder.
- **Update render output for a changed template**: edit the `.md`,
  run `task golden-update` locally, inspect the diff in
  `testdata/golden/*/expected.md` carefully, commit both the template
  and the regenerated golden together.

## Anti-patterns

- **No frontmatter** in `.md` block files. Metadata lives in
  `_index.yaml`; duplicating it in frontmatter is forbidden.
- **No cross-category constraint rules in code.** The picker
  soft-trusts the user. Coupling-by-validation is what the spec
  rejects; do not reintroduce it.
- **No reordering by editing `_index.yaml` `options` lists.** Change
  the folder's `NN-` prefix instead.
- **No language-specific content in cross-cutting blocks.** A
  Python-specific Dockerfile in `20-infra/` is the wrong shape. Move
  it under the language subtree if it belongs there at all.
- **Never run `task golden-update` to fix a failing CI test.** Run
  it only after intentional template or render-logic changes,
  locally, with the diff inspected before committing. The fixtures
  are the regression net; blindly regenerating them removes the net.
- **No language-id ↔ top-level-category-id collisions.** The `show`
  resolver depends on disambiguation; the validator enforces it.
- **No zero-byte `.md` block files.** Validator rejects them.
- **No `--no-verify`, no force-push to `main`, no `--no-edit` on
  rebase.**

## Git workflow

- **`main` is always green.** Every change goes through a PR with
  review before merge; `.github/workflows/ci.yml` enforces it.
- **Every change lives on a feature branch.** Naming is required:
  - `feat/<short-slug>` — new functionality.
  - `fix/<short-slug>` — bug fix.
  - `chore/<short-slug>` — tooling, config, deps, docs, anything
    non-functional.
  - `refactor/<short-slug>` — internal restructuring with no external
    behaviour change.
  - `style/<short-slug>` — formatting, comments, naming only.
- One branch per logical chunk of work.
- **PR review** by the `trackness-agents:pr-reviewer` subagent before
  merge. Every review comment is addressed on the branch — either
  the fix is pushed, or a reasoned reply is posted explaining why
  the suggestion is YAGNI / out-of-scope. No dismissal.
- **Squash merge** to keep `main` history one commit per logical
  change. Feature branches preserved on origin (`--delete-branch=false`).
- Use `gh` for GitHub-side operations (PR open, PR review, PR merge,
  releases, branch protection, API queries). Reserve `git` for
  local-only operations.

## Required commands

- `task verify` before every commit. Runs `gofmt`, `go vet`,
  `go test ./...`, manifest hygiene as part of the test suite. CI
  runs the same on push/PR via `.github/workflows/ci.yml` (matrix:
  `stable` + `oldstable`; parallel `golangci-lint` job using
  `.golangci.yml`).
- `task build` for a local binary in `bin/em-dee`.
- `task golden-update` to regenerate render fixtures (read the
  anti-patterns first).
- `task release` runs `goreleaser release --snapshot --clean
  --skip=publish` — local snapshot build only, never publishes. Real
  releases happen via `.github/workflows/release.yml` on a `v*` tag
  push.

## Cutting a release

Releases are tag-driven. To cut one:

1. Ensure `main` is at the desired tip and CI is green.
2. `git tag v<MAJOR>.<MINOR>.<PATCH>` on `main`, push the tag.
3. `.github/workflows/release.yml` triggers, runs `goreleaser
   release --clean`, attaches archives + `checksums.txt` to a
   GitHub Release.
4. The archive `name_template` is `em-dee_<os>_<arch>` (version
   omitted) so `releases/latest/download/em-dee_<os>_<arch>.<ext>`
   resolves predictably — `em-dee update` depends on this URL shape.

No manual `goreleaser release` from a local machine for real
releases; the workflow is the contract.

## Test seams

`internal/cli.Options` carries optional test-injection fields. In
production these are all nil and the code falls back to real
implementations; tests pass them to drive code paths without hitting
the real world. The current set:

- `Registry` — a pre-built `*registry.Registry`. Skips `registry.Load()`.
- `registryLoadErr` — forces `resolveRegistry` to surface a failure
  (used by the generate-handles-broken-registry test).
- `updateHTTPClient` — `http.Client` for `em-dee update --check`'s
  GitHub API call.
- `updateExePath` — overrides `os.Executable()` for install-method
  detection tests.
- `updateApply` — overrides the binary swap in `em-dee update`.
- `reviewRunner` — `review.Runner` for the claude-review pipeline;
  production uses `&review.ExecRunner{}`.

When adding a new external dependency to a CLI subcommand, add a
seam to `Options` rather than reaching out to package globals or
`os.Exit` directly. The pattern is uniform across the package and
tests are the only consumer.

## Templates filesystem

`internal/registry/templates/` carries the catalog. Every option's
`.md` block is currently a TODO stub — a TODO HTML comment plus a
one-line placeholder summary, NOT a zero-byte file (the validator
rejects those). Finalised content lands per language. The
render-package tests use a separate fixture tree at
`internal/render/testdata/templates/` so they're independent of
catalog content drift.
