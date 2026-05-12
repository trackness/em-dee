### Human consumer

The primary consumer of this CLI is a human at a terminal. Rendered
human output is the default; JSON is the opt-in for piping into
another tool. Both contracts ship from day one.

#### Output contract

- Human-rendered output is the default on stdout. ANSI, colour, and
  Unicode box-drawing are permitted on a TTY (see TTY detection
  below). Prompts are permitted on a TTY; mutation gating still
  applies through `--yes` / `--dry-run` as pinned in cli/base.md.
- Errors in human mode render on stderr as plain text `Error: ...`.
  The structured JSON error envelope from cli/base.md fires only
  under `--output json`.
- Logs go to stderr regardless of verbosity. stdout carries the
  rendered output only.

#### Mode handling

`--output human` (short `-o human`) is the default. `--output json`
is the alternate for piping; scripts should pass it explicitly so
intent survives a future consumer-pick change.

- Any other `--output` value exits 1 with `INVALID_OUTPUT_MODE`.
- Under `--output json`: stdout carries a single JSON document and
  nothing else (no ANSI, no progress, no banners). Errors render as
  the structured envelope from cli/base.md on stderr. Prompts are
  suppressed regardless of TTY. Output is sorted, timestamp-free
  unless semantic, and preserves Go struct field declaration order.

#### Table rendering

Default: `github.com/jedib0t/go-pretty/v6/table` at style `Light`
unless a command justifies another. Plain aligned columns
(shell-completion output, key=value listings) may use stdlib
`text/tabwriter` when a bordered table is over-engineering. One
renderer per command — do not mix ad-hoc formatters with go-pretty
in the same output.

**Excluded:** `github.com/olekukonko/tablewriter` — v1 rollout
unstable, legacy v0 API divergent.

#### TTY detection

Default: `golang.org/x/term`. Gate colour and box-drawing on
`term.IsTerminal(int(os.Stdout.Fd()))`. Suppress both when stdout is
piped to a file or another process; the human-default contract still
holds for the rendered layout, just without ANSI.

#### Colour

Default: a hand-rolled `internal/output/colour.go` helper (~20
lines) over `golang.org/x/term`. Table-cell colour goes through
go-pretty's `text` subpackage (already in via go-pretty). Adopt
`github.com/fatih/color` at implementation time only when three or
more distinct colour uses arise outside go-pretty tables; the
adoption replaces the hand-rolled helper, and its transitive deps
(`github.com/mattn/go-isatty`, `github.com/mattn/go-colorable`) are
accepted in that case.

**Excluded:** `github.com/charmbracelet/lipgloss`,
`github.com/muesli/termenv`. Direct import of
`github.com/mattn/go-isatty` or `github.com/mattn/go-colorable` is
forbidden; transitive arrival via `fatih/color` is fine.

#### Determinism

Human-mode output is tested against golden text fixtures in
`testdata/`. Sort every collection by a stable key documented in the
command's `--help`, omit gratuitous timestamps, and preserve
declaration order in any JSON the `--output json` fallback emits.

#### What lives in cli/base.md

The type-universal rules apply unchanged to the human consumer; do
not restate them. See cli/base.md for: Configuration, Exit codes,
Structured errors (fire only under `--output json` here), Mutation
gating, Signal handling, Secret redaction, Help and usage routing,
Paging.
