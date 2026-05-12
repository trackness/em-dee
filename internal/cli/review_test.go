package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/review"
)

// stubRunner is a review.Runner that returns canned bytes — lets the
// generate-level tests drive every exit-code and failure-mode branch
// without a real claude binary.
type stubRunner struct {
	stdout []byte
	stderr []byte
	err    error
}

func (s *stubRunner) Run(_ context.Context, _ string) ([]byte, []byte, error) {
	return s.stdout, s.stderr, s.err
}

// runGenerateCmd is a small test helper that builds the root, sets
// args, captures stdout/stderr, and runs Execute. Returns
// (stdout, stderr, err).
func runGenerateCmd(t *testing.T, opts Options, args []string) (string, string, error) {
	t.Helper()
	root := NewRootCmd(opts)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// TestGenerate_NoReviewFlagSkipsReview asserts that --no-review skips
// the review step entirely — the runner is never invoked, no review
// output appears on stderr.
func TestGenerate_NoReviewFlagSkipsReview(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	invoked := 0
	// The inner runner's response is irrelevant — we're asserting the
	// spy is never invoked at all under --no-review.
	inner := &stubRunner{
		stdout: []byte(`{"verdict":"problems","summary":"bad","issues":[]}`),
	}
	runnerSpy := &spyRunner{inner: inner, count: &invoked}

	opts := Options{Registry: reg, reviewRunner: runnerSpy}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--no-review",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if invoked != 0 {
		t.Errorf("review runner invoked %d times under --no-review", invoked)
	}
	if strings.Contains(stderr, "claude review") {
		t.Errorf("review output leaked under --no-review:\n%s", stderr)
	}
}

// TestGenerate_ReviewFalseFlagSkipsReview asserts that the explicit-off
// form `--review=false` skips the review with the same semantics as
// `--no-review`. Pre-M3 this flag was bound but never read, so the
// review still ran — a silent UX trap for scripts that prefer the
// `--<name>=false` idiom.
func TestGenerate_ReviewFalseFlagSkipsReview(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	invoked := 0
	inner := &stubRunner{
		stdout: []byte(`{"verdict":"problems","summary":"bad","issues":[]}`),
	}
	runnerSpy := &spyRunner{inner: inner, count: &invoked}

	opts := Options{Registry: reg, reviewRunner: runnerSpy}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--review=false",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if invoked != 0 {
		t.Errorf("review runner invoked %d times under --review=false", invoked)
	}
	if strings.Contains(stderr, "claude review") {
		t.Errorf("review output leaked under --review=false:\n%s", stderr)
	}
}

// TestGenerate_DryRunSkipsReview asserts that --dry-run does not run
// the review (the file isn't written, so review is meaningless).
func TestGenerate_DryRunSkipsReview(t *testing.T) {
	reg := loadFixtureRegistry(t)
	invoked := 0
	runner := &spyRunner{
		inner: &stubRunner{stdout: []byte(`{"verdict":"ok","summary":"ok","issues":[]}`)},
		count: &invoked,
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--dry-run",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if invoked != 0 {
		t.Errorf("review runner invoked %d times under --dry-run", invoked)
	}
}

// TestGenerate_ReviewOK asserts a verdict:"ok" runner result renders
// the review and returns nil error → exit 0 mapped by Execute.
func TestGenerate_ReviewOK(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stdout: []byte(`{"verdict":"ok","summary":"all good","issues":[]}`),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr, "all good") {
		t.Errorf("review summary missing from stderr:\n%s", stderr)
	}
}

// TestGenerate_ReviewProblemsExits2 asserts the exit-code rule:
// verdict:"problems" → exit 2 via the exitCodeError seam.
func TestGenerate_ReviewProblemsExits2(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stdout: []byte(`{"verdict":"problems","summary":"bad","issues":[{"severity":"error","location":"X","issue":"i","suggestion":"s"}]}`),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err == nil {
		t.Fatal("expected exitCodeError on verdict:problems")
	}
	var ec *exitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected exitCodeError; got %T %v", err, err)
	}
	if ec.code != 2 {
		t.Errorf("exit code: got %d want 2", ec.code)
	}
}

// TestGenerate_ReviewWarningsExits0 asserts warnings exits 0 (only
// problems is non-zero; --strict-review is deferred to v2).
func TestGenerate_ReviewWarningsExits0(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stdout: []byte(`{"verdict":"warnings","summary":"meh","issues":[]}`),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Errorf("expected nil error on warnings; got %v", err)
	}
}

// TestGenerate_ReviewParseFailureExits0 asserts tier 3 (unstructured)
// renders the raw response and exits 0 (parse failure is best-effort,
// exit 0).
func TestGenerate_ReviewParseFailureExits0(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stdout: []byte("totally not JSON at all"),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Errorf("expected nil error on tier-3 parse; got %v", err)
	}
	if !strings.Contains(stderr, "unstructured") {
		t.Errorf("unstructured header missing from stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "totally not JSON at all") {
		t.Errorf("raw response missing from stderr:\n%s", stderr)
	}
}

// TestGenerate_ReviewOutTier1WritesParsedJSON asserts --review-out
// writes the parsed JSON when parsing reached tier 1 or 2.
func TestGenerate_ReviewOutTier1WritesParsedJSON(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")
	rev := filepath.Join(tmp, "review.json")

	runner := &stubRunner{
		stdout: []byte(`{"verdict":"warnings","summary":"hi","issues":[]}`),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--review-out=" + rev,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(rev)
	if err != nil {
		t.Fatalf("read review-out: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal review-out: %v\nraw: %s", err, data)
	}
	if got["verdict"] != "warnings" {
		t.Errorf("verdict: got %v want warnings", got["verdict"])
	}
	if got["summary"] != "hi" {
		t.Errorf("summary: got %v want hi", got["summary"])
	}
}

// TestGenerate_ReviewOutTier3WritesSentinel asserts --review-out
// writes the unstructured sentinel JSON shape verbatim when parsing
// falls to tier 3.
func TestGenerate_ReviewOutTier3WritesSentinel(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")
	rev := filepath.Join(tmp, "review.json")

	rawText := "this is not JSON\n"
	runner := &stubRunner{stdout: []byte(rawText)}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--review-out=" + rev,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(rev)
	if err != nil {
		t.Fatalf("read review-out: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal review-out: %v\nraw: %s", err, data)
	}
	if got["verdict"] != "unstructured" {
		t.Errorf("verdict: got %v want unstructured", got["verdict"])
	}
	if got["summary"] != "claude review could not be parsed as structured JSON" {
		t.Errorf("summary: got %v", got["summary"])
	}
	if got["raw"] != rawText {
		t.Errorf("raw: got %q want %q", got["raw"], rawText)
	}
	issues, ok := got["issues"].([]any)
	if !ok || len(issues) != 0 {
		t.Errorf("issues: got %v want []", got["issues"])
	}
}

// TestGenerate_MissingClaudePrintsNote asserts the "claude not on
// PATH" failure mode: print a note on stderr, exit 0.
func TestGenerate_MissingClaudePrintsNote(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{err: review.ErrClaudeNotFound}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Errorf("expected nil err; got %v", err)
	}
	if !strings.Contains(stderr, "claude CLI not found") {
		t.Errorf("expected 'claude CLI not found' note; got:\n%s", stderr)
	}
}

// TestGenerate_ReviewTimeoutPrintsNote asserts the timeout failure
// mode: print a note, exit 0.
func TestGenerate_ReviewTimeoutPrintsNote(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{err: review.ErrTimeout}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Errorf("expected nil err; got %v", err)
	}
	if !strings.Contains(stderr, "timed out") {
		t.Errorf("expected 'timed out' note; got:\n%s", stderr)
	}
}

// TestGenerate_ReviewNonZeroExitPrintsStderr asserts the non-zero
// exit failure mode: print stderr verbatim under a failure header,
// exit 0.
func TestGenerate_ReviewNonZeroExitPrintsStderr(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stderr: []byte("boom: API key invalid\n"),
		err:    errors.New("exit status 1"),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, stderr, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults", "--out=" + out,
	})
	if err != nil {
		t.Errorf("expected nil err; got %v", err)
	}
	if !strings.Contains(stderr, "claude review failed") {
		t.Errorf("expected 'claude review failed' header; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "API key invalid") {
		t.Errorf("expected stderr passthrough; got:\n%s", stderr)
	}
}

// TestGenerate_ReviewTimeoutFlag asserts --review-timeout=<duration>
// parses cleanly. The actual timeout firing is exercised by the
// runner-level tests; this is a CLI smoke that the flag is wired.
func TestGenerate_ReviewTimeoutFlag(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{
		stdout: []byte(`{"verdict":"ok","summary":"ok","issues":[]}`),
	}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--review-timeout=30s",
	})
	if err != nil {
		t.Errorf("expected nil err with --review-timeout=30s; got %v", err)
	}
}

// TestGenerate_ReviewTimeoutFlagInvalid asserts a bad duration is a
// hard error (before the file gets written).
func TestGenerate_ReviewTimeoutFlagInvalid(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	runner := &stubRunner{stdout: []byte(`{"verdict":"ok","summary":"ok","issues":[]}`)}
	opts := Options{Registry: reg, reviewRunner: runner}
	_, _, err := runGenerateCmd(t, opts, []string{
		"generate", "--language=python", "--use-defaults",
		"--out=" + out, "--review-timeout=blueberry",
	})
	if err == nil {
		t.Errorf("expected error on --review-timeout=blueberry")
	}
}

// spyRunner counts how many times Run is invoked on its inner runner.
// Used to verify that --no-review / --dry-run actually short-circuit
// before the review step.
type spyRunner struct {
	inner review.Runner
	count *int
}

func (s *spyRunner) Run(ctx context.Context, p string) ([]byte, []byte, error) {
	*s.count++
	return s.inner.Run(ctx, p)
}
