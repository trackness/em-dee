package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

// Runner is the interface the review pipeline uses to invoke
// `claude -p`. The seam exists for the same reason spec §11 calls for
// it: real subprocess invocation is awkward to test against, so parse
// and presentation tests inject a stub.
//
// `Run` returns:
//   - `stdout`: the model's raw response bytes. The default ExecRunner
//     strips the `--output-format=json` wire envelope (see decision
//     below); stub runners in tests return whatever the test wants
//     Parse to see.
//   - `stderr`: subprocess stderr verbatim. The spec §7.6 "claude
//     review failed:" path prints this when the runner returns an
//     exec error.
//   - `err`: ErrClaudeNotFound when LookPath fails, ErrTimeout when
//     the context deadline expires, or any os/exec error otherwise.
//
// Decision (plan Task 5.2 tradeoff): the wire-envelope unwrapping
// (`--output-format=json`'s `.result` / `.content` field) lives in the
// runner, NOT in parse.go. Reasoning: the envelope is part of the
// transport contract between em-dee and the `claude` CLI, not part of
// the response schema between em-dee and the model. Putting the
// unwrap in parse would mean parse needs to know about subprocess
// transport — wrong layer. If a future `claude` version changes the
// envelope shape, only the runner needs updating.
type Runner interface {
	Run(ctx context.Context, prompt string) (stdout []byte, stderr []byte, err error)
}

// ErrClaudeNotFound is returned by ExecRunner.Run when `claude` is not
// on PATH. The CLI layer maps this to spec §7.6's "note: claude CLI
// not found; skipping review" line.
var ErrClaudeNotFound = errors.New("claude CLI not found on PATH")

// ErrTimeout is returned by ExecRunner.Run when the context deadline
// expires before claude exits. The CLI layer maps this to spec §7.6's
// "note: claude review timed out after <duration>" line.
//
// Distinct from a generic exec error so the CLI can pick the right
// failure-mode wording — the spec gives the timeout case its own line.
var ErrTimeout = errors.New("claude review timed out")

// ExecRunner is the production Runner: shells out to `claude -p
// <prompt> --output-format=json`, unwraps the JSON envelope to extract
// the model's raw response, and returns that.
//
// Spec §7.1: the prompt is passed as a single -p argv arg; no stdin.
// Argv length is well below ARG_MAX on darwin/linux.
type ExecRunner struct {
	// Path overrides exec.LookPath("claude"). Empty means look it up
	// once per Run() call. Production leaves this empty.
	Path string
}

// Run implements Runner. Performs the LookPath check up-front so the
// "claude not on PATH" failure mode is detected before any subprocess
// dance; runs the subprocess under the caller's context; unwraps the
// envelope; returns (model-response-bytes, subprocess-stderr, err).
func (r *ExecRunner) Run(ctx context.Context, prompt string) ([]byte, []byte, error) {
	path := r.Path
	if path == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return nil, nil, ErrClaudeNotFound
		}
		path = p
	}

	cmd := exec.CommandContext(ctx, path, "-p", prompt, "--output-format=json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Spec §7.6: "kill the process group" on timeout. configureProcessGroup
	// installs platform-specific plumbing — on unix it sets SysProcAttr
	// to start a new pgid and overrides cmd.Cancel to SIGKILL the whole
	// group via syscall.Kill(-pid, ...). On windows it's a no-op (default
	// exec.CommandContext behaviour kills the direct child only; Windows
	// process-group semantics live in a different model, JobObjects,
	// which is out of scope for the §7.6 fix). See runner_unix.go /
	// runner_windows.go for the build-tagged implementations.
	configureProcessGroup(cmd)

	err := cmd.Run()
	if err != nil {
		// Distinguish timeout (context deadline exceeded) from a non-
		// zero exit, so the CLI layer can pick the right §7.6 wording.
		// We check ctx.Err() rather than testing the error chain for
		// context-cancelled because exec.CommandContext wraps the
		// signal-induced exit, and the wrap differs by platform.
		if ctx.Err() == context.DeadlineExceeded {
			return stdout.Bytes(), stderr.Bytes(), ErrTimeout
		}
		return stdout.Bytes(), stderr.Bytes(), err
	}

	return unwrapEnvelope(stdout.Bytes()), stderr.Bytes(), nil
}

// envelope mirrors the relevant fields of `claude -p
// --output-format=json`'s wire output. We accept several field names
// because the exact key has varied across `claude` versions; whichever
// is non-empty wins. Order matters when more than one is populated:
// `result` is the current canonical field, then `content`, then `text`.
type envelope struct {
	Result  string `json:"result"`
	Content string `json:"content"`
	Text    string `json:"text"`
}

// unwrapEnvelope extracts the model's response text from the wire
// envelope. On any parse failure — invalid JSON, none of the known
// fields populated — it returns the raw input unchanged so Parse's
// lenient tier has something to work with.
func unwrapEnvelope(raw []byte) []byte {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return raw
	}
	switch {
	case env.Result != "":
		return []byte(env.Result)
	case env.Content != "":
		return []byte(env.Content)
	case env.Text != "":
		return []byte(env.Text)
	default:
		return raw
	}
}

// BuildPrompt concatenates the embedded prompt template with the
// rendered CLAUDE.md content per spec §7.1's shape:
//
//	<prompt template>
//
//	---
//
//	<file path="CLAUDE.md">
//	<content>
//	</file>
//
// The returned string is passed to Runner.Run as a single argv arg.
// A trailing newline is added to content if missing so </file> always
// sits on its own line.
func BuildPrompt(content []byte) string {
	var b bytes.Buffer
	b.WriteString(promptTemplate)
	b.WriteString("\n\n---\n\n<file path=\"CLAUDE.md\">\n")
	b.Write(content)
	if len(content) == 0 || content[len(content)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("</file>\n")
	return b.String()
}

// DefaultTimeout is the default review subprocess deadline per spec
// §7.6. The CLI layer overrides this via `--review-timeout`.
const DefaultTimeout = 60 * time.Second
