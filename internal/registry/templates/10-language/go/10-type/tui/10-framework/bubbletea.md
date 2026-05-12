### Bubble Tea

Use `charm.land/bubbletea/v2` when a screen needs a custom
Model / Update / View loop beyond huh's form abstractions.
Elm-architecture is the Charm ecosystem's native idiom; huh,
lipgloss, and bubbletea compose against it without adapter glue.

#### Model / Update / View

- Every screen is a `tea.Model` with three methods: `Init() tea.Cmd`,
  `Update(tea.Msg) (tea.Model, tea.Cmd)`, `View() string`.
- `Update` is the only place state changes. Return a new model value
  plus an optional command; do not mutate fields off the Update path.
- `View` is pure rendering. It returns a string and performs no I/O,
  no mutation, no logging. The returned string is the complete
  visible state for the frame.

#### Messages and commands

- Drive every state transition by dispatching a `tea.Msg` to
  `Update`. Do not spawn goroutines that mutate model fields.
- Run long-running I/O inside a `tea.Cmd`. The command function
  executes off the main loop and returns a result `tea.Msg`;
  `Update` consumes that message on the next tick.
- Custom message types are plain structs. Name them by what happened
  (`fetchCompletedMsg`, `tickMsg`), not by what should happen next.

#### Program options

- Construct with `tea.NewProgram(initialModel, opts...)`.
- Pass `tea.WithAltScreen()` for full-screen TUIs so the user's
  scrollback is preserved on exit.
- Pass `tea.WithMouseCellMotion()` when the screen needs mouse input.
- Handle resize by responding to `tea.WindowSizeMsg` in `Update` and
  threading the dimensions into any lipgloss layout that needs them.

#### Quit and cancellation

- Return `m, tea.Quit` from `Update` to terminate the program.
- Treat `Ctrl-C`, `Esc`, and `q` as user-cancel inputs. On any of
  them, return `m, tea.Quit` and have the outer caller exit with
  status `130`, matching the TUI cancellation contract.
- Never exit `0` on a user-aborted bubbletea program.

#### Errors

- `Update` has no error-return path. Carry an `err error` field on
  the Model.
- On a terminal failure, set `m.err` and return `m, tea.Quit`.
- After `p.Run()` returns, type-assert the final model, inspect its
  `err` field, and exit accordingly. Wrap with `%w` when surfacing
  to a CLI caller.

#### Sub-models

- Decompose complex screens into nested `tea.Model` values, one per
  logical screen or panel.
- The parent's `Update` forwards messages to the active child via
  `sub, cmd := child.Update(msg)` and stores the returned sub-model.
- Forward only what the child needs to see; intercept global keys
  (quit, navigation) at the parent before delegating.

#### Testing

- Drive `Update` directly with synthetic `tea.Msg` values. Assert on
  the returned model state and the returned command.
- Never call `p.Run()` from a test. The program loop owns the
  terminal and is not headless-safe.
- For time-dependent transitions, use `testing/synctest` to advance
  the bubble clock deterministically rather than wall-clock sleeps.

#### Excluded

- `github.com/rivo/tview` — component-tree architecture, not TEA;
  mixes badly with huh and lipgloss in the same binary.
- `github.com/jroimartin/gocui` — unmaintained.
- `github.com/nsf/termbox-go` — superseded by the bubbletea / tcell
  stack.
- `github.com/charmbracelet/bubbletea` — wrong namespace; see
  `tui/base.md` for the `charm.land/v2` rationale.
