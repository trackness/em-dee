### Go CLI

Type-universal Go CLI discipline. Applies to every Go CLI in this
repository regardless of framework or consumer pick.

#### Layout

```
cmd/<cli-name>/      thin main — parse, construct, delegate
internal/cli/        command definitions, flag wiring, output formatting
internal/<domain>/   implementation packages, one concern each
internal/output/     json/human renderers, shared envelope types, exit codes
testdata/            golden files per package (human + json variants)
```

`internal/output/` owns the output contract; commands call into it
rather than rendering ad hoc. Goldens cover both human and JSON
variants per command.

#### Configuration

Four-source precedence, strict: flag > env > file > default. Higher
sources override lower silently.

- A missing config file is not an error. A malformed config file is
  — exit 1 with a structured error naming the file path and the
  parser message.
- Missing required values exit 1 with a structured error naming each
  missing key and every source that could have supplied it.
- Auto-discovery, first match wins:
  1. `--config <path>` (explicit).
  2. `$<CLI>_CONFIG` env var.
  3. `$XDG_CONFIG_HOME/<cli-name>/config.yaml`.
  4. `$HOME/.config/<cli-name>/config.yaml` (fallback when XDG unset).

  On Windows replace 3 and 4 with `%APPDATA%\<cli-name>\config.yaml`.
- `--config=""` (or env `$<CLI>_CONFIG=""`) disables file loading
  entirely. Skills that need hermetic invocation use this.
- A `config view` subcommand emits the resolved configuration with
  per-value source attribution (`flag`, `env`, `file:<path>`,
  `default`) as JSON.
- Tests never read host config. Use `t.Setenv` to point
  `$XDG_CONFIG_HOME` at `t.TempDir()`, or pass `--config=""`
  explicitly.

Default: `github.com/knadh/koanf/v2` with providers `file`, `env/v2`,
`posflag`, and parser `yaml`.

Excluded: `spf13/viper` — divergent precedence semantics and
surprising environment-variable behaviour.

#### Exit codes

```
0   success
1   user / input error (bad flag, missing arg, missing required input,
    unsupported mode for this command)
2   target system returned an error
3   transport error (network, DNS, TLS, auth)
130 SIGINT
```

Skills branch on these values; do not repurpose them. Adding a new
exit code is a major-version bump.

#### Structured errors

In JSON output mode, every error reaches the user as a single JSON
object on stderr:

```
{"error": {"code": "<UPPER_SNAKE_CASE>", "message": "...", "details": {}}}
```

- Error codes are stable across minor versions; adding a new code is
  additive.
- Every error that reaches the user maps to a stable code in
  `internal/output/errors.go`. New domain errors require a new code
  there; do not synthesise codes inline at the call site.

#### Mutation gating

Every command that mutates target state carries a
`<cli-name>/mutating` annotation on the **leaf** command. Groups do
not inherit — the group dispatches, the leaf mutates.

- The root `PersistentPreRunE` rejects an annotated invocation that
  supplies neither `--yes` nor `--dry-run` with code
  `CONFIRMATION_REQUIRED` and exit 1.
- The gate runs *after* configuration resolution so env and file can
  contribute `--yes` / `--dry-run` values.
- A TTY-less invocation with missing input exits the same way —
  never block waiting on stdin.

Downstream projects with no mutating commands delete this prose from
their generated `CLAUDE.md` rather than opt in.

#### Signal handling

Wire `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` once
in `main` and pass the derived context everywhere.

- SIGINT propagates as `context.Canceled`; the process exits 130.
- SIGTERM maps to the same path. A request-scoped CLI has no
  graceful-shutdown phase to honour.

#### Secret redaction layer

`internal/output/` carries the redaction contract:

- `output.Redacted` — the constant `"<redacted>"`, a versioned
  contract value. Skills may branch on the literal.
- `output.SensitiveKeys` — literal config-key names that trigger
  redaction.
- `output.SensitiveSuffixes` — case-insensitive suffix matches:
  `-token`, `-secret`, `-password`, `-key`.
- `output.SensitiveHeaders` — HTTP header names that trigger
  header-value redaction.

`config view` replaces matching values with `output.Redacted` before
serialising. Source attribution (`flag`, `env`, `file:<path>`,
`default`) is preserved — skills diagnosing auth issues need to know
*where* the secret came from, not *what* it was.

`config view` emits redacted strings even for non-string config
values whose key matches the heuristic (e.g. an int field whose key
ends in `-key`). Declare such fields as `any` in skill-side schemas,
or reserve the sensitive set for string-valued keys.

Append target-specific names to these slices at process startup,
before the first call to `cli.Run`. Concurrent append plus read is a
data race.

#### Paging discipline

List commands default to a stated limit named in `--help`. `--limit
N` adjusts the limit. `--all` is permitted, with a stderr warning
about output size. Never dump unbounded output to stdout by default.

#### Help and usage routing

Route help and usage output to stderr by calling
`cmd.SetOut(os.Stderr)` on the root command. `--help` and `help`
still dispatch through the framework's rendering, but land on stderr;
stdout stays reserved for command output.

A bare command group (root or any non-leaf) exits 1 with
`SUBCOMMAND_REQUIRED` rather than dumping help to stdout. This rule
binds regardless of which CLI framework is picked.
