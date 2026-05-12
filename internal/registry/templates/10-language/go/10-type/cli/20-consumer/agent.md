### Agent consumer

The primary consumer of this CLI is a Claude skill, not a human at a
terminal. Output is an API. Every change to output shape — JSON
fields, error codes, exit-code meaning, per-command human-mode
support — is a breaking change and bumps the major version.

#### Output contract

- JSON-only. stdout carries a single JSON document per invocation,
  nothing else. No ANSI, no progress bars, no banners.
- Logs go to stderr regardless of verbosity. stdout is reserved for
  the response document.
- Errors render as the structured envelope from `Go CLI` →
  Structured errors, emitted on stderr in JSON form.
- No prompts, ever — never block on stdin, even on a TTY. Mutation
  gating uses the `--yes` / `--dry-run` mechanism from `Go CLI` →
  Mutation gating.

#### Mode handling

`--output json` (short `-o json`) is the only accepted value, and
JSON is the default when the flag is omitted. Skills should pass it
explicitly anyway so intent survives a future consumer-pick change.

- `--output human` exits 1 with error code `HUMAN_OUTPUT_NOT_SUPPORTED`.
- Any other `--output` value exits 1 with `INVALID_OUTPUT_MODE`.

#### Determinism

Output must be byte-stable across runs given identical inputs.

- Sort every collection by a stable key, named in the command's
  `--help`. Map iteration order is not stable; serialise via sorted
  slices.
- Omit timestamps from default output unless semantically required
  (an audit record's `created_at`; not a "generated at" banner).
- Preserve Go struct field declaration order in JSON encoding. Field
  order is part of the contract; reordering struct fields is breaking.

#### Introspection — the `commands` subcommand

A top-level `commands` subcommand emits the full command tree as
JSON so skills can discover the surface without parsing help text.
The emitted `CommandsOutput` shape is a versioned contract surface;
skills hard-code branches on its fields.

Top-level fields:

- `name` — the CLI binary name.
- `commands` — array of command entries (see below).
- `exit_codes` — map of exit code to short description.
- `error_codes` — array of stable `UPPER_SNAKE_CASE` codes the CLI
  may emit in the structured error envelope.

Per-command entry fields:

- `path` — array of tokens from the root to this command.
- `short` — one-line description.
- `flags` — flag definitions for this command.
- `human_output` — boolean; always `false` under the agent pick.
- `idempotent` — boolean; `true` when the command performs no state
  mutation regardless of inputs.

Adding a field is additive (minor). Renaming or removing one is a
major-version bump.

#### Versioning

A top-level `version` subcommand emits the tool version as JSON. When
the invocation reaches a target system, the response also carries the
observed target version. Skills branch on these to gate
target-version-specific behaviour.
