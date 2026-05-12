package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_DryRunHappyPath asserts that --language=python --use-defaults
// --dry-run produces non-empty markdown on stdout without touching the
// filesystem.
func TestGenerate_DryRunHappyPath(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Python") {
		t.Errorf("expected python content in output; got:\n%s", buf.String())
	}
}

// TestGenerate_UnknownLanguage asserts unknown language ids hard-error
// through ResolveSelection's option-id validation.
func TestGenerate_UnknownLanguage(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=elixir", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown language option")
	}
}

// TestGenerate_MissingRequiredLanguage asserts the helpful error when
// --language is omitted without --use-defaults (interactive flow needs
// a TTY, which tests don't have).
func TestGenerate_MissingRequiredLanguage(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for missing language")
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("error should mention language; got: %v", err)
	}
}

// TestGenerate_UseDefaultsWithoutLanguageIsClear asserts that
// `--use-defaults` without `--language` produces a single clear
// error up front, rather than letting the pipeline reach the deeper
// "required category not set and no default available" check on the
// language category. The language prompt is reserved for the
// interactive flow, so non-interactive callers must supply --language
// regardless of --use-defaults.
func TestGenerate_UseDefaultsWithoutLanguageIsClear(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--use-defaults", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for --use-defaults without --language")
	}
	if !strings.Contains(err.Error(), "--language") {
		t.Errorf("error should name --language; got: %v", err)
	}
	if !strings.Contains(err.Error(), "--use-defaults") {
		t.Errorf("error should name --use-defaults so the user understands the interaction; got: %v", err)
	}
}

// TestGenerate_EmptyLanguageRejected asserts --language= (cobra-changed
// flag with empty value) is a hard error: the language category is
// required and the resolver rejects required-empty.
func TestGenerate_EmptyLanguageRejected(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for --language= on required category")
	}
}

// TestGenerate_ExistingFileErrorsWithoutForce asserts the existing-file
// guard.
func TestGenerate_ExistingFileErrorsWithoutForce(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(out, []byte("preexisting\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--out=" + out})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for existing file without --force")
	}
	if !strings.Contains(err.Error(), "exists") && !strings.Contains(err.Error(), "force") {
		t.Errorf("error should mention existing file / --force; got: %v", err)
	}

	// Original file is unchanged.
	data, _ := os.ReadFile(out)
	if string(data) != "preexisting\n" {
		t.Errorf("original file modified: %q", string(data))
	}
}

// TestGenerate_ForceBacksUpExistingFile asserts --force renames the
// existing file to CLAUDE.md.bak.<unix-ts> before writing.
func TestGenerate_ForceBacksUpExistingFile(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(out, []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	// --no-review keeps this test focused on write semantics; the
	// review path is covered by review_test.go.
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--out=" + out, "--force", "--no-review"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// New file has fresh content.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if !strings.Contains(string(data), "Python") {
		t.Errorf("output file missing python content:\n%s", string(data))
	}

	// Backup exists alongside.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("readdir tmp: %v", err)
	}
	var foundBackup bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "CLAUDE.md.bak.") {
			foundBackup = true
			b, _ := os.ReadFile(filepath.Join(tmp, e.Name()))
			if string(b) != "old content\n" {
				t.Errorf("backup content mismatch: %q", string(b))
			}
		}
	}
	if !foundBackup {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("no CLAUDE.md.bak.* found; tmp contents: %v", names)
	}
}

// TestGenerate_DryRunSkipsExistenceCheck asserts --dry-run bypasses the
// existing-file guard (writes to stdout, never touches the file).
func TestGenerate_DryRunSkipsExistenceCheck(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(out, []byte("preexisting\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--out=" + out, "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Stdout has the rendered content.
	if !strings.Contains(buf.String(), "Python") {
		t.Errorf("dry-run stdout missing python content:\n%s", buf.String())
	}
	// Original file untouched.
	data, _ := os.ReadFile(out)
	if string(data) != "preexisting\n" {
		t.Errorf("dry-run modified the file: %q", string(data))
	}
}

// TestGenerate_SuccessLineOnStderr asserts the post-write success
// line is emitted to stderr after a non-dry-run write. The line
// carries the path, block count, and KB size.
func TestGenerate_SuccessLineOnStderr(t *testing.T) {
	reg := loadFixtureRegistry(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "CLAUDE.md")

	root := NewRootCmd(Options{Registry: reg})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	// --no-review focuses this test on the success-line wording; the
	// review path is covered by review_test.go.
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--out=" + out, "--no-review"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := stderr.String()
	for _, want := range []string{"wrote " + out, "blocks", "KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("success line missing %q: %q", want, got)
		}
	}
}

// TestGenerate_DryRunNoSuccessLine asserts --dry-run does not emit
// the success line — dry-run is a pipeline-friendly path and adding
// stderr chatter on top would confuse tooling that wraps em-dee.
func TestGenerate_DryRunNoSuccessLine(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(stderr.String(), "wrote ") {
		t.Errorf("dry-run unexpectedly emitted success line: %q", stderr.String())
	}
}

// TestIsInteractive_NonTTYInTests asserts the predicate returns false
// under `go test`, where stdin/stdout are pipes. This is the seam
// that keeps existing tests' non-interactive flow stable.
func TestIsInteractive_NonTTYInTests(t *testing.T) {
	t.Parallel()
	if isInteractive() {
		t.Error("isInteractive() = true under go test; expected false (stdin/stdout are pipes)")
	}
}

// TestGenerate_InteractiveGateFallsThroughOnNonTTY asserts that with
// no --language and no --use-defaults, on a non-TTY (test process),
// the command takes the hard-error path. This locks in the
// predicate's role as the interactive↔non-interactive gate so a
// regression that flips it doesn't silently spawn huh in CI.
func TestGenerate_InteractiveGateFallsThroughOnNonTTY(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on non-TTY without --language")
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("error should mention language; got: %v", err)
	}
}
