# CLAUDE.md — em-dee

Operating contract for Claude Code sessions working in this repository.
This file is read by the harness on every session and is the source of
truth for how to behave here.

## Operating principles

These are load-bearing. Every change in this repo is made under them.

1. **Don't assume. Don't hide confusion. Surface tradeoffs.**
2. **Minimum code that solves the problem. Nothing speculative.**
3. **Touch only what you must. Clean up only your own mess.**
4. **Define success criteria. Loop until verified.**

## What this repo is

em-dee is a Go CLI that generates `CLAUDE.md` files for new projects
from a curated, embedded catalog of opinionated markdown blocks. The
authoritative design lives at
`docs/superpowers/specs/2026-05-11-em-dee-design.md`. Read that before
making non-trivial changes.

## Mechanical recipes

> This section will be filled in during implementation per §10.1 of the
> design spec. Until the templates filesystem and the manifest hygiene
> validator exist, recipes here would be premature.

The shape these recipes will take, summarised from §10.1:

- **Add an option** to an existing category: drop
  `templates/<NN-cat>/<id>.md`, append one entry to that folder's
  `_index.yaml`, run `task verify`.
- **Add a new top-level category**: `mkdir templates/<NN-name>/`,
  create `_index.yaml`, add `.md` files, run `task verify`.
- **Add a new language**: `mkdir templates/10-language/<id>/`, create
  `base.md`, register in `templates/10-language/_index.yaml`, add
  nested sub-categories, run `task verify`.
- **Reorder**: change the folder's `NN-` prefix; never edit `options`
  list order to reorder.

## Anti-patterns

> Also filled in during implementation. Summary from §10.1:

- No frontmatter in `.md` files (metadata lives in `_index.yaml`).
- No cross-category constraint rules in code (soft-trust the picker).
- No reordering by editing `options` lists.
- No language-specific content in cross-cutting blocks.
- Never run `task golden-update` to fix a failing test in CI.

## Required commands

- `task verify` before every commit.
- `task build` for a local binary.

## Project state

Currently: design phase complete, implementation pending. The spec is
the contract. The implementation plan is at
`docs/superpowers/plans/<date>-em-dee-implementation.md` once written.
