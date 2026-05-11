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
- `golang.org/x/term` — terminal-width detection (used by §7.4
  presentation wrap)
- `gopkg.in/yaml.v3` — parse `_index.yaml`
- `github.com/minio/selfupdate` (or equivalent) — atomic binary
  replacement (see §12.6)
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
├── internal/registry/templates/    # //go:embed source — must live inside
│   ├── 10-language/                # the registry pkg per Go's embed scope
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

**Note on templates location**: Go's `//go:embed` directive cannot
reference paths outside the package directory that owns it. The
templates filesystem therefore lives at
`internal/registry/templates/` rather than at the repo root. All
subsequent references in this spec to paths like
`templates/10-language/...` are logical — they're rooted at the
templates filesystem wherever it lives. The `_index.yaml` mechanical
recipes in CLAUDE.md §10.1 paths are similarly logical.

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
  call it. `Picks` is **tri-state per category**: for single-pick,
  `*string` (nil = unset, empty string = explicitly chosen none,
  non-empty = chosen option id); for multi-pick, `*[]string` (nil =
  unset, empty slice = explicitly chosen none, non-empty slice =
  chosen ids). The distinction between "unset" and "explicit none"
  matters for default application — `ApplyDefaults` only fills in
  categories that are `unset` (nil pointer), never categories that are
  explicitly empty.
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

Blocks are separated by `\n\n`. The rendered output ends with exactly
one trailing `\n`. Each block's own trailing newline is stripped
before joining; the final newline is appended once at the end. Empty
`Picks` (nothing selected) → zero bytes, not a stray newline. This
contract is testable and the renderer enforces it.

**Multi-pick determinism**: for any multi-pick category, the chosen
options are always emitted in `_index.yaml` declaration order,
regardless of the order the user typed them on the CLI
(`--infra=kubernetes,docker` and `--infra=docker,kubernetes` produce
byte-identical output) or selected them in the interactive form. This
keeps golden fixtures stable across input permutations.

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
em-dee show <ref>                   # cat one block to stdout (see below)
em-dee version
em-dee version --json
em-dee update
em-dee update --check
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

**Flag state mapping into `Picks` (tri-state per §3.3)**:

- Flag **omitted** from the command line → category remains `unset`
  (nil pointer). `ApplyDefaults` may fill it in.
- Flag **present with a value** (`--python.logging=loguru`) → category
  is set to the chosen option(s).
- Flag **present with empty value** (`--python.logging=`) → category
  is set to "explicitly empty" (the slice/pointer is non-nil but
  carries no option). For optional categories this writes no block; for
  required categories it is a hard error.

**cli ↔ registry hand-off**: the cobra layer collects flag state
into a `map[string]any` (top-level category id or namespaced
`<lang-id>.<category-id>` key → string or `[]string` value) and
passes it to `registry.ResolveSelection(m map[string]any) (Picks,
error)`, which is the single resolution entry point shared with
golden-fixture loading (§9.2). The CLI does no semantic resolution
of its own.

**`show` reference form**: `em-dee show` takes a single positional
dotted reference and prints the corresponding block's `.md` content
to stdout (or errors if the ref doesn't resolve to a leaf option).
Examples:

- `em-dee show language.python` → `templates/10-language/python/base.md`
- `em-dee show python.logging.loguru` →
  `templates/10-language/python/20-logging/loguru.md`
- `em-dee show infra.docker` → `templates/20-infra/docker.md`
- `em-dee show go.framework.gin` →
  `templates/10-language/go/10-framework/gin.md`

The reference grammar is `<segment>(.<segment>)*` where each segment is
an id (kebab); the resolver walks the registry left-to-right.

**Required category resolution**:

- `required: true` + `default: X` + flag omitted → default `X`
  satisfies the requirement; no error. (Note: per §9.1, the only
  required category in v1 — `language` — must not carry a `default`,
  so this branch is forward-compatibility only.)
- `required: true` + no `default` + flag omitted → hard error in
  non-interactive mode.
- `required: true` + flag present with empty value → hard error.

### 5.2 Interactive flow

The interactive flow runs **two sequential huh forms** with separate
`.Run()` calls. We deliberately do *not* use huh's `OptionsFunc` /
dynamic-fields pattern on a single form — instead, the language is
fully resolved in form 1 before form 2 is constructed. Reasoning:
form 2's *structure* (which groups exist, what their option sets are)
depends on the chosen language; constructing it after form 1 returns
keeps each form's definition trivially testable and avoids huh's
dynamic-field corner cases.

1. **Form 1 — language**: a single `huh.Form` with one
   `huh.Group` containing one `huh.Select[string]` for the language.
   The select is constructed with no pre-selected value, so the
   user must affirmatively press Enter on a highlighted option to
   accept. (huh v2's `huh.Select` always *highlights* one row — the
   first by default — but does not commit a value until the user
   presses Enter; a Ctrl-C/Esc cancellation is handled per the
   "Cancellation" paragraph below.) A `.Validate(func(s string)
   error { ... })` on the field enforces non-empty selection as
   belt-and-braces — though by construction the user cannot accept
   an empty value from a `huh.Select` with a non-empty options list.
2. **Form 2 — rest**: constructed *after* form 1 returns, using the
   chosen language's subtree plus the cross-cutting categories. One
   `huh.Group` per category, paginated. Defaults pre-populate bound
   variables before `.Run()` so Enter accepts the default. Optional
   categories without a default present an empty selection state by
   default.
3. **Confirm**: a final `huh.Group` with a `huh.Confirm` lists the
   blocks that will be rendered, in render order (§4.4), and asks for
   final approval. This group is appended to form 2.
4. **Existing-file check** (see §6).
5. **Write the file.**
6. **Review** (default on; see §7).
7. **Success line** rendered via lipgloss: `wrote CLAUDE.md (N blocks,
   N.NN KB)` and (if review ran) the review summary block.

**Cancellation**: Ctrl-C / Esc at any point during form 1 or form 2
aborts the run cleanly. No partial state is written. Back-navigation
across forms is *not* supported in v1 — if the user picks the wrong
language in form 1, they must Ctrl-C and re-run. (Within a form, huh's
own back-navigation between groups works as normal.)

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

em-dee invokes `claude` once per review as a subprocess via
`os/exec.Command`:

```
claude -p "<full prompt>" --output-format=json
```

Where `<full prompt>` is built in Go by concatenating the embedded
review prompt template with the rendered CLAUDE.md content, separated
by a fenced delimiter:

```
<review prompt template>

---

<file path="CLAUDE.md">
<rendered CLAUDE.md content>
</file>
```

The entire string is passed as a single `-p` argument. **No stdin** —
the spec does not assume `claude -p` reads its prompt from stdin
(it accepts the prompt as an argument). Argv length is well below
ARG_MAX (≥256 KB on Darwin/Linux); a typical generated CLAUDE.md is
< 10 KB.

The wire-level transport (`--output-format=json`) gives a typed
protocol envelope from the `claude` CLI, separating "Claude
responded" from "claude exited non-zero" from "claude timed out."

The review prompt template is committed at
`internal/review/prompt.md` and embedded into the binary via a
`//go:embed prompt.md` directive in `internal/review/review.go`. It
instructs Claude to respond with a single JSON object matching the
schema below, with no markdown fences and no preamble.

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

This is the schema as produced by the claude wire response. §7.7
defines an *additional* sentinel `verdict` value (`"unstructured"`)
that appears **only** in the on-disk `--review-out` JSON when
parsing fell back to tier 3 — never in a claude wire response.
Consumers parsing an `--review-out` artifact should treat the
`verdict` field as the four-value enum `"ok" | "warnings" |
"problems" | "unstructured"`.

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

**Truncation**: section names ("location" field) longer than 60
columns are truncated to 57 columns + `...`. Issue text and
suggestions are wrapped (not truncated) to the terminal width
detected via `golang.org/x/term`, with a fallback of 100 columns
when the terminal width can't be determined.

### 7.5 Exit code

- `ok` or `warnings` → exit 0.
- `problems` → exit **2**.
- Parse failure → exit 0 (review is best-effort; see §7.3 tier 3).
- The file was always written successfully before review runs; review
  is advisory and the user can always inspect the file themselves.

A `--strict-review` flag making `warnings` also fail is deferred to v2.

### 7.6 Failure modes

- **`claude` not on PATH**: detected via `exec.LookPath("claude")`.
  On any `LookPath` error (including `exec.ErrNotFound`), print
  `note: claude CLI not found; skipping review` to stderr and exit 0.
- **`claude -p` exits non-zero**: print stderr verbatim under a
  `claude review failed:` header; exit 0. The file was already
  written; review is best-effort.
- **Timeout**: 60-second wall-clock default from subprocess start to
  exit, overridable via `--review-timeout=<duration>`. On timeout,
  kill the process group, print `note: claude review timed out
  after <duration>`; exit 0. Per-byte / streaming timeouts are not
  used in v1.
- **`--dry-run`** skips review entirely (nothing was written).
- The `EM_DEE_REVIEW_TIMEOUT` env var is **not** read in v1. Only the
  flag is honored. (See §14.)

### 7.7 Review-related flags

- `--review` / `--no-review` — defaults to `--review`.
- `--review-out=<path>` — write the parsed JSON to disk (for CI and
  tooling). Independent of whether the review is presented on stdout.
  If parsing reaches tier 3 (unstructured fallback per §7.3), the
  written JSON is `{"verdict": "unstructured", "summary": "claude
  review could not be parsed as structured JSON", "raw": "<original
  text>", "issues": []}` — a sentinel `verdict` value so consumers can
  branch on this case explicitly.
- `--review-timeout=<duration>` — override the 60s default. Accepts
  Go duration syntax (`30s`, `2m`, etc.).

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

In v1 there is no category that is both `required: true` and carries a
`default` — the only required category is `language`, and §4.2 / §9.1
forbid `default` on language. The semantics are nonetheless defined
for forward compatibility:

- **Interactive**: the default seeds the picker; the user must still
  hit Enter (or pick a different option). The picker is not skipped.
- **Non-interactive**: an omitted flag is satisfied by the default;
  no error. Explicit empty value is a hard error per §5.1.

### 8.4 `em-dee list` output

`em-dee list` annotates options with `(default)` next to the defaulted
option(s) so the user can preview what Enter will give them.

## 9. Validation

### 9.1 Manifest hygiene (runs in `task verify`)

A test in `internal/registry` walks the embedded templates filesystem
and asserts each of the following. Validation failure on any rule
fails `task verify` and CI.

**Folder / file structure:**

- Every category folder name matches `^[0-9]{2}-[a-z][a-z0-9-]*$`.
- Every category folder contains exactly one `_index.yaml`.
- Every `options[].file` exists in the same folder.
- Every `options[].file` is non-empty (size > 0 bytes). A zero-byte
  block would produce a `\n\n` gap with no content between when the
  renderer joins blocks (§4.4), breaking the trailing-newline
  contract — pin the constraint at the validator so the renderer
  doesn't have to defend against it.
- Every `.md` file in a category folder is referenced by some option
  (no orphans). **Exception**: `base.md` directly under
  `templates/10-language/<lang>/` is always rendered but is not
  referenced by any option — the orphan check explicitly excludes
  these files.
- Every language sub-folder under `templates/10-language/` contains a
  `base.md` plus zero or more `NN-<name>/` sub-categories matching
  the same rules.

**`_index.yaml` schema:**

- Every `_index.yaml` parses and matches the typed schema.
- `pick` is exactly `"single"` or `"multi"`.
- `display_name` is non-empty for the category and for every option.
- Every option `id` matches `^[a-z][a-z0-9-]*$` (kebab).
- Every `id` is unique within its `options` list.
- For `pick: single`: if `default` is present, it is a string
  referencing a valid option `id`.
- For `pick: multi`: if `default` is present, it is a list of strings,
  each referencing a valid option `id`, with no duplicates.

**Category-level invariants:**

- The `language` category (the `_index.yaml` at
  `templates/10-language/_index.yaml`) has `required: true`, `pick:
  single`, and no `default`.
- Every other category has `required: false` in v1. (Forward-compat:
  the validator allows other categories to be `required: true`, but
  the catalog must not exercise it in v1.)

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

  The dotted keys are the same `<lang-id>.<category-id>` form used by
  the CLI flags (§5.1). `selection.yaml` is parsed via the same
  flag-resolution path as CLI flag handling (a shared
  `registry.ResolveSelection(map[string]any) Picks` helper) — golden
  fixtures and CLI inputs share one code path, so the two cannot
  drift.

- `expected.md` — the exact rendered output.

Tests in `internal/render` load every scenario, render, and assert
byte-equality against `expected.md`. To update a golden, regenerate
via a `task golden-update` target.

**Coverage requirements:**

- At least one fixture per *finalised* language (i.e. a language whose
  option content is no longer TODO). Languages whose blocks are still
  TODO placeholders are excluded from golden coverage until content
  is finalised; this avoids constant golden churn during catalog
  buildout.
- At least one fixture per major edge case (no optional picks, all
  defaults, every category populated, multi-pick category with
  selections entered in reverse manifest order — to lock in the
  ordering rule from §4.4).

## 10. Repo conventions for Claude-Code maintenance

### 10.1 `CLAUDE.md` at repo root

The top-level `CLAUDE.md` is the contract a future Claude session reads
before editing. It documents, in this order:

- **Operating principles** (load-bearing; every session in this repo
  works under these):
  1. Don't assume. Don't hide confusion. Surface tradeoffs.
  2. Minimum code that solves the problem. Nothing speculative.
  3. Touch only what you must. Clean up only your own mess.
  4. Define success criteria. Loop until verified.
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
- **Anti-patterns**:
  - Don't add frontmatter to `.md` files; metadata lives only in
    `_index.yaml`.
  - Don't add cross-category constraint rules in code; the picker
    soft-trusts the user.
  - Don't reorder by editing `_index.yaml` `options` lists — change
    the folder's `NN-` prefix, or move options between manifests.
  - Don't add language-specific content to cross-cutting blocks (no
    Python-specific Dockerfile lives under `templates/20-infra/`).
  - **Don't run `task golden-update` to fix a failing test in CI.**
    Run it only after intentional render-logic or template changes,
    locally, with the resulting diff inspected before committing.
    The golden fixtures only catch regressions if they aren't blindly
    regenerated.
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

## 12. Build, release, install, update

The full lifecycle is supported via the GitHub repository. No
out-of-band artefact hosting; nothing the user has to set up beyond
either a Go toolchain, Homebrew, or `curl`.

### 12.1 GitHub repository

Repo at `github.com/trackness/em-dee`. The Go module path matches.

- **Branch model**: `main` is the only long-lived branch. All work goes
  through PRs; PRs run CI; merges require green CI. Branch protection
  on `main` requires up-to-date and passing checks.
- **Tags**: SemVer (`v0.1.0`, …). Pushing a `v*` tag triggers a release.
- **Releases**: produced by `goreleaser` from a tag push. Each release
  carries platform archives, a `checksums.txt`, and source tarballs.

### 12.2 CI workflow — `.github/workflows/ci.yml`

Triggers on every push and PR.

- Checkout.
- Set up Go (matrix: latest stable + N−1).
- Cache Go modules.
- `task verify` — runs `gofmt`, `go vet`, `go test ./...`, and the
  manifest hygiene test (the hygiene test is just one of the Go tests,
  so it falls out of `go test ./...`).
- A parallel `lint` job runs `golangci-lint`.

CI must be green for any merge to `main`.

### 12.3 Release workflow — `.github/workflows/release.yml`

Triggers on tag push matching `v*`.

- Checkout with `fetch-depth: 0` (goreleaser requires full history).
- Set up Go.
- Run `goreleaser release --clean`.
- Requires `GITHUB_TOKEN` (provided automatically by Actions); requires
  `HOMEBREW_TAP_GITHUB_TOKEN` once the Homebrew tap is enabled.

### 12.4 `.goreleaser.yaml`

- **Builds**: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64,
  windows/amd64.
- **ldflags** inject version metadata (see §12.7).
- **Archives**: `tar.gz` for unix, `zip` for windows. The archive
  `name_template` is `em-dee_{{ .Os }}_{{ .Arch }}` (omits the
  version) so that
  `https://github.com/trackness/em-dee/releases/latest/download/em-dee_<os>_<arch>.<ext>`
  resolves to the latest release's asset. The release page still
  shows the version; only the filename is version-stripped.
- **Checksums**: SHA256 of every archive, published as
  `checksums.txt`. The self-update path depends on this file existing
  at a predictable URL inside each release.
- **GitHub Release**: auto-created, with archives + checksums
  attached. Release notes generated from commit history since the
  last tag.
- **Homebrew tap**: declared in the config but commented out until a
  `homebrew-trackness-tap` repo exists. Enabling is a one-line edit.

### 12.5 Install paths

The README documents all three with copy-pasteable commands.

- **Go install** (any platform with Go):
  ```
  go install github.com/trackness/em-dee/cmd/em-dee@latest
  ```
- **Direct download** (any platform; recommended for non-Go users):
  ```
  curl -fsSL \
    https://github.com/trackness/em-dee/releases/latest/download/em-dee_<os>_<arch>.tar.gz \
    | tar -xz
  ```
- **Homebrew** (post-tap setup, macOS/Linux):
  ```
  brew install trackness/tap/em-dee
  ```

### 12.6 Update mechanism

em-dee ships with a self-update subcommand for users who installed via
direct download. Users who installed via `go install` or `brew` are
detected and redirected to the appropriate tool.

- **`em-dee update`** — check GitHub Releases for the latest tag and,
  if newer than the embedded build version, install it:
  1. Fetch the latest release metadata via the GitHub API.
  2. Download the platform-appropriate archive.
  3. Download `checksums.txt`; verify the archive's SHA256.
  4. Extract the binary into a temp file.
  5. Atomically replace the running binary using
     `github.com/minio/selfupdate` (or equivalent).
  6. Print `updated <old version> → <new version>`.
- **`em-dee update --check`** — print whether an update is available,
  do not install. Exit codes: `0` = up-to-date, `1` = update
  available, `2` = error (network, parse, rate limit, etc.). This
  three-state convention lets `if em-dee update --check` and
  scripted comparisons distinguish "stale" from "broken."
- **`em-dee version --json`** — prints version, commit, date, and
  platform. Used by `update` and by tooling.

**Install-method detection** (best-effort, via the path of the running
executable resolved with `os.Executable`):

- *Unix (darwin, linux):*
  - Path under `${GOPATH}/bin/`, `${HOME}/go/bin/`, or `$(go env
    GOBIN)` (whichever is non-empty) → suggest `go install
    github.com/trackness/em-dee/cmd/em-dee@latest`; refuse self-update.
  - Path under `/opt/homebrew/`, `/usr/local/Cellar/`,
    `/home/linuxbrew/.linuxbrew/` → suggest `brew upgrade
    trackness/tap/em-dee`; refuse self-update.
  - Anything else → proceed with self-update.
- *Windows:*
  - Path containing `\go\bin\` (covers both `%USERPROFILE%\go\bin\`
    and `%GOPATH%\bin\` patterns) → suggest `go install ...`; refuse
    self-update.
  - Homebrew detection does not apply (Homebrew is not a Windows
    install path).
  - Anything else → proceed with self-update.

A `--force` override flag is **deferred to v2**.

**Security**:

- HTTPS-only for all downloads.
- SHA256 verification against the release's `checksums.txt` is
  mandatory. Mismatch aborts the update and leaves the existing binary
  in place.
- GPG / sigstore signature verification is **deferred to v2**. The v1
  posture (HTTPS + SHA256 from a GitHub Release) is documented as the
  baseline in `CLAUDE.md` so future maintainers know what to harden.

**Failure modes**:

- No network → `network unavailable; try again later`, exit non-zero.
- Already on latest → `you are on the latest version (vX.Y.Z)`, exit 0.
- Checksum mismatch → abort with a clear error; binary unchanged.
- Insufficient permissions to overwrite binary → suggest re-running
  with elevated permissions (`sudo` on unix), exit non-zero.
- GitHub API rate-limited → suggest setting `GITHUB_TOKEN` env var,
  exit non-zero.

### 12.7 Version embedding

`cmd/em-dee/main.go` declares:

```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```

Populated at build time:

- `goreleaser` sets these from the tag, commit, and build date.
- Local `task build` injects `version=dev-<git-sha>` so manual builds
  report something useful and the self-update path can compare
  intelligibly.

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

- **Module path**: `github.com/trackness/em-dee` — owner to be filled in
  before module init.
- **Binary name**: `em-dee`. The hyphenated form is intentional and
  matches the project name; `go install
  github.com/trackness/em-dee/cmd/em-dee@latest` produces a binary
  called `em-dee` because Go derives the binary name from the final
  path segment.
- **Env vars**: no `EM_DEE_*` env vars are read in v1. All
  configuration is via flags. If env-var support is added later, the
  prefix `EM_DEE_` is reserved.

## 15. Deferred to plan or v2

- Exact `.md` block content per option (to be drafted during
  implementation; see §13).
- `.claude/settings.local.json` hook contents (soft-convention; see
  §10.3).
- Whether to publish a Homebrew tap at first release (tap repo
  `homebrew-trackness-tap` to be created when enabled).
- `--strict-review` flag (makes `warnings` also fail).
- License selection.
- `em-dee update --force` to override install-method detection.
- GPG / sigstore signature verification on self-update.
- Back-navigation across forms 1 and 2 in the interactive flow.
- Env-var configuration (the `EM_DEE_*` prefix is reserved but no
  env vars are read in v1).

## 16. Open questions

None at the time of writing that block planning. Items the spec
explicitly defers are listed in §15; anything that surfaces during
spec review is folded back into the relevant section above.
