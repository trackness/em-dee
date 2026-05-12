## Go

Language-universal Go discipline. Applies to every project in this
repository regardless of program type.

### Go version

Pin the toolchain in `go.mod`. This block assumes Go 1.26+. Do not
write code that requires a newer toolchain without bumping the pin.

### Layout

```
cmd/<binary>/        thin main — parse, construct, delegate
internal/<domain>/   implementation packages, one concern each
testdata/            golden files and fixtures per package
```

No `pkg/`. Public surface lives in the module root; everything else
is `internal/`. One concern per `internal/<domain>/` package.

### Language and stdlib

- Use `new(T)` over `x := v; &x` for optional pointer fields.
- Use stdlib builtins `min`, `max`, `clear`. Do not import helpers.
- Zero values are useful. Declare empty slices as `var s []T`.
- Exception: when a JSON field must serialise as `[]` not `null`,
  return `[]T{}` and comment naming the contract.

### Errors

Default: stdlib `errors` plus `fmt.Errorf("<operation>: %w", err)`.

- Wrap with `%w` at every layer that adds context; the leading
  fragment names the operation.
- Sentinel errors at package scope: `var ErrFoo = errors.New("foo")`.
  Export only when callers must detect them.
- Detect with `errors.Is` and `errors.As`. No type switches on errors.
- Do not panic except in `main` for unrecoverable startup failures.

Excluded: `github.com/pkg/errors` — `Cause()` is incompatible with
`errors.Is`/`errors.As`.

### Context

Default: stdlib `context.Context` on every call that does I/O, is
cancellable, or crosses a goroutine boundary.

- Name the parameter `ctx` and place it first. Never store a
  `context.Context` in a struct field.
- Always call the cancel returned by `WithCancel`, `WithTimeout`, or
  `WithDeadline`. Do not leave `context.TODO` in landed code.

### Concurrency

Default: stdlib `context`, `sync`, `sync/atomic`, `testing/synctest`.
`testing/synctest` (stable since 1.25) handles both deterministic
time and synthetic-bubble goroutine accounting, so a separate
`go.uber.org/goleak` import is only needed for code paths that
spawn goroutines outside a synctest bubble — long-lived server
loops, daemons, anywhere `synctest.Run` doesn't enclose the work.

- Every goroutine has an explicit lifecycle: a `context.Context` for
  cancellation plus a wait mechanism for shutdown. No fire-and-forget.
- Use `testing/synctest` for time-dependent concurrent tests. Never
  `time.Sleep` in tests.

Excluded: `jonboulle/clockwork`, `benbjohnson/clock` — fake-clock
libraries require production coupling that `testing/synctest`
eliminates.

### Atomics

Default: stdlib `sync/atomic` typed values (`atomic.Int64`,
`atomic.Pointer[T]`). Excluded: `go.uber.org/atomic` — stdlib typed
atomics are a strict superset.

### JSON

Default: stdlib `encoding/json`. `json/v2` is experimental in 1.26;
do not enable it. Tag every serialised field explicitly. Excluded:
`json-iterator/go`.

### YAML

Default: `github.com/goccy/go-yaml`; honours `json` struct tags when
`yaml` tags are absent. Excluded: direct import of `gopkg.in/yaml.v3`
— archived April 2025.

### Logging

See the picked logging block for the chosen logger. Do not duplicate
logger-specific rules here.

### Lint and format

Default: `gofmt -s` and `goimports` on save; `golangci-lint` with
config at `.golangci.yml`. Lint findings are errors in CI. Disable a
linter only inline at the offending site with a comment naming the
reason.

### Testing

Default: stdlib `testing` plus `github.com/google/go-cmp/cmp`.

- Table-driven tests with subtests via `t.Run(tt.name, ...)`. Named
  fields when entries carry four or more fields.
- Use `t.TempDir()` for filesystem work; never `os.TempDir()`.
- Integration tests sit behind `//go:build integration` under a
  separate task target.
- `go test -race ./...` must pass before a change is complete.

Excluded: `stretchr/testify` (any subpackage), `onsi/ginkgo`,
`onsi/gomega`.

### Mocking and test doubles

Default: hand-rolled fakes in test files. Define small focused
interfaces at command and client boundaries; implement fakes in the
tests that consume them.

Opt-in: `go.uber.org/mock` per-repo for interfaces that meet both
(1) ten or more methods and (2) hand-rolled fakes would appear in
three or more test files. When opted in, pin `mockgen`, place
generated mocks under `internal/<target>/mocks/`, drive regeneration
with `//go:generate`.

Excluded: `github.com/golang/mock` (archived), `vektra/mockery`,
`matryer/moq`.

### HTTP response mocking

Default: stdlib `net/http/httptest` with a real local server.
Handlers inspect the incoming `*http.Request` directly and fail with
a specific diff when expectations do not match. Canned responses live
in `testdata/`.

Excluded: `jarcoal/httpmock`, `h2non/gock`, `dankinder/httpmock`,
`go.nhat.io/httpmock` — each intercepts at the `Transport` level,
conflicting with explicit-`Transport` control.

### Dependencies

Prefer stdlib. A new third-party dependency requires a one-line
justification in the PR description naming the stdlib alternative and
why it was insufficient. Commit `go.sum` alongside `go.mod`. No
`replace` directives in tagged releases.

### Secret handling

- Redact at the rendering site, not the call site, before any value
  reaches stdout, stderr, or a log sink.
- Source precedence: environment variables > file > flag.
- Never log secret values, including inside error messages.
- File permissions `0600` on any secret-bearing config file.
- No secrets in user-facing error messages.

### `task verify`

`task verify` is the pre-commit gate. The Taskfile target runs, in
order:

```
gofmt -l .                # fails on any unformatted file
go vet ./...
go test ./...
```

CI runs the same target plus a parallel `golangci-lint run` job
keyed off `.golangci.yml`. Run `-race` locally when the change
touches concurrency. Fix every failure before presenting a change
for review; do not regenerate goldens to mask a real failure.
