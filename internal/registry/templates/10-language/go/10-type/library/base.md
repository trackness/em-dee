### Go library

Type-universal Go library discipline. Applies to every Go module
imported by other Go code — no `main` package, no CLI surface.

#### Module identity

The `module` line in `go.mod` matches the canonical import path
consumers use. The module path is the contract: changing it after
publish breaks every consumer and orphans the old path in the proxy.
Pick it once, before the first tag.

#### No `main`

A library has no executable; a `main` package at the module root or
under `cmd/` recategorises the project. Companion CLIs ship as
separate modules or repos, never as `cmd/` inside the library module.

#### Public API surface and `/internal/`

Exports of the module's root package are the contract; every
non-`internal/` sub-package the module ships is also part of it. The
`godoc` rendering is the spec consumers read. Anything not part of the
contract lives under `internal/` — the toolchain refuses imports of
`<module>/internal/...` from outside the module. Default to
`internal/`; promote out only when an external consumer needs it.

#### Godoc

Every exported identifier carries a doc comment starting with the
identifier name followed by a period — `// Foo returns ...`. Package
documentation lives on a `// Package <name> ...` comment in `doc.go`.

#### Examples

Add `func Example...` functions in `_test.go` for every non-trivial
exported identifier. Match stdout with an `// Output:` block. Examples
render in `godoc` and are part of the published documentation.

#### Semantic versioning

Tag releases `vMAJOR.MINOR.PATCH`. `v0.x.y` permits breaking changes
between minor versions; `v1.0.0` and beyond do not. A major bump above
`v1` relocates the module path — `module/v2`, `module/v3`, etc., per
Go's semantic import versioning. The `module` line carries the `/vN`
suffix; every import path follows.

#### `go.sum` and `replace`

Commit `go.sum` alongside `go.mod`; the module proxy will not serve a
release whose checksums do not match. Never tag a release whose
`go.mod` contains a `replace` directive — consumers cannot honour
module-local rewrites. Use `replace` only for local development;
remove every directive before tagging.

#### Dependency minimisation

A library's transitive deps become its consumers' transitive deps. The
bar for adding a third-party dep is higher than in a CLI of the same
scope — every consumer inherits the graph. Prefer stdlib; when
unavoidable, justify the dep in the PR description naming the stdlib
alternative and why it was insufficient.

#### No side effects on import

A library returns values and errors. It does not write to stdout or
stderr, call `os.Exit`, parse flags, read env vars on package init, or
mutate global state in `init`. Never `panic` except for unrecoverable
invariants — return an error otherwise. Libraries log through the slog
default; do not accept a logger parameter unless the design demands it.

#### Backward compatibility

Exported signatures are immutable within a major version. Adding a
parameter to an exported function is a breaking change; add a new
function, or adopt a struct-of-options pattern from day one:

```go
type FooOptions struct {
    Timeout time.Duration
    _       struct{} // forces keyed construction.
}

func Foo(ctx context.Context, opts FooOptions) error { ... }
```

The unexported `_ struct{}` field forces keyed construction, so adding
fields later never breaks positional literals in consumer code.

#### Test layout

`_test.go` files sit alongside production code. Default to `package
foo` (white-box) when tests need internal access; use `package foo_test`
(black-box) for tests that exercise only the public API. `t.Parallel()`
everywhere safe; `testing/synctest` for time-dependent code.

#### Release flow

Releases are tag-driven. `git tag vX.Y.Z && git push origin vX.Y.Z`;
the module proxy ingests the tag and serves the version. No GitHub
Release artefacts are required.

#### Excluded

- `pkg/` directory layout — everything not under `internal/` is
  implicitly public.
- `cmd/` inside the library module — companion CLIs ship as separate
  modules.
- `goreleaser` for library releases — libraries distribute through
  the module proxy via tags alone.
