### Go TUI

Type-universal Go TUI discipline. Applies to every Go TUI in this
repository regardless of which framework drives the screen.

#### Library stack

- Use the Charm stack: `charm.land/huh/v2` for forms,
  `charm.land/lipgloss/v2` for styling, `charm.land/bubbletea/v2`
  when a full custom Model / Update / View loop is required.
- Pin against the `charm.land/v2` namespace. Do not introduce
  `github.com/charmbracelet/lipgloss` or `github.com/charmbracelet/bubbletea`
  in new work: huh v2 transitively requires `charm.land/lipgloss/v2`,
  and carrying both lipgloss copies in the module graph is wasteful
  and produces type-incompatible style values across the two trees.
- One stack per binary. Do not mix `tview`, `tcell`, or `gocui` into
  a project that already uses bubbletea / huh.

#### Form construction is split from `.Run()`

- Every form has a `BuildXForm` constructor and a `RunXForm` runner.
- `BuildXForm` returns the `*huh.Form` plus the bound storage
  (typically a struct that owns `chosen *string`, `singles map[string]*string`,
  `multis map[string]*[]string`, etc.). It performs no I/O.
- `RunXForm` calls `BuildXForm`, invokes `.Run()`, maps errors, and
  returns the form's outputs.
- The split exists so unit tests can construct, inspect, and assert
  on bindings without a TTY. Do not collapse it back into a single
  function "for brevity"; the seam is load-bearing.

#### Cancellation contract

- Declare a package-local `ErrCancelled = errors.New("cancelled by user")`
  sentinel.
- In every runner, wrap `huh.ErrUserAborted` into `ErrCancelled`:

  ```go
  if err := form.Run(); err != nil {
      if errors.Is(err, huh.ErrUserAborted) {
          return ErrCancelled
      }
      return fmt.Errorf("run X form: %w", err)
  }
  ```

- Callers detect cancellation via `errors.Is(err, ErrCancelled)`
  without importing huh.
- A cancelled form exits the process with status `130` (SIGINT
  parity). Never exit `0` on a user-aborted form; never bury the
  abort and continue.

#### Validation

- Every form field carries a `.Validate(func(T) error)` for required-
  input and shape checks.
- Belt-and-braces applies: write the validator even when the field
  type cannot commit empty today. The validator is the contract; huh's
  current behaviour is the implementation.

#### Styles single-home

- Every package that renders to a terminal owns exactly one
  `styles.go` file declaring all `lipgloss.Style` values used by the
  package.
- No inline `lipgloss.NewStyle()...Render(...)` chains in update / view
  code. Update / view code reads a named style and calls `.Render`.
- Use ANSI 256-color codes — `lipgloss.Color("10")` for green, `"11"`
  for yellow, `"9"` for red — rather than hex literals. The 256-color
  path degrades gracefully on terminals without truecolor and across
  the wide set of `TERM` values agents and CI runners use.

#### Headless testing

- Tests construct the form via `BuildXForm`, assert on the bound
  storage and the form's structure (option labels, default selection,
  validator behaviour), and never call `.Run()`.
- Drive validators directly: extract the validator from the field
  under test (or re-expose it through the struct) and call it with
  the inputs you want to assert on.
- Use `t.Setenv` and `t.TempDir` rather than mutating process state.
  No test may depend on a TTY being attached.

#### TTY and width detection

- Use `golang.org/x/term` for terminal detection.
- Gate any interactive form on `term.IsTerminal(int(os.Stdin.Fd()))`.
  When stdin is not a TTY, fail fast with a clear error or fall back
  to a non-interactive path; never attempt to run a huh form against
  a piped stdin.
- Provide an explicit escape hatch — a `--no-tui` flag or equivalent
  — so the binary remains usable in CI, piped invocations, and agent
  contexts.
- Read terminal width via `term.GetSize` when layout decisions need
  it. Treat detection failure as "assume 80 columns" rather than
  panicking.

#### Exit codes

- Match the CLI exit-code taxonomy when the TUI ships alongside CLI
  subcommands. Pinned values regardless: `0` success, `130` user
  cancellation.
- A TUI-only program may define its own narrow taxonomy, but `0` and
  `130` keep their meaning.

#### Multi-phase forms

- When later choices depend on earlier ones, build them as separate
  forms with sequential `.Run()` calls, not one giant form with
  dynamic options.
- Each phase has its own `BuildXForm` / `RunXForm` pair and its own
  bound storage. The caller threads the output of phase N into the
  input of phase N+1.
- This keeps each phase independently testable and keeps the option
  list for any one `huh.Select` static at construction time, which is
  the shape huh's accessors handle most predictably.

#### Excluded

- `github.com/jroimartin/gocui` — unmaintained.
- `github.com/nsf/termbox-go` — superseded by the bubbletea / tcell
  stack.
- `github.com/chzyer/readline` — use huh's input components for
  prompts.
- `github.com/charmbracelet/lipgloss` and
  `github.com/charmbracelet/bubbletea` — see the namespace rationale
  under Library stack; pin against `charm.land/v2` instead.
