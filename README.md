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

**Direct download** (any platform; recommended for non-Go users). Substitute
`<os>` (`darwin`, `linux`, `windows`) and `<arch>` (`amd64`, `arm64`) for your
platform:

```sh
curl -fsSL https://github.com/trackness/em-dee/releases/latest/download/em-dee_<os>_<arch>.tar.gz | tar -xz
```

Or auto-detect via `uname`:

```sh
curl -fsSL "https://github.com/trackness/em-dee/releases/latest/download/em-dee_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar -xz
```

**Homebrew** (post-tap-setup; see spec §12.4 — this command is not live until
the `trackness/tap` formula is published):

```sh
brew install trackness/tap/em-dee
```

## Usage

| Command | Description |
| --- | --- |
| `em-dee` | Interactive flow — prompts for language and each category. |
| `em-dee generate --language=python --python-logging=loguru` | Non-interactive generation. Flag names use dashes, not dots (`--python-logging`, not `--python.logging`). |
| `em-dee generate --use-defaults` | Accept all defaults; only the language must still be supplied. |
| `em-dee list` | Show the full catalog (categories and options). |
| `em-dee show <ref>` | Print one block's markdown to stdout, e.g. `em-dee show python.logging.loguru` or `em-dee show infra.docker`. |
| `em-dee version` | Print the version. `--json` for machine-readable output. |
| `em-dee update --check` | Check for a newer release. Exit code `0` = up-to-date, `1` = update available, `2` = error. |

## Claude review

After writing `CLAUDE.md`, em-dee shells out to `claude -p` to review the
generated file. This is opt-out via `--no-review`. Use `--review-out=<path>`
to capture the structured JSON output (verdict, findings, suggested edits)
for programmatic consumption; `--review-timeout=<duration>` overrides the
default 60s subprocess deadline.

## Configuration

- Design spec: [`docs/superpowers/specs/2026-05-11-em-dee-design.md`](docs/superpowers/specs/2026-05-11-em-dee-design.md)
- Operating contract: [`CLAUDE.md`](CLAUDE.md)

## Status

v0.1.0 ships with placeholder / TODO content in the catalog — the catalog
structure is final, but the per-option markdown blocks are stubs pending
real content drafting. This is intentional per spec §13 to ship the working
binary against a real release; finalised content lands in subsequent
releases.

## License

MIT — see [`LICENSE`](LICENSE).
