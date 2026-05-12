# em-dee

em-dee is a Go CLI that generates `CLAUDE.md` files for new projects from a
curated, embedded catalog of opinionated markdown blocks. Pick a language and
program type (CLI, server, library, …), then a small number of associated
choices (framework, logging, …) plus cross-cutting concerns (infra, CI,
tooling); em-dee concatenates the matching blocks into a `CLAUDE.md` and,
by default, asks Claude to review the result.

## Install

Three install paths. Pick whichever fits your environment.

**Go install** (any platform with a Go toolchain):

```sh
go install github.com/trackness/em-dee/cmd/em-dee@latest
```

**Direct download** — no Go toolchain required. Copy-paste the snippet
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

`Expand-Archive` extracts into a sibling folder named after the
archive (here, `./em-dee/`) — unlike the unix `tar -xz` snippets,
which extract flat in CWD. Pass `-DestinationPath .` to flatten if
you want parity. The trailing `Remove-Item` cleans up the downloaded
`.zip`.

```powershell
Invoke-WebRequest -Uri https://github.com/trackness/em-dee/releases/latest/download/em-dee_windows_amd64.zip -OutFile em-dee.zip; Expand-Archive em-dee.zip -DestinationPath .; Remove-Item em-dee.zip
```

**Homebrew** — post-tap-setup, not live until the `trackness/tap`
formula is published:

```sh
brew install trackness/tap/em-dee
```

## Usage

`em-dee` with no subcommand is the interactive entrypoint; on a TTY it
prompts for language, program type (where the language has type-conditional
content), and per-category picks. In non-interactive contexts (CI, pipes,
`Makefile` recipes), pass `--language=<id> --use-defaults` — em-dee accepts
the registry defaults for every other category and errors out instead of
half-prompting. `--language=<id>` is the only per-category flag; everything
else is interactive or `--use-defaults`.

| Command                                                  | Description                                                                       |
|----------------------------------------------------------|-----------------------------------------------------------------------------------|
| `em-dee`                                                 | Interactive flow on a TTY.                                                        |
| `em-dee generate --language=python --use-defaults`       | Non-interactive generation: language pick supplied, every other category default. |
| `em-dee list`                                            | Show the full catalog.                                                            |
| `em-dee list --json`                                     | Same, machine-readable.                                                           |
| `em-dee show <ref>`                                      | Print one block's markdown (see ref forms below).                                 |
| `em-dee version`                                         | Print version, human form.                                                        |
| `em-dee version --json`                                  | Print version as machine-readable JSON.                                           |
| `em-dee update --check`                                  | Check for a newer release (see exit codes below).                                 |

### `show <ref>` resolver forms

Resolved paths are relative to `internal/registry/templates/`. Container
categories are elided from the dotted ref (per CONTENT-STYLE.md §2.3) —
only the chosen container option's id appears in the ref.

| Form                                       | Example                       | Resolves to                                                              |
|--------------------------------------------|-------------------------------|--------------------------------------------------------------------------|
| `language.<lang>`                          | `language.python`             | `10-language/python/base.md`                                             |
| `<lang>.<cat>.<opt>`                       | `python.logging.loguru`       | `10-language/python/20-logging/loguru.md`                                |
| `<lang>.<type>.<cat>.<opt>`                | `python.cli.framework.typer`  | `10-language/python/10-type/cli/10-framework/typer.md`                   |
| `<lang>.<type>`                            | `python.cli`                  | `10-language/python/10-type/cli/base.md`                                 |
| `<cat>.<opt>`                              | `infra.docker`                | `20-infra/docker.md`                                                     |

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

After rendering the output, em-dee shells out to `claude -p` to review
the rendered content (the in-memory byte slice — not the on-disk file
at `--out`). This is opt-out via `--no-review`. Use
`--review-out=<path>` to capture the structured JSON output:

| Field     | Type              | Notes                                                                                                                  |
|-----------|-------------------|------------------------------------------------------------------------------------------------------------------------|
| `verdict` | string            | One of `ok`, `warnings`, `problems`. The on-disk artifact may also carry `unstructured` as a tier-3 fallback sentinel. |
| `summary` | string            | One-sentence overall assessment.                                                                                       |
| `issues`  | array             | Per-issue `{severity, location, issue, suggestion}` objects. May be empty.                                             |
| `raw`     | string            | Emitted only on the tier-3 unstructured-fallback path; carries the unparsed text. Absent from the JSON entirely on tier 1 / tier 2 (not present-but-empty). |

`--review-timeout=<duration>` overrides the default 60s subprocess
deadline.

## Further reading

- Operating contract: [`CLAUDE.md`](CLAUDE.md)

(em-dee v1 has no user-facing env-var configuration — `EM_DEE_*` is
reserved but unread, and all knobs are flags documented under Usage.
The one env var em-dee does read is `GITHUB_TOKEN`: when set, it's
forwarded as `Authorization: Bearer` on the GitHub API calls in
`em-dee update --check` and the `em-dee update` install path, purely
to raise the unauthenticated rate limit.)

## Status

v0.1.0 will ship with placeholder / TODO content in the catalog. The
CLI surface, the catalog structure, the review pipeline, and the
release/update mechanism are finalised; only the per-block markdown
is TODO. Finalised content lands in subsequent releases.

## License

MIT — see [`LICENSE`](LICENSE).
