# em-dee

em-dee is a Go CLI that generates `CLAUDE.md` files for new projects from a
curated, embedded catalog of opinionated markdown blocks. Pick a language and
a small number of associated choices (framework, logging, testing) plus
cross-cutting concerns (infra, CI, tooling); em-dee concatenates the matching
blocks into a `CLAUDE.md` and, by default, asks Claude to review the result.

## Install

Three install paths. Pick whichever fits your environment.

**Go install** (any platform with a Go toolchain):

```sh
go install github.com/trackness/em-dee/cmd/em-dee@latest
```

**Direct download** (recommended for non-Go users). Copy-paste the snippet
for your platform:

*macOS (Apple Silicon)*

```sh
curl -fsSL https://github.com/trackness/em-dee/releases/latest/download/em-dee_darwin_arm64.tar.gz | tar -xz
```

*macOS (Intel)*

```sh
curl -fsSL https://github.com/trackness/em-dee/releases/latest/download/em-dee_darwin_amd64.tar.gz | tar -xz
```

*Linux (x86_64)*

```sh
curl -fsSL https://github.com/trackness/em-dee/releases/latest/download/em-dee_linux_amd64.tar.gz | tar -xz
```

*Linux (ARM64)*

```sh
curl -fsSL https://github.com/trackness/em-dee/releases/latest/download/em-dee_linux_arm64.tar.gz | tar -xz
```

*Windows (x86_64, PowerShell)*

```powershell
Invoke-WebRequest -Uri https://github.com/trackness/em-dee/releases/latest/download/em-dee_windows_amd64.zip -OutFile em-dee.zip; Expand-Archive em-dee.zip
```

**Homebrew** (post-tap-setup; see [spec section 12.4](docs/superpowers/specs/2026-05-11-em-dee-design.md)
— this command is not live until the `trackness/tap` formula is
published):

```sh
brew install trackness/tap/em-dee
```

## Usage

`em-dee` with no subcommand is the interactive entrypoint; on a TTY it
prompts for language and each category. In non-interactive contexts
(CI, pipes, `Makefile` recipes), pass `--language=<id>` and any other
category flags — em-dee errors out instead of half-prompting.

| Command                                                     | Description                                                    |
|-------------------------------------------------------------|----------------------------------------------------------------|
| `em-dee`                                                    | Interactive flow on a TTY.                                     |
| `em-dee generate --language=python --python-logging=loguru` | Non-interactive generation. Flag names use dashes, not dots.   |
| `em-dee generate --use-defaults`                            | Accept all defaults; only `--language` must still be supplied. |
| `em-dee list`                                               | Show the full catalog.                                         |
| `em-dee list --json`                                        | Same, machine-readable.                                        |
| `em-dee show <ref>`                                         | Print one block's markdown (see ref forms below).              |
| `em-dee version`                                            | Print version, human form.                                     |
| `em-dee version --json`                                     | Print version as machine-readable JSON.                        |
| `em-dee update --check`                                     | Check for a newer release (see exit codes below).              |

### `show <ref>` resolver forms

| Form                 | Example                 | Resolves to                   |
|----------------------|-------------------------|-------------------------------|
| `language.<lang>`    | `language.python`       | `python/base.md`              |
| `<lang>.<cat>.<opt>` | `python.logging.loguru` | `python/20-logging/loguru.md` |
| `<cat>.<opt>`        | `infra.docker`          | `20-infra/docker.md`          |

### `update --check` exit codes

| Exit | Meaning                                                                       |
|------|-------------------------------------------------------------------------------|
| `0`  | Up-to-date, or running a `dev` / `dev-<sha>` build (staleness check skipped). |
| `1`  | Update available.                                                             |
| `2`  | Error (network, parse, rate limit, etc.).                                     |

### `generate` behaviour flags

`em-dee generate` (and the bare `em-dee` interactive entrypoint) accepts
a small set of flags that control where and how the file is written:

| Flag             | Default     | Description                                                                                                                                                            |
|------------------|-------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `--out=<path>`   | `CLAUDE.md` | Path to write the generated file.                                                                                                                                      |
| `--force`        | `false`     | Overwrite an existing file at `--out`. Previous contents are backed up to `<out>.bak.<unix-ts>` in the same directory. Without `--force`, em-dee refuses to overwrite. |
| `--dry-run`      | `false`     | Write rendered output to stdout instead of disk. Skips the existing-file check and the Claude review.                                                                  |
| `--use-defaults` | `false`     | Accept the default option for every category. `--language` must still be supplied (or chosen interactively on a TTY).                                                  |

## Claude review

After writing `CLAUDE.md`, em-dee shells out to `claude -p` to review the
generated file. This is opt-out via `--no-review`. Use
`--review-out=<path>` to capture the structured JSON output for
programmatic consumption — the shape is documented in spec section 7.2:

| Field     | Type              | Notes                                                                                                                                     |
|-----------|-------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `verdict` | string            | One of `ok`, `warnings`, `problems`. The on-disk artifact may also carry `unstructured` as a tier-3 fallback sentinel (spec section 7.7). |
| `summary` | string            | One-sentence overall assessment.                                                                                                          |
| `issues`  | array             | Per-issue `{severity, location, issue, suggestion}` objects. May be empty.                                                                |
| `raw`     | string (optional) | Present only on the tier-3 unstructured-fallback path. Carries the unparsed text.                                                         |

`--review-timeout=<duration>` overrides the default 60s subprocess
deadline.

## Further reading

- Design spec: [`docs/superpowers/specs/2026-05-11-em-dee-design.md`](docs/superpowers/specs/2026-05-11-em-dee-design.md)
- Operating contract: [`CLAUDE.md`](CLAUDE.md)

(em-dee v1 has no env-var configuration — `EM_DEE_*` is reserved but
unread, per spec section 14. All knobs are flags, documented under
Usage.)

## Status

v0.1.0 ships with placeholder / TODO content in the catalog — the catalog
structure is final, but the per-option markdown blocks are stubs pending
real content drafting. The CLI surface, the catalog structure, the review
pipeline, and the release/update mechanism are all finalised in v0.1.0;
only the per-block markdown is TODO. This is intentional per spec section
13 to ship the working binary against a real release; finalised content
lands in subsequent releases.

## License

MIT — see [`LICENSE`](LICENSE).
