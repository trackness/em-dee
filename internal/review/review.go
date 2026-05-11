// Package review shells out to `claude -p`, parses the JSON response,
// and presents the result via lipgloss.
//
// Phase 5 lands this in four files:
//
//   - review.go (this file): package doc + embedded prompt template.
//   - runner.go: the Runner interface + default ExecRunner.
//   - parse.go (Task 5.3): three-tier JSON parse.
//   - present.go (Task 5.4): lipgloss-rendered presentation.
//
// Higher-level wiring into `em-dee generate` lives in the cli package
// (Task 5.5).
package review

import (
	_ "embed"
)

// promptTemplate is the embedded review prompt. It is concatenated with
// the rendered CLAUDE.md content (via BuildPrompt) before being passed
// to `claude -p` as a single argv arg.
//
//go:embed prompt.md
var promptTemplate string

// PromptTemplate returns the embedded review prompt template. Exposed
// so tests and tooling can inspect the contract the runtime ships with
// without paying the cost of re-reading the file from disk.
func PromptTemplate() string { return promptTemplate }
