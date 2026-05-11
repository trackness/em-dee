// Package review shells out to `claude -p`, parses the JSON response,
// and presents the result via lipgloss.
//
// Phase 5 lands this in four tasks:
//
//   - 5.1: embed the review prompt template (this file).
//   - 5.2: define the Runner interface + default subprocess
//     implementation, in this same file.
//   - 5.3: Parse() in parse.go.
//   - 5.4: Present() in present.go.
//
// Higher-level wiring into `em-dee generate` lives in the cli package
// (Task 5.5).
package review

import (
	_ "embed"
)

// promptTemplate is the embedded review prompt. It is concatenated with
// the rendered CLAUDE.md content before being passed to `claude -p` as
// a single argv arg per spec §7.1.
//
//go:embed prompt.md
var promptTemplate string

// PromptTemplate returns the embedded review prompt template. Exposed
// so tests and tooling can inspect the contract the runtime ships with
// without paying the cost of re-reading the file from disk.
func PromptTemplate() string { return promptTemplate }
