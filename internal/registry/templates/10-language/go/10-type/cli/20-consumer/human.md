### Human consumer

The primary consumer of this CLI is a human at a terminal. Rendered
human output is the default; JSON is the opt-in for piping into
another tool. Both contracts ship from day one.

#### Output contract

- Human-rendered output is the default on stdout. ANSI, colour, and
  Unicode box-drawing are permitted on a TTY (see TTY detection
  below). Prompts are permitted on a TTY; mutation gating still
  applies through `--yes` / `--dry-run` from `Go CLI` → Mutation
  gating.
- Errors in human mode render on stderr as plain text `Error: ...`.
  The structured JSON envelope from `Go CLI` → Structured errors
  fires only under `--output json`.
- Logs go to stderr regardless of verbosity. stdout carries the
  rendered output only.

#### Mode handling

`--output human` (short `-o human`) is the default. `--output json`
is the alternate for piping; scripts should pass it explicitly so
intent survives a future consumer-pick change.

- Any other `--output` value exits 1 with `INVALID_OUTPUT_MODE`.
- Under `--output json`: stdout carries a single JSON document and
  nothing else (no ANSI, no progress, no banners). Errors render as
  the structured envelope from `Go CLI` → Structured errors on
  stderr. Prompts are suppressed regardless of TTY. Output is sorted,
  timestamp-free unless semantic, and preserves Go struct field
  declaration order.

#### Rendering stack

Default: `github.com/jedib0t/go-pretty/v6/table` at `table.StyleLight`
for bordered tables; stdlib `text/tabwriter` for plain aligned
columns (shell-completion output, key=value listings) where a border
is over-engineering. TTY detection runs via `golang.org/x/term` —
gate colour and box-drawing on `term.IsTerminal(int(os.Stdout.Fd()))`
and suppress both when stdout is piped. General colour goes through a
hand-rolled `internal/output/colour.go` helper (~20 lines) over
`golang.org/x/term`; table-cell colour reuses go-pretty's `text`
subpackage (already in via go-pretty).

- One renderer per command — do not mix ad-hoc formatters with
  go-pretty in the same output.
- Piping to a file or another process keeps the rendered layout but
  drops the ANSI; the layout itself is part of the contract.

**Opt-in:** `github.com/fatih/color` for general colour when three or
more distinct colour uses arise outside go-pretty tables.

**When opted in:** the adoption replaces the hand-rolled helper. Its
transitive deps (`github.com/mattn/go-isatty`,
`github.com/mattn/go-colorable`) become accepted via that path.

**Excluded:** `github.com/olekukonko/tablewriter` — v1 rollout
unstable, legacy v0 API divergent.
`github.com/charmbracelet/lipgloss`, `github.com/muesli/termenv` —
the bubbletea/huh stack belongs to the TUI type, not CLI human-mode
rendering.
Direct import of `github.com/mattn/go-isatty` or
`github.com/mattn/go-colorable` — transitive arrival via
`fatih/color` is fine; direct imports drag the dependency
unnecessarily.

#### Determinism

Human-mode output is tested against golden text fixtures in
`testdata/`. Sort every collection by a stable key documented in the
command's `--help`, omit gratuitous timestamps, and preserve
declaration order in any JSON the `--output json` fallback emits.
