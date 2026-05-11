# em-dee — Implementation Plan

**Date**: 2026-05-11
**Spec**: `docs/superpowers/specs/2026-05-11-em-dee-design.md`

This plan decomposes the em-dee design spec into bite-sized, ordered,
testable tasks. Each task is small enough to be completed cleanly in a
single subagent dispatch (typically 30–90 minutes of focused work for
someone familiar with the spec). Phases are arranged so each one
unlocks the next without producing throwaway scaffolding.

Every task in this plan honors the four operating principles in
`CLAUDE.md`:

1. Don't assume; surface tradeoffs.
2. Minimum code that solves the problem.
3. Touch only what you must.
4. Define success criteria. Loop until verified.

Task-level notes:

- **Spec refs** point at sections of the design spec.
- **Verification** is mechanical and concrete. "Looks good" is never
  acceptable.
- **TDD-favored** tasks call out writing the test first.
- **Risks** are listed only where a non-obvious failure mode exists.
- **Open implementation choices** that don't have one correct answer
  are surfaced in the relevant task as a tradeoff, not hidden.

A final "Open items for the implementer to decide" section at the end
collects the genuinely-human choices (module owner, license, etc.) per
spec §15.

---

## Phase 0 — Bootstrap

Bring the repo from "design + CLAUDE.md only" to "an empty Go module
that builds and a Taskfile that runs."

### Task 0.1: Initialize Go module

**Depends on**: (none)
**Spec refs**: §3.1, §14
**Steps**:
- Decide the module path. Spec §14 says
  `github.com/<owner>/em-dee` with owner deferred. For now, pick a
  placeholder owner (the implementer's GitHub handle is the obvious
  choice) and document it in the "Open items" section of this plan
  so the rename, if needed before first release, is a one-line edit
  to `go.mod` plus a grep across the tree.
- `go mod init github.com/<owner>/em-dee`.
- Set the Go version in `go.mod` to `1.22` per §3.1.
- Commit `go.mod` (no `go.sum` yet; no deps).
**Verification**: `go build ./...` succeeds (trivially — no packages
yet). `go.mod` declares the right module path and Go version.
**Risks**: Module path rename later requires a global path rewrite.
**Mitigation**: Pick the realistic owner now; spec §15 calls this out
as deferred, but doing it once correctly is cheaper than doing it
twice.

### Task 0.2: Create `Taskfile.yml` with stub targets

**Depends on**: 0.1
**Spec refs**: §10.2
**Steps**:
- Create `Taskfile.yml` (v3 schema) with these targets:
  - `verify`: runs `gofmt -l` (fail if any output), `go vet ./...`,
    `go test ./...`.
  - `build`: runs `go build -o bin/em-dee ./cmd/em-dee` — will be
    wired up after Task 0.4 creates the entrypoint. Until then this
    target can echo a `not-yet-implemented` message that the
    verification step deliberately accepts, or be added later. Prefer
    adding it now with the real command and accepting that it fails
    until 0.4 lands; that's the smaller change.
  - `release`: invokes `goreleaser release --clean` (gated on
    `.goreleaser.yaml` from Phase 6).
  - `golden-update`: runs the golden regenerator helper (gated on
    Phase 2 — leave a stub that exits non-zero with a clear message
    for now).
- Document a one-line preamble in the Taskfile pointing at this plan
  and the spec.
**Verification**: `task verify` exits 0 against the empty package
set; `task --list` shows the four targets.
**Risks**: `task build` will not work until 0.4 lands. That is
acceptable and noted; do not add a placeholder `main.go` here — keep
the task isolated.

### Task 0.3: Create the empty package skeleton

**Depends on**: 0.1
**Spec refs**: §3.2
**Steps**:
- Create empty directories:
  - `cmd/em-dee/`
  - `internal/cli/`
  - `internal/registry/`
  - `internal/render/`
  - `internal/tui/`
  - `internal/review/`
  - `templates/`
  - `testdata/golden/`
- Each Go package directory gets a `doc.go` with a `package <name>`
  declaration and a one-line `// Package <name> ...` comment so the
  module compiles and `go vet` doesn't complain about empty dirs.
  No other code.
- `templates/` and `testdata/golden/` are content directories — do
  not add Go files there.
**Verification**: `go vet ./...` clean; `go build ./...` succeeds.
Directory tree matches §3.2 exactly (sans content).
**Risks**: None.

### Task 0.4: Minimal `cmd/em-dee/main.go` with version-embedding vars

**Depends on**: 0.3
**Spec refs**: §3.3, §12.7
**Steps**:
- Replace `cmd/em-dee/doc.go` with a real `main.go`:
  ```go
  package main

  import "<module>/internal/cli"

  var (
      version = "dev"
      commit  = "none"
      date    = "unknown"
  )

  func main() {
      cli.Execute(version, commit, date)
  }
  ```
- Add a stub `cli.Execute(version, commit, date string)` in
  `internal/cli/` that, for now, simply prints "em-dee
  <version>". Cobra wiring lands in Phase 3. The stub exists so
  `task build` works and so the version-embedding shape is locked in
  early.
**Verification**: `task build` produces `bin/em-dee`; `./bin/em-dee`
prints `em-dee dev`. `go vet ./...` clean.
**Risks**: Locking in the `Execute(version, commit, date)` signature
now constrains Phase 3's cobra wiring — but the signature is what
§12.7 dictates, so this isn't a free design choice.

---

## Phase 1 — Registry (data plane, no I/O beyond embed)

Stand up the typed registry layer that owns all template-filesystem
knowledge. Every subsequent phase depends on this. Tests come first
where the contract is mechanical.

### Task 1.1: Define the `Registry` and `Picks` types

**Depends on**: 0.3
**Spec refs**: §3.3, §4.1, §4.2, §5.1
**Steps**:
- In `internal/registry/types.go`, define:
  - `type Option struct { ID, DisplayName, Description, File string }`.
  - `type Pick string` for "single", "multi" with two named consts.
  - `type Category struct { Path string; DisplayName string; Pick Pick;
    Required bool; Default any; Options []Option; Subcategories
    map[string][]*Category }` — `Subcategories` is only populated for
    the `language` category, keyed by language option id, value =
    ordered list of that language's sub-categories.
  - `type Registry struct { Categories []*Category }` — top-level
    categories in render order (`10-language`, `20-infra`, …).
  - `type Picks struct { Values map[string]*Value }` where `Value` is
    a tagged union exposing `Single *string` and `Multi *[]string`
    (tri-state per §3.3 / §5.1). Key form is `<category-id>` for
    top-level, `<lang-id>.<category-id>` for language-nested.
- Add helper constructors / accessors only where the rest of the
  plan needs them (don't speculatively add setters).
**Verification**: Package compiles; `go vet` clean; a one-line
godoc on every exported symbol.
**Risks**: Choosing `map[string]*Value` vs typed fields. **Tradeoff**:
typed fields are clearer but require an enum of category ids in code,
which we want to keep in YAML only. The map keeps the registry
data-driven. Use the map.

### Task 1.2: Embed the templates filesystem and walk it

**Depends on**: 1.1
**Spec refs**: §3.1, §3.2, §4.1
**Steps**:
- In `internal/registry/registry.go`, add `//go:embed all:templates/**`
  on a `var templatesFS embed.FS`. The directive lives in the
  registry package; the `templates/` directory itself is at repo
  root.
- Decision point: embed via `//go:embed all:templates/**` (which
  captures dotfiles and underscored files) so that future `_index.yaml`
  files are included. Document this in a comment.
- Add `Load() (*Registry, error)` that walks the filesystem and
  builds the `Registry`. For Task 1.2 we only need the walk skeleton
  — wiring `_index.yaml` parsing is the next task. Returning an
  empty Registry for an empty `templates/` is fine.
**Verification**: A test in `registry_test.go` calls `Load()` against
the embedded (empty) filesystem and asserts no error, `len(Registry.
Categories) == 0`. Tests pass.
**Risks**: `//go:embed` path semantics — directory must exist at
build time even if empty. **Mitigation**: keep a `.gitkeep` in
`templates/` so the embed compiles.

### Task 1.3: Parse `_index.yaml` into typed Categories (TDD)

**Depends on**: 1.2
**Spec refs**: §4.2, §8.1
**Steps**:
- Add `gopkg.in/yaml.v3` to `go.mod`.
- **Test first**: in `internal/registry/registry_test.go`, set up
  one or more fixtures under `internal/registry/testdata/` (separate
  from the embedded production `templates/` tree) with hand-written
  `_index.yaml` content covering: single-pick with default, multi-
  pick with default, multi-pick with no default, language category
  with `required: true` no default.
- Implement an internal `parseIndex(fs.FS, path string) (*Category,
  error)` that parses one `_index.yaml` and returns a typed
  `Category`. Use a separate yaml-tagged DTO struct internal to the
  package; map it onto `Category` (the public type).
- Wire `Load()` to descend the embedded FS, calling `parseIndex` at
  each directory and recursing into language subtrees per §4.1.
- The walk must respect the `^[0-9]{2}-[a-z][a-z0-9-]*$` folder name
  pattern for category folders so order is deterministic. Sort
  children by folder name (lexicographic on the `NN-` prefix gives
  numeric order for 00–99).
**Verification**: Tests pass against the in-package testdata
fixtures, exercising both single and multi pick, default presence /
absence, and the language nesting. `task verify` clean.
**Risks**: yaml.v3 strict-mode tradeoff. **Tradeoff**: strict mode
(`KnownFields(true)`) catches typos in `_index.yaml`; lenient mode
is friendlier to schema evolution. **Decision**: use strict mode —
manifest hygiene already requires the schema to be exact.

### Task 1.4: Manifest hygiene validation (TDD)

**Depends on**: 1.3
**Spec refs**: §9.1
**Steps**:
- **Test first**: in `internal/registry/validate_test.go`, define a
  table-driven test where each row is a deliberately-broken
  in-memory fixture (a `fstest.MapFS`) plus the expected error
  substring. Cover every rule in §9.1:
  - Bad folder name pattern.
  - Missing `_index.yaml`.
  - `options[].file` doesn't exist on disk.
  - Orphan `.md` (except `base.md` under a language).
  - Language sub-folder missing `base.md`.
  - `pick` value not in `{single, multi}`.
  - Missing `display_name` (category or option).
  - Option id not matching `^[a-z][a-z0-9-]*$`.
  - Duplicate option id.
  - Single-pick default that doesn't reference a real id.
  - Multi-pick default with a non-existent id or a duplicate.
  - `templates/10-language/_index.yaml` has `default` set (forbidden
    by §9.1).
  - `templates/10-language/_index.yaml` has `required != true`,
    `pick != single`, or `default` set (each one its own test row).
- Implement `Validate(*Registry, fs.FS) error` in
  `internal/registry/validate.go` that runs all checks and returns a
  joined error (use `errors.Join`) so a single run surfaces every
  failure rather than one at a time.
- Wire `Load()` to call `Validate()` and return the error if any.
**Verification**: Every test row passes (each broken fixture
produces its specific error; the one valid baseline fixture
parses clean). `task verify` clean.
**Risks**: `errors.Join` chains can be noisy in CI output. **
Mitigation**: include the offending path in every wrapped error so
the noise is at least actionable.

### Task 1.5: `ApplyDefaults` (TDD)

**Depends on**: 1.4
**Spec refs**: §3.3, §5.1, §8.2, §8.3
**Steps**:
- **Test first**: in `internal/registry/defaults_test.go`:
  - Unset category with a default → filled in with the default.
  - Explicitly-empty category (non-nil pointer, empty value) → left
    alone.
  - Already-chosen category → left alone.
  - Unset category with no default → still unset.
  - Multi-pick default applied as a slice.
  - Required-with-default branch (forward-compat) — exercise by
    constructing a registry with a synthetic required category that
    has a default (note in the test comment that this branch is not
    exercised by the v1 catalog but is part of the contract).
- Implement `ApplyDefaults(picks Picks, reg *Registry) Picks` —
  pure function, no I/O.
**Verification**: All test rows pass; mutation is non-destructive
(the input Picks is not mutated; the returned Picks is a new value).
**Risks**: Accidentally mutating shared pointers. **Mitigation**:
explicit deep-copy in the implementation; one test asserts the input
is unchanged after the call.

### Task 1.6: `ResolveSelection(map[string]any) (Picks, error)` (TDD)

**Depends on**: 1.5
**Spec refs**: §5.1, §9.2
**Steps**:
- **Test first**: in `internal/registry/resolve_test.go`:
  - Single-pick key with a string value → tri-state single set.
  - Multi-pick key with `[]string` value → tri-state multi set.
  - Multi-pick key with comma-separated string → also accepted
    (this is how cobra delivers `--infra=docker,kubernetes` if we
    use `StringSlice` flags; clarify in Task 3.4 whether cobra
    provides a slice or a string — for now, accept both forms here
    so the resolver is the single source of truth).
  - Empty string for single-pick → "explicit none".
  - Empty `[]string` (length 0) → "explicit none".
  - Unknown category id → hard error.
  - Unknown option id within a category → hard error.
  - Required category with empty value → hard error.
  - Required category omitted → no error here (defaults / interactive
    handle it; resolution itself doesn't enforce required-but-omitted).
  - Multi-pick determinism: input list `[kubernetes, docker]` is
    normalised to declaration order — verify against the registry's
    option ordering.
- Implement `ResolveSelection(reg *Registry, m map[string]any) (Picks,
  error)`. This is the **shared entry point** for CLI flag handling
  and golden-fixture loading (§9.2).
**Verification**: All rows pass. `task verify` clean.
**Risks**: The interface between cobra and `ResolveSelection`
(string vs []string for multi-pick flags). **Tradeoff**: documented
above; accepting both keeps the resolver permissive without
spreading parsing logic across packages.

---

## Phase 2 — Render and golden fixtures

Pure rendering. After this phase, em-dee can produce a CLAUDE.md
from a `Picks` value, and the golden infrastructure is in place to
keep that output stable.

### Task 2.1: `render.Render(*Registry, Picks) ([]byte, error)` (TDD)

**Depends on**: 1.6
**Spec refs**: §4.4
**Steps**:
- **Test first**: in `internal/render/render_test.go`, add at
  minimum:
  - A scenario where every category is populated; verify the order
    is exactly §4.4.
  - A scenario where optional categories are unset; verify those
    blocks do not appear and no spurious blank lines are emitted.
  - A multi-pick determinism scenario: input `[kubernetes, docker]`
    and `[docker, kubernetes]` produce byte-identical output.
  - Blocks separated by exactly `\n\n`. No leading / trailing
    whitespace beyond what the blocks contain.
- Implement `Render(reg *Registry, picks Picks) ([]byte, error)`:
  - Walk the registry in render order.
  - For language: emit `base.md`, then walk language sub-categories.
  - For each chosen option, read its `.md` content from the embedded
    FS (registry exposes a helper for this — add it in this task).
  - Multi-pick categories: emit options in registry declaration order
    regardless of the order they appear in `Picks`.
  - Join with `\n\n`.
- The function is pure: no `os` calls, only the embedded FS via the
  registry helper.
**Verification**: Unit tests pass; output is byte-equal across input
permutations on the multi-pick determinism test.
**Risks**: Trailing newline handling. **Mitigation**: lock the
contract in a test — the rendered output ends with exactly one `\n`
(or zero — pick one and document; preferring "exactly one trailing
newline" so generated files match unix convention; if any block
already ends with `\n`, the join must not double it).

### Task 2.2: Golden fixture loader and `task golden-update`

**Depends on**: 2.1
**Spec refs**: §9.2, §10.2
**Steps**:
- In `internal/render/golden_test.go`, add `TestGolden` that:
  - Walks `testdata/golden/` for every `<scenario>/` directory.
  - Reads `selection.yaml`, runs `registry.ResolveSelection` on it
    (the shared entry point — see §9.2 in the spec; this is the
    explicit anti-drift guarantee).
  - Calls `render.Render`.
  - Asserts byte-equal against `expected.md`. If
    `GOLDEN_UPDATE=1` is set, write `expected.md` instead of
    asserting (this is the regen path).
- Add `cmd/golden-update/main.go` (a tiny internal tool) **or**
  simply rely on `GOLDEN_UPDATE=1 go test ./internal/render/...`.
  Pick the env-var path — simpler, no extra binary to maintain.
- Wire `task golden-update` to
  `GOLDEN_UPDATE=1 go test ./internal/render/...`.
- Add an initial smoke fixture under `testdata/golden/smoke/` with
  `selection.yaml` and `expected.md` for one scenario. Use a
  language whose blocks are placeholder TODOs is fine — but per
  §9.2, golden coverage is restricted to finalised content; mark
  this initial fixture as `smoke` and gate it on placeholder
  content existing. Real per-language fixtures land in Phase 7
  alongside finalised content.
**Verification**: `task verify` runs golden tests and they pass.
`task golden-update` regenerates the smoke fixture in place; the
diff is empty if no render-logic changes have been made.
**Risks**: Easy to accidentally regen goldens in CI. **Mitigation**:
the env-var gating means only deliberate local action triggers
regen; document this in `CLAUDE.md` as an anti-pattern (already in
§10.1 of spec — call it out in Phase 8's CLAUDE.md task).

---

## Phase 3 — Non-interactive CLI

Cobra root + the read-only subcommands and a flag-driven
`generate`. After this phase, em-dee works end-to-end without a TUI.

### Task 3.1: Cobra root command and `version` subcommand

**Depends on**: 0.4, 1.6
**Spec refs**: §5, §12.7
**Steps**:
- In `internal/cli/root.go`, replace the stub `Execute` with a
  cobra root command. The signature stays
  `Execute(version, commit, date string)` so `main.go` is unchanged.
- The root command, when invoked with no subcommand, defers to
  `generate` (Task 3.4) — but in this task, leave a TODO and only
  wire the `version` subcommand.
- In `internal/cli/version.go`, add `em-dee version` printing
  `em-dee <version> (commit <commit>, built <date>)` and
  `em-dee version --json` printing the JSON shape mandated by
  §12.6: `{ "version", "commit", "date", "platform" }`. `platform`
  is `runtime.GOOS+"/"+runtime.GOARCH`.
- Add a cobra-args test using `cmd.SetArgs(...)` and
  `cmd.SetOut(...)` to capture output.
**Verification**: `./bin/em-dee version` and `... version --json`
both succeed and match the expected formats. Unit test passes.
**Risks**: None.

### Task 3.2: `em-dee list` (human + `--json`)

**Depends on**: 3.1
**Spec refs**: §5, §8.4
**Steps**:
- In `internal/cli/list.go`, add `em-dee list` that walks the
  registry and prints a human-readable tree. Annotate defaulted
  options with `(default)` per §8.4.
- `--json` produces a stable, documented JSON representation of the
  registry (categories with options, defaults marked). This is the
  serialised form of the `Registry` for tooling consumers.
- Test using `cmd.SetArgs / SetOut`. Snapshot-test the JSON output
  against a known fixture (separate from golden render fixtures).
**Verification**: Unit test asserts both human and JSON output
shapes. `./bin/em-dee list` prints a sensible tree against the
final v1 catalog (or whatever placeholder catalog exists at this
point).
**Risks**: JSON shape becomes a contract for downstream tooling.
**Mitigation**: document the shape in a comment on the type so the
contract is visible.

### Task 3.3: `em-dee show <ref>` with dotted-ref grammar

**Depends on**: 3.2
**Spec refs**: §5.1
**Steps**:
- In `internal/cli/show.go`, parse the dotted reference per §5.1's
  examples:
  - `language.<lang>` → `templates/10-language/<lang>/base.md`.
  - `<lang>.<cat>.<opt>` → language-nested option block.
  - `<cat>.<opt>` → top-level category option block.
- Implement the resolver as a walk over the registry, not by
  string-manipulating filesystem paths.
- Errors: unknown ref → exit non-zero with a clear message
  ("`em-dee show <ref>`: no block found for 'foo.bar'; did you
  mean ...?" — the suggestion is optional, but the clear error
  message is not).
- Print the raw `.md` content of the resolved block to stdout.
**Verification**: Unit tests cover all four reference forms from
§5.1 plus the unknown-ref error path. `./bin/em-dee show
infra.docker` prints the docker block when content lands.
**Risks**: Ambiguity between `<cat>.<opt>` and `<lang>.<sub>` when
a future language id happens to match a top-level category id.
**Tradeoff**: in v1 there is no collision (`go` / `python` /
`typescript-node` / `rust` vs `infra` / `ci` / `tooling`). Resolver
walks `language.*` first when the first segment is a known language;
otherwise treats first segment as a top-level category. Document
the disambiguation rule in a code comment.

### Task 3.4: `em-dee generate` non-interactive (flags only)

**Depends on**: 3.3, 2.1
**Spec refs**: §5, §5.1, §5.3, §6
**Steps**:
- In `internal/cli/generate.go`, define `em-dee generate` with the
  flag set from §5:
  - `--language=<id>` (single-pick).
  - For each language, `--<lang-id>.<category-id>=<id>` (single-pick).
  - For each top-level cross-cutting multi-pick:
    `--<cat-id>=<csv-of-ids>` — use `StringSlice` (cobra accepts
    comma-separated via the slice flag) **or** plain `String` and let
    `ResolveSelection` do the split. **Tradeoff**: `StringSlice` is
    idiomatic cobra and gives `[]string` directly; plain `String`
    means resolver is the single source of truth. **Decision**:
    plain `String` — keeps Task 1.6's resolver as the only place
    that knows how multi-pick values are spelled.
  - Behaviour flags: `--out=<path>`, `--force`, `--dry-run`,
    `--use-defaults`, `--no-review`, `--review`, `--review-out=<p>`,
    `--review-timeout=<dur>`. Wire the flag definitions but defer
    review behaviour to Phase 5; for this task, `--no-review` /
    `--review` are accepted but no review runs yet.
- Flag-set construction is dynamic: enumerate registry categories
  and language subtrees, registering one flag per category. This
  keeps the CLI surface data-driven per §5.1.
- Build the `map[string]any` for `ResolveSelection`, call it, then
  `ApplyDefaults`, then validate required categories (per §5.1's
  "Required category resolution"), then call `render.Render`.
- File writing: implement existing-file handling per §6:
  - Default: error if target exists.
  - `--force`: rename to `CLAUDE.md.bak.<unix-ts>` in the same
    directory, print backup path to stderr.
  - `--out`: same rules apply at the alternate path.
  - `--dry-run`: print to stdout, skip both the existence check and
    (future) review.
- Tests: cover at least:
  - Happy path: `--language=go --use-defaults --dry-run` produces
    non-empty output.
  - Unknown option id → hard error.
  - Required-empty (`--language=`) → hard error.
  - Existing file without `--force` → error.
  - Existing file with `--force` → backup created.
**Verification**: All unit tests pass. End-to-end smoke:
`./bin/em-dee generate --language=go --use-defaults --dry-run`
prints rendered markdown that matches what the same `Picks` produces
through the registry+render path directly.
**Risks**: huh-style dynamic field per-language: in non-interactive
mode we register *all* language flags up-front (`--go.framework`,
`--python.framework`, etc.). Users will see a long `--help` but
that's fine; the alternative — gating flag registration on
`--language` — fights cobra's static flag-set model.

### Task 3.5: Default subcommand wiring (running `em-dee` runs `generate`)

**Depends on**: 3.4
**Spec refs**: §5
**Steps**:
- Make `em-dee` (no subcommand) execute `generate`. In cobra, this
  is typically `rootCmd.RunE = generateCmd.RunE` or setting
  `rootCmd` itself to behave as the default command. **Tradeoff**:
  another common pattern is `cobra.Command.SuggestionsMinimumDistance`
  + a custom `Args` validator; the simpler approach is to set
  `rootCmd.RunE` and copy the flag set. **Decision**: register
  `generate` as a real subcommand AND assign its `RunE` to the
  root's `RunE`, sharing flag definitions via a helper. Document
  the choice in a comment.
- Tests: `cmd.SetArgs([]string{"--language=go", "--use-defaults",
  "--dry-run"})` against the root command produces the same output
  as `cmd.SetArgs([]string{"generate", ...})`.
**Verification**: `./bin/em-dee --language=go --use-defaults --dry-run`
and `./bin/em-dee generate --language=go --use-defaults --dry-run`
print identical output.
**Risks**: cobra subtleties around args-vs-flags when root has its
own flags. **Mitigation**: integration test above.

### Task 3.6: `em-dee update --check` skeleton

**Depends on**: 3.1
**Spec refs**: §12.6
**Steps**:
- In `internal/cli/update.go`, register the `update` subcommand
  with `--check`. For this task, **only** the check path is
  implemented (read-only, no network mutation, simpler to land).
- Implement install-method detection per §12.6 (Unix and Windows
  paths). Refuse if the binary lives under a go-install or homebrew
  path; print the suggested command for those install methods and
  exit 0.
- Otherwise, query the GitHub Releases API for the latest tag:
  `GET https://api.github.com/repos/<owner>/<repo>/releases/latest`.
  Honor `GITHUB_TOKEN` env var if set (per §12.6's rate-limit
  failure-mode note).
- Compare against the embedded build version. Exit codes:
  `0` = up-to-date, `1` = update available, `2` = error per §12.6.
- Tests: install-method detection — table-driven on synthetic
  paths covering each Unix and Windows rule from §12.6. Network
  test isolated behind an HTTP client interface so the unit test
  injects a stub `http.RoundTripper`.
**Verification**: `./bin/em-dee update --check` (against a real
GitHub repo, once one exists) returns the right exit code. Unit
tests pass for install-method detection rules. The actual `update`
(no `--check`) is added in Phase 6 (Task 6.4) after release pipeline
exists.
**Risks**: GitHub API rate-limit on unauth requests (60/h). **
Mitigation**: documented; `GITHUB_TOKEN` raises it. Failure-mode
exit code 2 per §12.6.

---

## Phase 4 — Interactive TUI

Two sequential huh forms, lipgloss success line.

### Task 4.1: Add huh v2 + lipgloss dependencies and styles

**Depends on**: 3.5
**Spec refs**: §3.1, §5.2, §7.4
**Steps**:
- `go get charm.land/huh/v2 github.com/charmbracelet/lipgloss`. Pin
  versions in `go.mod` — pick the latest stable at implementation
  time and document the pinned version in this task's commit
  message. **Tradeoff**: latest gives newest fixes; pinning a tag
  protects against breakage. **Decision**: pin a specific tag.
- In `internal/tui/styles.go`, define the lipgloss styles used by
  both the success line and the review presentation:
  - Verdict markers: `verdictOK` (green ✓), `verdictWarn` (yellow
    ⚠), `verdictProblem` (red ✗).
  - Severity colors per §7.4 (info=neutral, warning=yellow,
    error=red).
  - Section / issue / suggestion styles.
- No form construction yet — just the styles plus a test that
  asserts each style renders an expected ANSI prefix (a thin
  smoke-test against lipgloss's `Render`).
**Verification**: `go test ./internal/tui/...` passes; `go vet`
clean.
**Risks**: huh v2 still-evolving API. **Mitigation**: pinning, plus
the deliberate decision in §5.2 to use two sequential `.Run()` calls
rather than dynamic fields, which sidesteps huh's most volatile
behaviour.

### Task 4.2: Form 1 — language selection

**Depends on**: 4.1
**Spec refs**: §5.2
**Steps**:
- In `internal/tui/form.go`, add `RunLanguageForm(reg *Registry)
  (string, error)` that constructs a `huh.Form` with one
  `huh.Group` containing one `huh.Select[string]` for the language.
- Options: every language in the registry, in registry order.
- No pre-selected value.
- `.Validate(func(s string) error { if s == "" { return
  errors.New("language is required") }; return nil })` as belt-and-
  braces per §5.2 paragraph 1.
- Ctrl-C / Esc returns a sentinel error (`ErrCancelled`) that the
  caller distinguishes from real errors.
- A smoke test that constructs the form for a non-empty registry
  without panicking (per §11 — full TUI is not unit-tested).
**Verification**: Smoke test passes. Manual: `./bin/em-dee` (after
Task 4.4) shows a single-select language form and accepts a
selection.
**Risks**: huh v2's `Select` highlight-vs-commit semantics per
§5.2 paragraph 1. **Mitigation**: rely on the explicit Enter-to-
commit behaviour as documented; the `.Validate` is belt-and-braces.

### Task 4.3: Form 2 — rest, plus confirm group

**Depends on**: 4.2
**Spec refs**: §5.2
**Steps**:
- In `internal/tui/form.go`, add `RunRestForm(reg *Registry, lang
  string, initial Picks) (Picks, error)` that constructs a `huh.Form`
  whose groups are, in order:
  - The chosen language's sub-categories (10-framework, 20-logging,
    30-testing, 40-deps if present), each as one
    `huh.Group`. Single-pick categories use `huh.Select`; multi-pick
    use `huh.MultiSelect`.
  - The cross-cutting categories (`20-infra`, `30-ci`,
    `40-tooling`), each as one `huh.Group`.
  - A final confirm group with a `huh.Confirm` whose description
    lists the blocks that will be rendered, in render order.
- Default-seeding: per §5.2 paragraph 2, defaults pre-populate
  bound variables before `.Run()` so Enter accepts the default. Use
  `ApplyDefaults` to compute what to seed; the bound variable for
  each field is initialised from the seeded `Picks`.
- Optional categories without a default present an empty selection
  state (no pre-population).
- Ctrl-C / Esc → `ErrCancelled`.
- Smoke test constructs the form for each language in the registry.
**Verification**: Smoke tests pass for every language. Manual: the
interactive flow renders, accepts defaults via Enter, and the
confirm group shows the right block list.
**Risks**: huh v2's behaviour when a `MultiSelect`'s bound variable
is pre-populated. **Mitigation**: smoke test plus a manual run; if
huh v2 changes this behaviour, the test catches the panic.

### Task 4.4: Wire interactive flow into `generate`

**Depends on**: 4.3, 3.4
**Spec refs**: §5.2, §5.3
**Steps**:
- In `internal/cli/generate.go`, detect "no flags provided that
  identify a category" → interactive mode. The exact predicate:
  if `--language` is not set AND no language-namespaced category
  flag is set AND no cross-cutting category flag is set AND
  `--use-defaults` is not set → run interactive. **Tradeoff**: this
  predicate has a few edge cases (e.g. `--dry-run` alone — is that
  interactive or non-interactive?). **Decision**: `--dry-run` alone
  is still interactive (the user wants to preview); they get
  prompted and the output goes to stdout. Document the rule.
- `--use-defaults` runs form 1 (language) interactively, then
  applies defaults for the rest non-interactively (per §5.3).
- Compose: `RunLanguageForm` → set Picks → `RunRestForm` (with
  defaults seeded) → set Picks → `ApplyDefaults` → existing-file
  check → write → success line.
- Success line per §5.2 paragraph 7: `wrote CLAUDE.md (N blocks,
  N.NN KB)` via the lipgloss styles from Task 4.1.
**Verification**: Manual end-to-end run: `./bin/em-dee` produces a
CLAUDE.md and prints the success line. `./bin/em-dee
--use-defaults` prompts only for language.
**Risks**: Predicate for "interactive mode" subtly wrong.
**Mitigation**: explicit unit tests on the predicate, separate
from the TUI itself.

---

## Phase 5 — Claude review

Subprocess invocation, three-tier parse, lipgloss presentation,
exit-code mapping, failure-mode handling.

### Task 5.1: Embedded review prompt template

**Depends on**: 0.3
**Spec refs**: §7.1
**Steps**:
- Create `internal/review/prompt.md` with the review prompt per
  §7.1's contract: instructs Claude to respond with a single JSON
  object matching §7.2's schema, no markdown fences, no preamble.
  The prompt must be specific about the verdict enum
  (`ok`/`warnings`/`problems`) and severity enum
  (`info`/`warning`/`error`).
- Embed in `internal/review/review.go` via `//go:embed prompt.md`.
**Verification**: A trivial test asserts the embedded string is
non-empty and contains the expected schema markers (verdict
key, severity key, etc.).
**Risks**: Prompt changes invalidate the contract. **Mitigation**:
the embedded prompt is part of the binary; any change goes through
PR review.

### Task 5.2: Claude subprocess invocation interface (TDD)

**Depends on**: 5.1
**Spec refs**: §7.1, §7.6, §11
**Steps**:
- In `internal/review/review.go`, define an interface:
  ```go
  type runner interface {
      Run(ctx context.Context, prompt string) (stdout []byte,
          stderr []byte, err error)
  }
  ```
  The default implementation shells out to `claude -p <prompt>
  --output-format=json` via `os/exec.Command`. The prompt is passed
  as a single argv arg per §7.1 (no stdin).
- Provide a `LookPath` check up-front; missing-claude returns a
  sentinel error.
- Timeout via `context.WithTimeout`; default 60s, overridable per
  `--review-timeout` (the flag is plumbed in Task 5.5).
- Tests use a stub `runner` (per §11) for the parsing and
  presentation tests.
**Verification**: Unit tests pass. Manual: with `claude` on PATH,
a small smoke test (no fixtures yet) constructs the runner and
gets a JSON response back.
**Risks**: ARG_MAX on platforms where it's smaller than expected.
**Mitigation**: §7.1 documents the boundary; if it's exceeded in
practice we add a stdin fallback in v2.

### Task 5.3: Three-tier JSON parse (TDD)

**Depends on**: 5.2
**Spec refs**: §7.2, §7.3
**Steps**:
- In `internal/review/parse.go`, implement `Parse([]byte)
  (ReviewResult, error)`:
  - Tier 1: strict JSON unmarshal against §7.2's schema. Validate
    `verdict` is one of the enum, severities are valid, etc.
  - Tier 2: lenient — find first `{` and last `}` in input,
    re-parse.
  - Tier 3: return a `ReviewResult` with `Verdict == "unstructured"`
    and `Raw == <input>` so the presentation and `--review-out`
    layers can branch on it.
- **Test first**: fixtures under `internal/review/testdata/`:
  - Clean JSON.
  - JSON wrapped in ```json ... ``` fences (tier 2 catches).
  - JSON with preamble ("Sure, here's the review: { ... }").
  - Malformed JSON (broken braces) — tier 3.
  - Valid JSON shape but unknown verdict — tier 3 (parse succeeded
    but validation didn't; document the design call: tier 1's
    "validate" includes schema correctness, so this falls to tier 3
    rather than being a silent corruption). **Tradeoff**: alternative
    is to surface a richer "structured-but-invalid" tier; reject
    that for v1 — fewer code paths.
**Verification**: All test rows pass.
**Risks**: Lenient extraction can mis-anchor on JSON-in-quoted-
strings. **Mitigation**: documented as best-effort per §7.3; tier 3
catches anything that survives.

### Task 5.4: Lipgloss-rendered presentation (TDD)

**Depends on**: 5.3, 4.1
**Spec refs**: §7.4
**Steps**:
- In `internal/review/present.go`, implement `Present(io.Writer,
  ReviewResult, termWidth int)`:
  - Header per §7.4 (`── claude review ──`, then verdict marker +
    summary).
  - Issue list: `Section "<location>" — <issue>\n    →
    <suggestion>`.
  - Truncate `location` longer than 60 columns to 57 + `...`.
  - Wrap (not truncate) `issue` and `suggestion` to terminal width;
    fallback to 100 cols when width is 0/unknown.
  - Severity colors per §7.4.
  - For `verdict == "unstructured"`, render under a
    `review (unstructured)` header with the raw text.
- Pass terminal width detected via `golang.org/x/term` in the
  caller (Task 5.5); presentation itself takes `termWidth int` so
  it's deterministically testable.
- **Test first**: golden-style fixtures asserting the rendered
  output (with ANSI stripped via a helper) matches expected text
  for each verdict and the unstructured case.
**Verification**: All test rows pass. Manual: a real review with
`claude` on PATH renders correctly.
**Risks**: ANSI in tests is fragile. **Mitigation**: strip ANSI for
assertions; keep the ANSI-rendered version verifiable by eye on
manual runs.

### Task 5.5: Wire review into `generate` end-to-end

**Depends on**: 5.4, 4.4
**Spec refs**: §7.1, §7.5, §7.6, §7.7
**Steps**:
- In `internal/cli/generate.go`, after the file write, run the
  review unless `--no-review` is set or `--dry-run` is set.
- Plumb `--review-timeout` (Go duration string) into the context.
  Default 60s per §7.6.
- Failure modes per §7.6 — `claude` not on PATH, non-zero exit,
  timeout — each handled per the spec's wording; exit 0 in all
  three.
- Exit code mapping per §7.5: `ok` / `warnings` → 0;
  `problems` → 2; parse failure → 0.
- Determine terminal width via `golang.org/x/term`; pass into
  `Present`. Fallback 100 cols.
- `--review-out` per §7.7:
  - On parse tier 1 or 2: write the parsed JSON.
  - On parse tier 3: write the sentinel JSON shape exactly per
    §7.7 (`{"verdict": "unstructured", "summary": "claude review
    could not be parsed as structured JSON", "raw": "<original
    text>", "issues": []}`).
- Unit tests:
  - `--no-review` skips review entirely.
  - `--dry-run` skips review entirely.
  - Stub runner returning `problems` → exit 2.
  - Stub runner returning `ok` → exit 0.
  - Tier-3 parse → exit 0; `--review-out` contains the sentinel
    JSON shape.
  - Missing `claude` → printed note on stderr; exit 0.
  - Timeout → printed note; exit 0.
**Verification**: All unit tests pass. Manual: real review against
a generated CLAUDE.md prints presentation and exits with the right
code.
**Risks**: Exit-code shenanigans — cobra wraps `RunE` errors. **
Mitigation**: don't return an error for `problems` (which would be
exit 1 via cobra) — call `os.Exit(2)` explicitly after rendering.
Document the decision; alternatively, set
`cmd.SilenceErrors = true` and use a sentinel error type that the
root-level entry maps to exit 2. **Decision**: explicit `os.Exit(2)`
after the review presentation, because cobra's error-to-exit
mapping is exit 1 by convention and §7.5 mandates exit 2 specifically.

---

## Phase 6 — Release pipeline and self-update

Make em-dee installable, releasable, and self-updateable.

### Task 6.1: `.goreleaser.yaml`

**Depends on**: 3.6
**Spec refs**: §12.4, §12.7
**Steps**:
- Write `.goreleaser.yaml` covering:
  - `builds`: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64,
    windows/amd64.
  - `ldflags`: inject `version`, `commit`, `date` into
    `main.version`/`main.commit`/`main.date` per §12.7.
  - `archives`:
    - `name_template`: `em-dee_{{ .Os }}_{{ .Arch }}` (no version)
      per §12.4 — this is **load-bearing** for the
      `releases/latest/download/...` URL to resolve.
    - `tar.gz` for unix, `zip` for windows.
  - `checksum`: produce `checksums.txt` with SHA256.
  - `release`: github, default settings.
  - `brews`: declared but `commented out` until the tap repo
    exists per §12.4.
- Run `goreleaser check` locally and ensure config validates.
- `task release` is already wired to `goreleaser release --clean`
  from Phase 0.
**Verification**: `goreleaser check` exits 0. `goreleaser build
--snapshot --clean` produces the expected archive names without
versions in them.
**Risks**: `name_template` mis-spelling. **Mitigation**: an explicit
manual check that the snapshot's output filenames match the URL
pattern from §12.5.

### Task 6.2: CI workflow (`.github/workflows/ci.yml`)

**Depends on**: 0.2
**Spec refs**: §12.2
**Steps**:
- Define the workflow per §12.2:
  - Triggers: push to any branch + PR.
  - Matrix: Go latest stable + N−1.
  - Steps: checkout → setup-go (with module cache) → `task verify`.
  - Parallel `lint` job running `golangci-lint`.
- Pick a `golangci-lint` config: start with the bundled defaults
  plus `gofmt`, `govet`, `errcheck`, `staticcheck`. Add
  `.golangci.yml` documenting the choice.
**Verification**: Push a no-op commit on a feature branch; CI runs
green. Pre-merge: `act` (or equivalent) locally runs the workflow
without errors.
**Risks**: CI being slow. **Mitigation**: module cache; matrix
parallelism.

### Task 6.3: Release workflow (`.github/workflows/release.yml`)

**Depends on**: 6.1, 6.2
**Spec refs**: §12.3
**Steps**:
- Define the workflow per §12.3:
  - Trigger: tag push matching `v*`.
  - Steps: checkout (`fetch-depth: 0`) → setup-go → `goreleaser
    release --clean`.
  - Permissions: `contents: write` (required by goreleaser for
    GitHub Releases).
  - `GITHUB_TOKEN` is provided by Actions automatically;
    `HOMEBREW_TAP_GITHUB_TOKEN` is documented but commented out
    until the tap repo exists per §12.4.
**Verification**: Cut a `v0.0.1-rc1` tag against a feature branch
(or use `goreleaser release --skip=publish --snapshot`) — the
release succeeds and archives appear with the right naming.
**Risks**: Tag-pushing privileges. **Mitigation**: documented.

### Task 6.4: `em-dee update` (real install path)

**Depends on**: 3.6, 6.3
**Spec refs**: §12.6
**Steps**:
- Add `github.com/minio/selfupdate` (or equivalent — pin a version).
- In `internal/cli/update.go`, implement the `update` (non-`--check`)
  path per §12.6:
  1. GitHub API for latest release metadata.
  2. Download the platform archive from
     `releases/latest/download/em-dee_<os>_<arch>.<ext>`.
  3. Download `checksums.txt`; verify SHA256 — mismatch aborts and
     leaves the existing binary in place per §12.6 security.
  4. Extract the binary from the archive into a temp file.
  5. `selfupdate.Apply` (atomic replace).
  6. Print `updated <old> → <new>`.
- Install-method detection from Task 3.6 still applies: refuse self-
  update for go-install / homebrew paths.
- HTTPS only.
- Failure modes per §12.6 all handled (network, already-latest,
  checksum mismatch, permissions, rate-limit).
- Tests: parser of `checksums.txt`, archive extractor (against an in-
  memory archive fixture), and the install-method gate (already
  covered in 3.6). The end-to-end network path is exercised manually
  in Phase 8.
**Verification**: Unit tests pass. Manual: build, publish a
`v0.0.1` release, install the binary via direct-download in a fresh
location, run `./em-dee update --check` then `./em-dee update`;
binary is replaced and `version` reports the new value.
**Risks**: Atomic replace on Windows. **Mitigation**: `selfupdate`
handles this; document if the underlying lib's behaviour changes.
Permissions on `/usr/local/bin` — surface the "re-run with sudo"
message per §12.6.

---

## Phase 7 — Catalog content

Stand up the v1 catalog folders, then fill in `.md` content per
language as it stabilises. Golden coverage grows alongside.

### Task 7.1: Scaffold all category folders with `_index.yaml`s

**Depends on**: 1.4
**Spec refs**: §13
**Steps**:
- Create the directory tree per §13:
  - `templates/10-language/_index.yaml` (required, single, no
    default), with the four language options.
  - `templates/10-language/<lang>/base.md` (placeholder TODO content
    per §13's "Empty options can be initially populated with a
    one-line placeholder...").
  - `templates/10-language/<lang>/10-framework/_index.yaml` +
    `.md`s, same for 20-logging, 30-testing, 40-deps.
  - `templates/20-infra/_index.yaml` + `docker.md`, `kubernetes.md`,
    `terraform.md`.
  - `templates/30-ci/_index.yaml` + `github-actions.md`,
    `gitlab-ci.md`.
  - `templates/40-tooling/_index.yaml` + `pre-commit.md`, `mise.md`,
    `taskfile.md`, `makefile.md`.
- All `.md` files have one-line TODO content pointing back to the
  spec — enough to satisfy manifest hygiene (the file exists, is
  non-empty) but explicitly marked TODO.
**Verification**: `task verify` passes — the manifest hygiene
validator is happy with the scaffolding. `./bin/em-dee list` shows
the full v1 catalog.
**Risks**: Per §9.2, scaffolded TODO content is excluded from
golden coverage. **Mitigation**: only one golden fixture (the smoke
fixture from Task 2.2) exists at this point; per-language goldens
land alongside finalised content.

### Task 7.2: Draft real `.md` content (per-language sub-tasks)

**Depends on**: 7.1
**Spec refs**: §13, §9.2
**Steps**:
- For each language (`go`, `python`, `typescript-node`, `rust`),
  in its own sub-task:
  - Draft real content for `base.md` and each option under
    `10-framework`, `20-logging`, `30-testing`, `40-deps`.
  - As each language's content is finalised, add at least one
    golden fixture per §9.2 covering that language.
- For cross-cutting categories, draft real content for each option
  under `20-infra`, `30-ci`, `40-tooling`. These are language-
  agnostic per §2 non-goals; **do not** add language-specific
  details (anti-pattern per §10.1).
- Add the additional golden fixtures required by §9.2:
  - No optional picks.
  - All defaults.
  - Every category populated.
  - Multi-pick in reverse manifest order (locks in §4.4
    determinism).
**Verification**: `task verify` passes; goldens are byte-stable
across runs; `./bin/em-dee show <ref>` returns the real content for
each finalised block.
**Risks**: Endless drafting cycles. **Mitigation**: each language
is its own sub-task with its own merge gate; TODO content remains
valid in the meantime per §13.

---

## Phase 8 — GitHub repo setup and end-to-end verification

The final phase: produce a real release, prove the install / update
paths against it, fill in CLAUDE.md.

### Task 8.1: Repo-level `CLAUDE.md` mechanical recipes + anti-patterns

**Depends on**: 7.1, 1.4, 2.2
**Spec refs**: §10.1
**Steps**:
- Replace the placeholder sections in the existing `CLAUDE.md`
  (lines marked "filled in during implementation") with the real
  recipes from §10.1:
  - Add an option / add a category / add a language / reorder.
  - Naming rules (kebab ids, `NN-name`, `<id>.md`).
  - Anti-patterns, including the explicit "Never run `task golden-
    update` to fix a failing test in CI" rule.
  - Required commands (`task verify`, `task build`).
- Cross-check: every recipe references actual paths and actual
  files that exist after Phase 7's scaffolding.
**Verification**: A fresh reader (or a fresh Claude session) can
follow each recipe and produce a passing `task verify`. Manual
walkthrough of each recipe against the repo.
**Risks**: CLAUDE.md drifts from the actual filesystem layout.
**Mitigation**: §10.1 is the contract; if a future change requires
updating recipes, that change includes the CLAUDE.md update in the
same PR.

### Task 8.2: README.md with install paths

**Depends on**: 6.1
**Spec refs**: §12.5
**Steps**:
- Write `README.md` covering:
  - One-paragraph what-it-is.
  - The three install paths exactly per §12.5 (go install,
    curl-tar, brew — with the brew line marked "post-tap-setup").
  - Usage examples: `em-dee`, `em-dee generate --use-defaults`,
    `em-dee list`, `em-dee show infra.docker`.
  - Link to the spec and CLAUDE.md.
**Verification**: Copy-paste each install command into a fresh
shell (after first release exists) — all three work or, for brew,
documents the right pre-condition. Markdown renders correctly on
GitHub.
**Risks**: Stale commands. **Mitigation**: each install line is
copy-pasteable and verifiable; review during initial release.

### Task 8.3: LICENSE selection

**Depends on**: (none — can land any time after 0.1)
**Spec refs**: §15
**Steps**:
- Pick a license. Per §15 this is deferred to the implementer.
- Add `LICENSE` at repo root with the chosen license text.
- Add the SPDX identifier to `go.mod` is **not** standard Go
  practice; instead, add the SPDX identifier in a top-of-file
  comment in `cmd/em-dee/main.go` if needed.
**Verification**: `LICENSE` exists; GitHub auto-detects it on the
repo page.
**Risks**: License choice is a one-way door for some choices.
**Mitigation**: surfaced in "Open items" — implementer decides
deliberately.

### Task 8.4: GitHub repo creation, branch protection, initial push

**Depends on**: 6.2, 6.3, 8.1, 8.2, 8.3
**Spec refs**: §12.1
**Steps**:
- Create `github.com/<owner>/em-dee` (the owner chosen in Task 0.1
  must match).
- Push `main`.
- Configure branch protection on `main` per §12.1:
  - Require PRs.
  - Require status checks (CI workflow's jobs) to pass.
  - Require branches to be up-to-date before merging.
- Document the protection settings in this plan's commit message
  so a future operator can re-create them if needed.
**Verification**: Try to push directly to `main` — blocked.
PRs require green CI to merge.
**Risks**: Owner / org choice. **Mitigation**: surfaced as an
"Open items" entry; once chosen, this task executes.

### Task 8.5: First release (`v0.1.0`) and self-update smoke test

**Depends on**: 8.4, 6.4, 7.2
**Spec refs**: §12.1, §12.3, §12.6
**Steps**:
- Tag `v0.1.0`. Release workflow runs; archives appear at
  `releases/latest/download/...`.
- Download the binary via the `curl | tar -xz` path from §12.5 in
  a fresh directory.
- Run end-to-end smoke:
  - `./em-dee --help` shows full command tree.
  - `./em-dee version` and `./em-dee version --json` work.
  - `./em-dee generate --language=go --use-defaults --dry-run`
    prints non-empty markdown.
  - `./em-dee generate --language=go --use-defaults --out=
    /tmp/CLAUDE.md` writes the file; subsequent run errors without
    `--force`; with `--force` produces the backup.
  - `./em-dee list` and `./em-dee show infra.docker` work.
  - `./em-dee update --check` against the live release reports
    `up-to-date`.
  - Manually bump the embedded version locally to a lower value
    (e.g. `dev-old`), rebuild, and verify `--check` reports
    `update available` with exit 1; then run `update` (against the
    actual release) and verify the binary is replaced.
  - `./em-dee generate --language=go --use-defaults`
    (interactive review path) shells out to `claude` and produces
    a review.
**Verification**: Every bullet above passes. Document any deviation
in a follow-up issue.
**Risks**: A real release is a one-way door (tags don't un-tag
gracefully). **Mitigation**: pre-flight the release with
`goreleaser release --snapshot --clean` (no publish) before tagging
`v0.1.0`.

---

## Open items for the implementer to decide

These are the genuinely-human choices the spec defers to the
implementer per §15. Each is surfaced here, not silently chosen
inside a task:

1. **Module owner / GitHub org**: spec §14 leaves this blank. Pick
   before Task 0.1 (`go mod init`) — changing it later is a global
   path rewrite plus a `go.mod` change.
2. **License**: spec §15. Task 8.3 lands the chosen file; the
   choice itself is the implementer's. MIT / Apache-2.0 / BSD-3 are
   the standard candidates for a Go CLI.
3. **Pinned library versions**: huh v2, lipgloss, cobra, yaml.v3,
   selfupdate. Tasks 1.3, 3.1, 4.1, 6.4 each pin a version; pick
   the latest stable at implementation time and document the
   pinned tag in the corresponding commit message.
4. **Homebrew tap timing**: spec §12.4 leaves the
   `homebrew-<owner>-tap` repo until "first release". Decide
   whether to enable in `v0.1.0` (Task 8.5) or wait until `v0.2.x`.
5. **`.claude/settings.local.json` hook contents**: spec §10.3
   notes this is a soft-convention. Decide whether to include any
   hooks (e.g. `task verify` on `templates/**` changes) or ship the
   directory empty.
6. **Initial Go versions in CI matrix**: spec §12.2 says "latest
   stable + N−1". Pin the concrete pair at Task 6.2 time.
7. **`golangci-lint` ruleset**: Task 6.2 picks a default set; if
   the implementer wants a tighter ruleset, decide at that task.
