### log/slog

Default: stdlib `log/slog`. Structured-by-default, zero third-party
dependency, the supported Go answer since 1.21.

### Handler selection

- Construct the handler once in `main`. Never instantiate handlers at
  call sites.
- Use `slog.NewJSONHandler(os.Stderr, opts)` for production, CI, and
  any agent-consumer project. Machine-parseable output is the contract.
- Use `slog.NewTextHandler(os.Stderr, opts)` only for human-facing
  local dev builds; gate it behind a `--log-format` flag or build tag.
- Wire the constructed logger as the process default:
  `slog.SetDefault(slog.New(handler))`. Libraries log via the package
  default; do not thread `*slog.Logger` through APIs unless a specific
  call site needs a distinct logger.

### Level configuration

- Drive level from a `*slog.LevelVar` constructed in `main` and passed
  into the handler via `&slog.HandlerOptions{Level: lvlVar}`.
- Default level is `slog.LevelInfo`. Read overrides from flag or env
  (`LOG_LEVEL=debug`); a runtime bump requires no restart.
- Call `slog.SetLogLoggerLevel(slog.LevelInfo)` in `main` so
  third-party packages writing to stdlib `log` route through the slog
  default at a pinned level.

### Call-site conventions

- Use the `Context` variants on every call inside a cancellable code
  path: `slog.InfoContext(ctx, ...)`, `slog.ErrorContext(ctx, ...)`,
  `slog.DebugContext(ctx, ...)`. Handlers extract trace and span ids
  from `ctx`; the non-`Context` variants drop that signal.
- Prefer typed attribute constructors over `slog.Any`: `slog.String`,
  `slog.Int`, `slog.Int64`, `slog.Bool`, `slog.Duration`, `slog.Time`.
  `slog.Any` reflects at the call site and shows up in CPU profiles
  under load.
- Group related fields with `slog.Group("http", slog.String("method",
  r.Method), slog.Int("status", code))` rather than flattening into a
  long attribute list.
- Log errors at `Error` with `slog.Any("err", err)` so wrapped chains
  serialise. Pair every error log with enough identifiers to locate
  the failing record (request id, user id, resource id).

### Multi-sink output

- Default is single-sink to `os.Stderr`. Reserve multi-sink for
  projects with a real dual-destination need (e.g. stderr for the
  operator, JSON file for an aggregator).
- When justified, compose with `slog.NewMultiHandler(fileHandler,
  stderrHandler)`. Both handlers share the same `LevelVar`.

### What never gets logged

- Request bodies, full HTTP responses, full headers (especially
  `Authorization`, `Cookie`, `Set-Cookie`).
- File contents, query result rows, secrets of any shape. Redaction
  rules in `go/base.md` bind every log call; structured output makes
  accidental leaks worse, not better.
- Log identifiers and outcomes; never the payload.

### Excluded

`go.uber.org/zap`, `github.com/rs/zerolog`, `github.com/sirupsen/logrus`
— stdlib `slog` covers their value with no third-party dependency.
