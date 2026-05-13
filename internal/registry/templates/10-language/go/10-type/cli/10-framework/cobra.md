### Cobra

Default: `github.com/spf13/cobra` with `github.com/spf13/pflag`.

#### Layout per command

One file per command under `internal/cli/`, each exporting a
constructor returning `*cobra.Command`. Groups and their subcommands
are separate files: `config.go` holds the `config` group constructor,
`config_view.go` holds `config view`. Future groups follow the same
split — no exceptions.

- Composition is explicit in `internal/cli/root.go`. Every command
  attaches via `AddCommand` from there; no `init()` registration and
  no package-level command variables.
- The constructor builds the command, wires its flags, sets `RunE`,
  and returns.

#### `RunE` everywhere

Use `RunE`, never `Run`. Errors propagate up to root, where they map
to exit codes via the stable code table in `internal/output/errors.go`
(see `Go CLI` → Exit codes and Structured errors).

- Validate argument count with `cobra.ExactArgs(N)`,
  `cobra.MaximumNArgs(N)`, or `cobra.RangeArgs(min, max)`. Do not
  hand-roll length checks in `RunE`.
- Return errors wrapped with `%w`; the root unwraps to find the
  mapped code.

#### Persistent root flags

The root command carries the conventional persistent flag set, wired
once in `root.go`:

- `--output`, `-o` — output mode (see the consumer block for the
  contract).
- `--log-level` — slog level threshold.
- `--timeout` — overall command deadline; drives the root context.
- `--config` — explicit config-file path (see `Go CLI` →
  Configuration).
- `--yes` — confirms mutating commands (see Command annotations).
- `--dry-run` — pairs with `--yes` for the mutation gate.

Per-repo auth and endpoint flags attach to root with `PersistentFlags`
in the same `root.go` block. Leaf-only flags live on the leaf
command, not root.

#### `PersistentPreRunE` chain

Root's `PersistentPreRunE` resolves configuration, applies the
log-level, and runs the mutation-gate check. Child commands inherit
the chain; do not redo configuration resolution in leaf `RunE`
bodies. A leaf that needs a resolved value reads it from the shared
config struct populated by root.

#### Help and usage

See `Go CLI` → Help and usage routing for the rules: `cmd.SetOut(os.Stderr)`
on root, and a bare command group exits 1 with `SUBCOMMAND_REQUIRED`.

#### Command annotations

The mutation gate (see `Go CLI` → Mutation gating) keys on
`cmd.Annotations["<cli-name>/mutating"]`. Set the annotation on the
**leaf** command in its constructor; never on a group. Groups
dispatch, leaves mutate — the gate trusts that distinction. The
annotation value is presence-only; any non-empty string passes.

#### Excluded

`github.com/urfave/cli` (any version), `github.com/alecthomas/kong`.
