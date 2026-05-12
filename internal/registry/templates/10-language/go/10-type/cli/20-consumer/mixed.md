### Mixed consumer

This CLI serves human terminal users and skill agents as first-class
consumers. Two output modes ship from day one, both versioned and
covered by golden fixtures. Output is an API; changes to JSON fields,
error codes, exit-code meaning, or per-command human-mode support
bump the major version.

#### Output contract

JSON is the default; `--output human` (short `-o human`) opts into
terminal rendering. Both shapes are stable contracts. Scripts and
skills pass `--output json` explicitly so intent survives a future
consumer-pick change.

#### JSON mode rules

- stdout carries a single JSON document per invocation, nothing
  else. No ANSI, no progress, no banners. Logs go to stderr.
- Errors render as the structured envelope from `Go CLI` →
  Structured errors on stderr in JSON form.
- No prompts, ever — never block on stdin regardless of TTY.
  Mutation gating uses `--yes` / `--dry-run` from `Go CLI` →
  Mutation gating.

#### Human mode rules

- Tables: `github.com/jedib0t/go-pretty/v6/table` at
  `table.StyleLight`. Plain aligned columns (shell-completion,
  key=value listings) may use stdlib `text/tabwriter`. One renderer
  per command.
- TTY detection via `golang.org/x/term`; gate colour and box-drawing
  on `term.IsTerminal(int(os.Stdout.Fd()))` and suppress both when
  stdout is piped.
- Colour: a hand-rolled `internal/output/colour.go` helper (~20
  lines) over `golang.org/x/term`; table-cell colour through
  go-pretty's `text` subpackage. Adopt `github.com/fatih/color` only
  when three or more distinct colour uses arise outside go-pretty.
- Errors render on stderr as plain text `Error: ...`; the structured
  envelope fires only under `--output json`. Prompts permitted on a
  TTY; mutation gating still applies from `Go CLI` → Mutation gating.

#### Mode selection

`--output json` (default) and `--output human` are the only accepted
values. Any other value exits 1 with `INVALID_OUTPUT_MODE`.
`--output human` on a machine-only command (see Introspection) exits
1 with `HUMAN_OUTPUT_NOT_SUPPORTED`.

#### Introspection — the `commands` subcommand

A top-level `commands` subcommand emits the full command tree as
JSON. The `CommandsOutput` shape is a versioned contract. Top-level
fields: `name`, `commands`, `exit_codes`, `error_codes` (stable
`UPPER_SNAKE_CASE` codes).

Per-command fields: `path`, `short`, `flags`, `human_output` (`true`
when the command renders human output; `false` exits 1 with
`HUMAN_OUTPUT_NOT_SUPPORTED` on `-o human`), `idempotent` (`true`
when the command performs no state mutation). Adding a field is
additive (minor); renaming or removing one bumps major.

#### Determinism

Both modes are tested against golden fixtures in `testdata/` — text
variant per command for human mode, JSON variant for JSON mode.
Sort collections by a stable key named in `--help`. Omit gratuitous
timestamps. Preserve Go struct field declaration order in JSON
output — field order is part of the contract.

#### Excluded

- `github.com/olekukonko/tablewriter` — v1 rollout unstable, legacy
  v0 API divergent.
- `github.com/charmbracelet/lipgloss`, `github.com/muesli/termenv`.
- Direct import of `github.com/mattn/go-isatty` or
  `github.com/mattn/go-colorable`. Transitive arrival via
  `fatih/color` is accepted.
