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

Content design rules for catalog blocks live in [`CONTENT-STYLE.md`](CONTENT-STYLE.md).

## Mechanical recipes

The templates filesystem lives at `internal/registry/templates/`
(inside the registry package because `//go:embed` cannot escape its
package directory). Every category folder is named `NN-<id>` with a
two-digit numeric prefix; that prefix dictates render order. Some
categories are *containers* — their options point at subdirectories
rather than `.md` files, and the sub-categories beneath are
conditional on which option is picked (see CONTENT-STYLE.md §2.4).

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
  with `file: <id>/base.md`. Add language-universal categories via the
  top-level recipe. If the language has type-conditional content, add
  a `10-type/` container (at most one per language scope, enforced by
  the validator) via the next recipe. Note: the language id must not
  collide with any top-level category id (the validator enforces
  this). Run `task verify`.
- **Add a type under a language**: under
  `internal/registry/templates/10-language/<lang>/10-type/`, `mkdir
  <type>/`. Pick one of two mutually-exclusive shapes for the type's
  entry in `10-type/_index.yaml`; the validator rejects mixing the
  two (the declared `file:` must match the on-disk presence or
  absence of `base.md`):
  - **With type-base discipline**: declare `file: <type>/base.md`
    and create `<type>/base.md`.
  - **Without type-base discipline**: declare `file: <type>/` (bare
    trailing slash) and do **NOT** create `<type>/base.md`.
  Every option in a single category must be the same shape — either
  all leaf (`.md` files) or all container (subdirectories). Mixed
  shapes are also rejected by the validator. Add
  `<type>/<NN-sub-cat>/_index.yaml` for each type-conditional
  sub-category and its `.md` option files. Run `task verify`.
- **Add a new option to a type sub-category**: drop
  `internal/registry/templates/10-language/<lang>/10-type/<type>/<NN-sub-cat>/<id>.md`,
  append the entry to that folder's `_index.yaml`, run `task verify`.
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
  the suggestion is YAGNI / out-of-scope. **No dismissal, including
  nits.** Loop until clean: after fixes land, re-dispatch the
  reviewer; repeat until the reviewer approves without further
  findings.
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

## CLI surface

The binary's invocation surface is the contract downstream tooling and
skills branch on. Changes to subcommand names, flag names, exit-code
semantics, or `update --check` exit values are breaking and need a
minor/major version bump.

| Command                       | Purpose                                                                                                                                                       |
|-------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `em-dee` / `em-dee generate`  | Build a CLAUDE.md. Interactive on a TTY; non-interactive needs `--language=<id>` (every other category falls back to its registry default).                  |
| `em-dee list [--json]`        | Print the catalog tree.                                                                                                                                       |
| `em-dee show <ref>`           | Print one block's `.md` content. Refs are dotted (`python.cli.framework.typer`, `infra.docker`); containers are elided.                                       |
| `em-dee version [--json]`     | Print embedded build version, commit, and date.                                                                                                               |
| `em-dee update [--check]`     | Self-update from GitHub Releases. `--check` exit codes: `0` up-to-date (or dev build), `1` update available, `2` error.                                       |

Category selection: `--language=<id>` is the only flag that picks a
category option. Every other category is picked through the
interactive form, or — when no interactive form runs — filled in
from its registry default automatically.

Behaviour flags on `em-dee generate`:

- `--out=<path>` (default `CLAUDE.md`) — where to write.
- `--force` — overwrite existing file at `--out` (previous contents
  backed up to `<out>.bak.<unix-ts>` in the same directory).
- `--dry-run` — write rendered output to stdout instead of disk;
  skips the existing-file check and skips the Claude review.
- `--use-defaults` — in the interactive flow, skip form 2 (the
  non-language picks) and silently accept registry defaults. On the
  non-interactive path (`--language=<id>` supplied without a TTY)
  defaults are filled automatically; this flag is a no-op there.
- `--review` / `--no-review` — toggle the post-write Claude review
  (default on).
- `--review-out=<path>` — write the parsed review JSON to disk.
- `--review-timeout=<duration>` — override the default 60s
  subprocess deadline (Go duration syntax).

## Subagents

- Every subagent dispatch uses `model: opus`. No exceptions, including
  reviewer subagents, fix-up subagents, and content-drafting subagents.
- The PR reviewer subagent is `trackness-agents:pr-reviewer` (see Git
  workflow). It is the only acceptable subagent type for PR review;
  general-purpose agents are not substituted.

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

`internal/registry/templates/` carries the catalog. Block content
lands per language; the Go subtree's primary blocks (`go/base.md`,
`go/20-logging/slog.md`, `go/10-type/cli/{base.md,10-framework/cobra.md,20-consumer/{agent,human,mixed}.md}`,
`go/10-type/tui/{base.md,10-framework/bubbletea.md}`,
`go/10-type/library/base.md`) carry finalised content. Remaining
options — Python catalog, Go alternates (`zap`, `kong`, `urfave-cli`,
`tview`), Go server type — are still TODO stubs: a TODO HTML comment
plus a one-line placeholder summary, NOT a zero-byte file (the
validator rejects those). The render-package tests use a separate
fixture tree at `internal/render/testdata/templates/` so they're
independent of catalog content drift.
