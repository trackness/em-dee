package review

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestBuildPrompt_ConcatenatesTemplateAndContent asserts the prompt
// passed to claude is the embedded template + the rendered content,
// separated by the spec §7.1 fenced delimiter.
func TestBuildPrompt_ConcatenatesTemplateAndContent(t *testing.T) {
	t.Parallel()
	content := []byte("# Generated CLAUDE.md\n\nbody body\n")
	got := BuildPrompt(content)

	// Template appears at the start.
	if !strings.HasPrefix(got, PromptTemplate()) {
		t.Errorf("BuildPrompt output does not start with the embedded template")
	}
	// The content appears inside the <file> wrapper.
	if !strings.Contains(got, "# Generated CLAUDE.md") {
		t.Errorf("BuildPrompt output missing content body")
	}
	// The file wrapper is present.
	if !strings.Contains(got, `<file path="CLAUDE.md">`) {
		t.Errorf("BuildPrompt output missing <file path=\"CLAUDE.md\"> open tag")
	}
	if !strings.Contains(got, `</file>`) {
		t.Errorf("BuildPrompt output missing </file> close tag")
	}
}

// TestBuildPrompt_AddsTrailingNewlineIfMissing asserts the file wrapper
// has a clean newline before </file> even when the rendered content
// doesn't end with one. Otherwise the close tag could end up appended
// to the last line of the file, which Claude might interpret as part
// of the file content.
func TestBuildPrompt_AddsTrailingNewlineIfMissing(t *testing.T) {
	t.Parallel()
	got := BuildPrompt([]byte("no trailing newline"))
	if !strings.Contains(got, "no trailing newline\n</file>") {
		t.Errorf("expected newline between content and </file>; got:\n%s", got)
	}
}

// TestExecRunner_ClaudeMissingReturnsSentinel asserts the LookPath
// failure path: when `claude` is not on PATH, the runner returns
// ErrClaudeNotFound without trying to spawn anything.
//
// We force LookPath to fail by pointing PATH at an empty directory.
// This is OS-independent.
func TestExecRunner_ClaudeMissingReturnsSentinel(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := &ExecRunner{}
	_, _, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrClaudeNotFound) {
		t.Errorf("got %v; want ErrClaudeNotFound", err)
	}
}

// TestExecRunner_TimeoutReturnsSentinel asserts that a context deadline
// surfaces as ErrTimeout rather than a generic exec error. We use a
// minimal shell-script stub on PATH that sleeps; on platforms without
// /bin/sh this test is skipped.
func TestExecRunner_TimeoutReturnsSentinel(t *testing.T) {
	if _, err := lookSh(); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	dir := t.TempDir()
	// A `claude` script that sleeps longer than the test timeout.
	// Use the absolute path to `sleep` because the test overrides PATH
	// to just `dir`, so `/bin/sh` (kernel-interpreted shebang) would
	// otherwise fail to resolve `sleep` and exit 127 before the
	// deadline fires.
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep on PATH: %v", err)
	}
	if err := writeExec(dir, "claude", "#!/bin/sh\nexec "+sleepPath+" 5\n"); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := &ExecRunner{}
	_, _, runErr := r.Run(ctx, "prompt")
	if !errors.Is(runErr, ErrTimeout) {
		t.Errorf("got %v; want ErrTimeout", runErr)
	}
}

// TestExecRunner_UnwrapEnvelopeResult asserts the `--output-format=json`
// envelope's `.result` field becomes the runner's stdout. The model's
// raw response is what callers see; the wire envelope is stripped.
func TestExecRunner_UnwrapEnvelopeResult(t *testing.T) {
	if _, err := lookSh(); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	dir := t.TempDir()
	// Stub claude prints a wire envelope to stdout. We use single quotes
	// around the JSON so the shell doesn't interpret braces.
	body := "#!/bin/sh\n" +
		`printf '%s' '{"result":"{\"verdict\":\"ok\",\"summary\":\"good\",\"issues\":[]}"}'` + "\n"
	if err := writeExec(dir, "claude", body); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	r := &ExecRunner{}
	stdout, _, err := r.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := `{"verdict":"ok","summary":"good","issues":[]}`
	if string(stdout) != want {
		t.Errorf("unwrap result: got %q want %q", stdout, want)
	}
}

// TestExecRunner_UnwrapEnvelopeFallthrough asserts that when the wire
// envelope is unrecognised (none of the known field names populated),
// the runner returns the raw bytes so the parse module's lenient tier
// has a shot.
func TestExecRunner_UnwrapEnvelopeFallthrough(t *testing.T) {
	if _, err := lookSh(); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	dir := t.TempDir()
	body := "#!/bin/sh\n" +
		`printf '%s' '{"some_other_key":"value"}'` + "\n"
	if err := writeExec(dir, "claude", body); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	r := &ExecRunner{}
	stdout, _, err := r.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(stdout), "some_other_key") {
		t.Errorf("expected raw envelope bytes on fallthrough; got %q", stdout)
	}
}

// TestExecRunner_NonZeroExitReturnsError asserts a non-zero claude exit
// surfaces as an exec.ExitError (or wrapped equivalent), distinct from
// the ErrTimeout / ErrClaudeNotFound sentinels.
func TestExecRunner_NonZeroExitReturnsError(t *testing.T) {
	if _, err := lookSh(); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	dir := t.TempDir()
	body := "#!/bin/sh\necho 'boom' >&2\nexit 7\n"
	if err := writeExec(dir, "claude", body); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	r := &ExecRunner{}
	_, stderr, err := r.Run(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
	if errors.Is(err, ErrTimeout) || errors.Is(err, ErrClaudeNotFound) {
		t.Errorf("unexpected sentinel for non-zero exit: %v", err)
	}
	if !strings.Contains(string(stderr), "boom") {
		t.Errorf("stderr from stub not propagated; got %q", stderr)
	}
}
