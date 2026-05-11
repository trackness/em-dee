# em-dee — Design Spec

**Date:** 2026-05-11
**Status:** Design — awaiting review
**Owner:** james

## 1. Overview

em-dee is a Go CLI that generates `CLAUDE.md` files for new projects from a
curated, embedded set of opinionated markdown blocks. The user selects a
language and a small number of associated choices (framework, logging,
testing) plus cross-cutting concerns (infra, CI, tooling); em-dee
concatenates the corresponding blocks into `CLAUDE.md` and (by default)
asks Claude to review the result before exiting.

The project is itself maintained primarily via Claude Code sessions, so
the repo is structured to make mechanical edits safe and obvious for a
future Claude session: filesystem layout *is* the schema, validation
runs as part of `task verify`, and every convention is documented in a
top-level `CLAUDE.md`.

## 2. Goals and non-goals

### Goals (v1)

- Single self-contained Go binary; templates embedded via `//go:embed`.
- Categorized configurator with both single- and multi-pick categories.
- Language is the primary axis; framework / logging / testing /
  dependency-manager are nested under each language.
- Cross-cutting categories (infra, CI, tooling) sit at the top level and
  have language-agnostic content.
- Per-category `default` values so the user can Enter through routine
  picks.
- Pretty, paginated interactive flow (huh) and a flag-driven
  non-interactive mode (cobra).
- Built-in Claude review of the generated file, default on, with a
  structured JSON response that em-dee parses and presents.
- Repo conventions designed for Claude-Code maintainability: mechanical
  add-option / add-category recipes, manifest hygiene validation,
  golden-fixture render tests.

### Non-goals (v1)

- No variable interpolation in blocks (no `{{.ProjectName}}` etc.).
- No user-supplied custom blocks at runtime (no XDG-config merging).
  Customization = fork the repo.
- No cross-category conflict rules. We trust the picker.
- No update / merge mode for existing `CLAUDE.md` files. Re-running
  regenerates; the user reconciles via git.
- No language-specific *content* in cross-cutting blocks. One Dockerfile
  block, not one per language. Combinatorial explosion is rejected.

## 3. Architecture

### 3.1 Stack

- Go 1.22+
- `github.com/spf13/cobra` — CLI framework
- `charm.land/huh/v2` — interactive forms
- `github.com/charmbracelet/lipgloss` — terminal styling
- `gopkg.in/yaml.v3` — parse `_index.yaml`
- `embed` (stdlib) — ship templates inside the binary

### 3.2 Project layout

```
em-dee/
├── CLAUDE.md                       # Claude-Code maintenance contract
├── README.md
├── LICENSE
├── go.mod / go.sum
├── Taskfile.yml                    # task verify, build, release
├── .goreleaser.yaml                # multi-arch release config
├── .claude/                        # harness config (hooks, settings)
├── cmd/em-dee/
│   └── main.go                     # thin entrypoint → cli.Execute()
├── internal/
│   ├── cli/
│   │   ├── root.go                 # cobra root cmd, persistent flags
│   │   ├── generate.go             # default cmd + explicit `generate`
│   │   ├── list.go
│   │   ├── show.go
│   │   └── version.go
│   ├── registry/
│   │   ├── registry.go             # load + parse _index.yaml files
│   │   ├── validate.go             # manifest hygiene checks
│   │   ├── defaults.go             # ApplyDefaults(Picks) → Picks
│   │   └── *_test.go
│   ├── render/
│   │   ├── render.go               # Picks + Registry → []byte
│   │   └── render_test.go          # golden fixture tests
│   ├── tui/
│   │   ├── form.go                 # huh form construction
│   │   └── styles.go               # lipgloss styles
│   └── review/
│       ├── review.go               # shell out to claude -p
│       ├── parse.go                # parse JSON response with fallbacks
│       ├── present.go              # lipgloss-rendered output
│       ├── prompt.md               # embedded review prompt template
│       └── *_test.go
├── templates/                      # //go:embed templates/**
│   ├── 10-language/
│   │   ├── _index.yaml
│   │   ├── go/
│   │   │   ├── base.md
│   │   │   ├── 10-framework/{_index.yaml, *.md}
│   │   │   ├── 20-logging/{_index.yaml, *.md}
│   │   │   ├── 30-testing/{_index.yaml, *.md}
│   │   │   └── 40-deps/{_index.yaml, *.md}
│   │   ├── python/
│   │   │   └── (same shape)
│   │   ├── typescript-node/
│   │   │   └── (same shape)
│   │   └── rust/
│   │       └── (same shape)
│   ├── 20-infra/{_index.yaml, *.md}
│   ├── 30-ci/{_index.yaml, *.md}
│   └── 40-tooling/{_index.yaml, *.md}
└── testdata/
    └── golden/
        └── <scenario>/
            ├── selection.yaml
            └── expected.md
```

### 3.3 Package responsibilities

- **`cmd/em-dee`** — main; calls `cli.Execute()`. Should be ~10 lines.
- **`internal/cli`** — cobra command definitions, flag wiring, glue
  between `registry`, `tui`, `render`, and `review`. Knows nothing about
  YAML or huh internals.
- **`internal/registry`** — loads the embedded templates filesystem,
  parses every `_index.yaml`, validates the manifest, and exposes a
  typed `Registry` plus a `Picks` type representing a user's selection.
  `ApplyDefaults(Picks) Picks` is the single source of truth for default
  resolution; both non-interactive flag handling and `--use-defaults`
  call it.
- **`internal/render`** — pure function: `(Registry, Picks) → []byte`.
  No I/O. Concatenates the chosen blocks in render order, separated by
  `\n\n`. Trivial to test against golden fixtures.
- **`internal/tui`** — constructs huh forms from a `Registry`, runs
  them, returns a `Picks`. Two-phase: language first, then a dynamically
  constructed form for the rest. lipgloss styles for the success summary
  live here too.
- **`internal/review`** — shells out to `claude -p
  --output-format=json`, parses the structured response, and presents
  it. Embedded prompt template at `internal/review/prompt.md`.

## 4. Data model

### 4.1 Templates filesystem

The filesystem *is* the schema. The render order falls out of folder
naming with two-digit numeric prefixes (`NN-name`):

- Categories at the top level (`10-language`, `20-infra`, `30-ci`,
  `40-tooling`) render in prefix order.
- Within a language subtree, the language's `base.md` renders first,
  then nested sub-categories in their prefix order.

The `language` category is special: it is required, single-pick, and its
choice changes which sub-categories are available. All other categories
are flat (no sub-tree).

### 4.2 `_index.yaml` schema

Every category folder contains exactly one `_index.yaml`:

```yaml
display_name: "Logging"             # shown in huh form
pick: single                        # "single" | "multi"
required: false                     # if true, picker blocks until satisfied
default: loguru                     # optional; single→id, multi→[ids]
options:
  - id: stdlib                      # kebab; must match a .md filename
    display_name: "Standard library logging"
    description: "logging module"   # shown in huh option help
    file: stdlib.md
  - id: loguru
    display_name: "Loguru"
    description: "Drop-in logging replacement"
    file: loguru.md
```

For multi-pick categories, `default` is a list:

```yaml
pick: multi
default: [docker, github-actions]
options:
  - id: docker
    ...
```

If `default` is omitted, the picker has no pre-selection. The language
category never carries a `default`.

### 4.3 Block files

Block files are **plain markdown — no frontmatter.** All metadata lives
in `_index.yaml`. Reasoning: a single source of truth is easier to keep
consistent than two places that can drift; the raw `.md` is easier to
read and author. The filename must match the option `id` plus `.md`.

### 4.4 Render order

```
templates/10-language/<lang>/base.md
templates/10-language/<lang>/10-framework/<chosen>.md         (if any)
templates/10-language/<lang>/20-logging/<chosen>.md           (if any)
templates/10-language/<lang>/30-testing/<chosen>.md           (if any)
templates/10-language/<lang>/40-deps/<chosen>.md              (if any)
templates/20-infra/<chosen>.md       [multi: each in _index.yaml order]
templates/30-ci/<chosen>.md          [multi: each in _index.yaml order]
templates/40-tooling/<chosen>.md     [multi: each in _index.yaml order]
```

Blocks are separated by `\n\n`. No leading or trailing whitespace
beyond what's in the blocks themselves.

## 5. CLI surface

```
em-dee                              # → interactive generate (default)
em-dee generate                     # same, explicit
em-dee generate \
  --language=python \
  --python.framework=fastapi \
  --python.logging=loguru \
  --infra=docker,kubernetes \       # multi-pick: comma-separated
  --ci=github-actions \
  --out=./CLAUDE.md \
  --force

em-dee generate --use-defaults      # accept all defaults; only prompt for language
em-dee generate --dry-run           # print to stdout, write nothing
em-dee generate --no-review         # skip the claude review
em-dee generate --review-out=r.json # also write review JSON to file

em-dee list                         # human-readable category/option tree
em-dee list --json                  # machine-readable
em-dee show language python         # cat one block to stdout
em-dee version
```

### 5.1 Flag derivation

- Top-level categories become long flags: `--<category-id>` (e.g.
  `--infra`, `--ci`).
- Language-nested categories are namespaced by language id:
  `--<lang-id>.<category-id>` (e.g. `--python.logging`,
  `--go.framework`). This keeps each flag self-documenting and avoids
  `--framework` meaning different things based on `--language`.
- Multi-pick categories accept comma-separated option ids.
- Unknown option ids are a hard error.
- Required categories without a value in non-interactive mode are a hard
  error.
- Defaults apply when a flag is *omitted*. Explicit empty
  (`--python.logging=`) selects "none chosen" for optional categories;
  for required categories it is an error.

### 5.2 Interactive flow

1. **Phase 1 — language**: a huh form with one `huh.Group` containing
   the language `huh.Select`. Required.
2. **Phase 2 — rest**: a huh form constructed dynamically from the
   chosen language's subtree plus the cross-cutting categories. One
   `huh.Group` per category, paginated. Defaults pre-populate bound
   variables before `.Run()` so Enter accepts.
3. **Confirm**: a final huh confirm screen lists the blocks that will
   be rendered, in order.
4. **Existing-file check** (see §6).
5. **Write the file.**
6. **Review** (default on; see §7).
7. **Success line** rendered via lipgloss: `wrote CLAUDE.md (N blocks,
   N.NN KB)` and (if review ran) the review summary block.

### 5.3 Non-interactive flow

Identical pipeline minus the huh forms. Flag values populate `Picks`,
`registry.ApplyDefaults` fills in omitted optional categories, validation
catches errors, then the same write → review path runs.

The `--use-defaults` flag is a one-line shortcut: prompt only for
language (still interactive), then accept defaults for everything else.
Errors if any non-defaulted optional category remains — though by
construction, optional categories with no default simply become "not
selected."

## 6. Existing-file handling

- **Default**: error out with a clear message.
  `CLAUDE.md exists at <path>. Pass --force to overwrite (current file
  will be backed up) or --out to write elsewhere.` Exit non-zero.
- **`--force`**: rename existing file to `CLAUDE.md.bak.<unix-ts>` *in
  the same directory* before writing the new content. Print the backup
  path on stderr. Never delete; always rename.
- **`--out=<path>`**: write to the given path. Same overwrite rules
  apply at that path.
- **`--dry-run`**: print rendered content to stdout. Skips both the
  existence check and the review.

## 7. Claude review

### 7.1 Invocation

```
claude -p --output-format=json
  (reads the rendered CLAUDE.md content on stdin)
  (review prompt embedded from internal/review/prompt.md)
```

The wire-level transport (`--output-format=json`) gives a typed protocol
envelope from the `claude` CLI, separating "Claude responded" from
"claude exited non-zero" from "claude timed out."

The review prompt is committed in `internal/review/prompt.md` and
embedded into the binary. It instructs Claude to respond with a single
JSON object matching the schema below, with no markdown fences and no
preamble.

### 7.2 Response schema

```json
{
  "verdict": "ok" | "warnings" | "problems",
  "summary": "one-sentence overall assessment",
  "issues": [
    {
      "severity": "info" | "warning" | "error",
      "location": "<section name or short quoted excerpt>",
      "issue": "<what's wrong>",
      "suggestion": "<what to do about it>"
    }
  ]
}
```

`issues` may be empty (and typically is when `verdict == "ok"`).

### 7.3 Parsing

Three-tier:

1. **Strict** JSON parse against the schema. If it succeeds and
   validates, use it.
2. **Lenient extraction**: take the substring from the first `{` to the
   last `}`, re-parse. Handles cases where Claude wraps in markdown
   fences or adds a preamble despite instructions.
3. **Final fallback**: surface the raw response text under a "review
   (unstructured)" header, flag the parse failure to the user, exit 0.

### 7.4 Presentation

lipgloss-rendered, in this shape:

```
── claude review ──
✓ ok    The CLAUDE.md is clear and complete.

(or)

⚠ warnings    Two minor issues.
  Section "Build"     — Missing reference to lockfile commit policy
    → Add a one-line note: "commit lockfiles for reproducible builds"
  Section "Testing"   — Pytest invocation example is stale
    → Use `pytest -q` instead of `py.test`
```

Severities color-coded: info=neutral, warning=yellow, error=red.
Verdict header gets `✓` green, `⚠` yellow, `✗` red.

### 7.5 Exit code

- `ok` or `warnings` → exit 0.
- `problems` → exit non-zero (specific code TBD in plan, suggest 2).
- Parse failure → exit 0 (review is best-effort).
- The file was always written successfully before review runs; review
  is advisory and the user can always inspect the file themselves.

A `--strict-review` flag making `warnings` also fail is deferred to v2.

### 7.6 Failure modes

- `claude` not on PATH → print `note: claude CLI not found; skipping
  review` to stderr; exit 0.
- `claude -p` exits non-zero → print stderr verbatim under a `claude
  review failed:` header; exit 0.
- 60-second timeout on the claude subprocess; on timeout, kill the
  process group, print `note: claude review timed out after 60s`; exit 0.
- `--dry-run` skips review entirely (nothing was written).

### 7.7 Review-related flags

- `--review` / `--no-review` — defaults to `--review`.
- `--review-out=<path>` — write the parsed JSON to disk (for CI and
  tooling). Independent of whether the review is presented on stdout.
- `--review-timeout=<duration>` — override the 60s default.

## 8. Defaults

### 8.1 Schema

- `default` is a top-level field on each `_index.yaml`.
- Single-pick: string option id (must reference a valid `id` within
  `options`).
- Multi-pick: list of option ids (each must reference a valid `id`).
- If `default` is omitted, the picker has no pre-selection.
- The `language` category must not have a `default`. Validation
  enforces this.

### 8.2 Resolution

`registry.ApplyDefaults(picks Picks) Picks` is the single source of
truth. It walks every category in the registry; for each category not
already chosen in `picks`, it fills in the default. The function is
pure and easily tested.

### 8.3 Required-with-default

If a category is `required: true` *and* has a `default`, the default
seeds the interactive picker but the picker still presents the category
(the user can hit Enter to accept). In non-interactive mode, the default
satisfies the requirement when the flag is omitted.

### 8.4 `em-dee list` output

`em-dee list` annotates options with `(default)` next to the defaulted
option(s) so the user can preview what Enter will give them.

## 9. Validation

### 9.1 Manifest hygiene (runs in `task verify`)

A test in `internal/registry` walks the embedded templates filesystem
and asserts:

- Every category folder name matches `^[0-9]{2}-[a-z][a-z0-9-]*$`.
- Every category folder contains exactly one `_index.yaml`.
- Every `_index.yaml` parses and matches the schema.
- Every `options[].file` exists in the same folder.
- Every `.md` file in a category folder is referenced by some option
  (no orphans).
- Every `id` is unique within its `options` list.
- Every `default` value references a valid `id`.
- The language category has no `default` and `required: true`.
- Every language sub-folder under `templates/10-language/` contains a
  `base.md` plus zero or more `NN-<name>/` sub-categories matching the
  same rules.

Validation failure on any of the above fails `task verify` and CI.

### 9.2 Golden fixtures

`testdata/golden/<scenario>/` contains pairs:

- `selection.yaml` — a `Picks` value, e.g.:

  ```yaml
  language: python
  python.framework: fastapi
  python.logging: loguru
  infra: [docker]
  ci: [github-actions]
  ```

- `expected.md` — the exact rendered output.

Tests in `internal/render` load every scenario, render, and assert
byte-equality against `expected.md`. To update a golden, regenerate via
a `task golden-update` target. At least one fixture per language is
required; one fixture per major edge case (no optional picks, all
defaults, every category populated).

## 10. Repo conventions for Claude-Code maintenance

### 10.1 `CLAUDE.md` at repo root

The top-level `CLAUDE.md` is the contract a future Claude session reads
before editing. It documents:

- **Mechanical recipes**:
  - *Add an option to an existing category*: drop
    `templates/<NN-cat>/<id>.md`; append one entry to that folder's
    `_index.yaml`. Run `task verify`.
  - *Add a new top-level category*: `mkdir
    templates/<NN-name>/`; create `_index.yaml` with `pick`, optional
    `default`, and `options`; add `.md` files. Run `task verify`.
  - *Add a new language*: `mkdir templates/10-language/<id>/`; create
    `base.md`; add the entry in `templates/10-language/_index.yaml`;
    add language-nested sub-categories using the same recipe as
    top-level categories. Run `task verify`.
  - *Reorder categories*: change the folder's `NN-` prefix.
- **Naming rules**: kebab ids; folder names `NN-name` with two-digit
  prefix; option `file` field is `<id>.md`.
- **Anti-patterns**: don't add frontmatter to `.md` files; don't add
  cross-category constraint rules in code; don't reorder by editing
  `_index.yaml`; don't add language-specific content to cross-cutting
  blocks.
- **Required commands**: `task verify` before commit; `task build` for
  a local binary.

### 10.2 `Taskfile.yml`

- `task verify` — `gofmt`, `go vet`, `go test ./...`, manifest hygiene
  test (which is itself a Go test, so this is just `go test ./...`).
- `task build` — `go build ./cmd/em-dee`.
- `task release` — invoke `goreleaser`.
- `task golden-update` — regenerate `testdata/golden/*/expected.md`
  from current render output.

### 10.3 `.claude/` harness config

A `.claude/settings.local.json` wiring hooks for `task verify` on file
changes within `templates/` is recommended but not required. The exact
hook contents are deferred to the implementation plan; this is a
soft-convention, not a hard part of the spec.

## 11. Testing strategy

| Package | Test types |
|---------|-----------|
| `internal/registry` | parse correctness, validation rules (each rule has its own test), `ApplyDefaults` behaviour |
| `internal/render` | golden fixture tests; one per major scenario |
| `internal/review` | JSON parse variants (clean, fenced, with preamble, malformed); failure-mode tests with a stub `claude` binary or injected interface |
| `internal/cli` | cobra command wiring tests using `cmd.SetArgs(...)` and capturing output |
| `internal/tui` | smoke test that constructs a form for each language without panicking; full TUI flow is not unit-tested |

The `review` package's external dependency (`claude` CLI) is abstracted
behind an interface so tests can inject a fake. The default
implementation shells out via `os/exec`.

## 12. Distribution

- **`goreleaser`** builds binaries for darwin/arm64, darwin/amd64,
  linux/arm64, linux/amd64, windows/amd64.
- **`go install github.com/<owner>/em-dee/cmd/em-dee@latest`** — for
  users with a Go toolchain.
- **Homebrew tap** — optional, configured via `goreleaser`. Decide at
  release time, not in this spec.

## 13. v1 catalog (starting point)

The exact catalog is the one place the spec leaves room to iterate
during implementation. Strawman (subject to revision in the plan, but
this is what we start with):

| # | Category | Pick | Required | Default | Options |
|---|---|---|---|---|---|
| 10 | language | single | yes | — | `go`, `python`, `typescript-node`, `rust` |
| Within go | 10-framework | single | no | `stdlib-net-http` | `stdlib-net-http`, `gin`, `echo` |
| Within go | 20-logging | single | no | `slog` | `slog`, `zap` |
| Within go | 30-testing | single | no | `go-test` | `go-test` |
| Within python | 10-framework | single | no | `fastapi` | `fastapi`, `django`, `flask` |
| Within python | 20-logging | single | no | `loguru` | `stdlib`, `loguru`, `structlog` |
| Within python | 30-testing | single | no | `pytest` | `pytest` |
| Within python | 40-deps | single | no | `uv` | `uv`, `poetry`, `pip-tools` |
| Within typescript-node | 10-framework | single | no | `express` | `express`, `fastify`, `hono` |
| Within typescript-node | 20-logging | single | no | `pino` | `pino`, `winston` |
| Within typescript-node | 30-testing | single | no | `vitest` | `vitest`, `jest` |
| Within rust | 10-framework | single | no | `axum` | `axum`, `actix-web` |
| Within rust | 20-logging | single | no | `tracing` | `tracing`, `log` |
| Within rust | 30-testing | single | no | `cargo-test` | `cargo-test` |
| 20 | infra | multi | no | `[docker]` | `docker`, `kubernetes`, `terraform` |
| 30 | ci | multi | no | `[github-actions]` | `github-actions`, `gitlab-ci` |
| 40 | tooling | multi | no | `[pre-commit]` | `pre-commit`, `mise`, `taskfile`, `makefile` |

The block *content* (the actual `.md` text per option) is to be drafted
during implementation, informed by the user's preferences as the catalog
materialises. Empty options can be initially populated with a one-line
placeholder pointing back to this spec, marked TODO, so the structure
compiles and tests pass before content is finalised.

## 14. Module identity

- **Module path**: `github.com/<owner>/em-dee` — owner to be filled in
  before module init.
- **Binary name**: `em-dee`.
- **Env var prefix**: `EM_DEE_` (e.g. `EM_DEE_REVIEW_TIMEOUT`,
  `EM_DEE_OUT`). Reserved for future use; not used in v1.

## 15. Deferred to plan or v2

- Exact `.md` block content per option.
- `.claude/settings.local.json` hook contents.
- Whether to publish a Homebrew tap at first release.
- Specific exit code for `verdict == "problems"`.
- Module owner / GitHub org.
- License selection.

## 16. Open questions

None at the time of writing that block planning. Anything that surfaces
during the spec review will be folded back into this document before
the writing-plans skill is invoked.
