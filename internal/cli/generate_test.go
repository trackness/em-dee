package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
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

// TestCheckRequiredTree_FlagsNestedMissingRequired pins H1: a required
// category nested under a chosen container option is caught by the
// recursive required-check. Pre-fix, the check walked only top-level
// categories and silently let nested required-empty cells slip past,
// producing a truncated CLAUDE.md.
func TestCheckRequiredTree_FlagsNestedMissingRequired(t *testing.T) {
	t.Parallel()

	// Synthesise a registry with a container category carrying a
	// required leaf under one of its options. Picks deliberately omits
	// the required leaf, so checkRequiredTree must surface a clear
	// dotted-key error.
	requiredLeaf := &registry.Category{
		ID:          "framework",
		DisplayName: "Framework",
		Pick:        registry.PickSingle,
		Required:    true,
		Options: []registry.Option{
			{ID: "typer", DisplayName: "Typer", File: "typer.md"},
		},
	}
	typeContainer := &registry.Category{
		ID:          "type",
		DisplayName: "Program type",
		Pick:        registry.PickSingle,
		IsContainer: true,
		Options: []registry.Option{
			{ID: "cli", DisplayName: "CLI", File: "cli/base.md"},
		},
		Subcategories: map[string][]*registry.Category{
			"cli": {requiredLeaf},
		},
	}

	picks := registry.NewPicks()
	picks.Values["type"] = registry.NewSingle("cli")
	// Note: NO picks for `cli.framework` — that's the bug we're catching.

	err := checkRequiredTree(picks, typeContainer, "")
	if err == nil {
		t.Fatal("expected an error for missing nested required category")
	}
	// The error must name the nested key (cli.framework), not just the
	// top-level container.
	if !strings.Contains(err.Error(), "cli.framework") {
		t.Errorf("error should reference the nested dotted key cli.framework; got: %v", err)
	}
}

// TestCheckRequiredTree_AcceptsPopulatedSubtree asserts the happy path:
// when every required cell at every depth has a value, the check
// returns nil.
func TestCheckRequiredTree_AcceptsPopulatedSubtree(t *testing.T) {
	t.Parallel()

	requiredLeaf := &registry.Category{
		ID:          "framework",
		DisplayName: "Framework",
		Pick:        registry.PickSingle,
		Required:    true,
		Options: []registry.Option{
			{ID: "typer", DisplayName: "Typer", File: "typer.md"},
		},
	}
	typeContainer := &registry.Category{
		ID:          "type",
		DisplayName: "Program type",
		Pick:        registry.PickSingle,
		IsContainer: true,
		Options: []registry.Option{
			{ID: "cli", DisplayName: "CLI", File: "cli/base.md"},
		},
		Subcategories: map[string][]*registry.Category{
			"cli": {requiredLeaf},
		},
	}

	picks := registry.NewPicks()
	picks.Values["type"] = registry.NewSingle("cli")
	picks.Values["cli.framework"] = registry.NewSingle("typer")

	if err := checkRequiredTree(picks, typeContainer, ""); err != nil {
		t.Errorf("expected nil for fully-populated picks; got %v", err)
	}
}

// TestCheckRequiredTree_SkipsUnchosenContainerOption asserts that a
// required leaf under a container option that wasn't picked does NOT
// fire — the subtree is dark when the container option isn't chosen,
// so its required cells aren't in scope. A required container with no
// pick is still rejected at the container itself (covered by
// TestCheckRequiredTree_FlagsTopLevelRequiredEmpty).
func TestCheckRequiredTree_SkipsUnchosenContainerOption(t *testing.T) {
	t.Parallel()

	requiredLeaf := &registry.Category{
		ID:          "framework",
		DisplayName: "Framework",
		Pick:        registry.PickSingle,
		Required:    true,
		Options: []registry.Option{
			{ID: "typer", DisplayName: "Typer", File: "typer.md"},
		},
	}
	typeContainer := &registry.Category{
		ID:          "type",
		DisplayName: "Program type",
		Pick:        registry.PickSingle,
		IsContainer: true,
		Options: []registry.Option{
			{ID: "cli", DisplayName: "CLI", File: "cli/base.md"},
			{ID: "library", DisplayName: "Library", File: "library/base.md"},
		},
		Subcategories: map[string][]*registry.Category{
			"cli":     {requiredLeaf},
			"library": nil,
		},
	}

	picks := registry.NewPicks()
	picks.Values["type"] = registry.NewSingle("library") // not cli

	if err := checkRequiredTree(picks, typeContainer, ""); err != nil {
		t.Errorf("unchosen container option's required leaves are out of scope; got: %v", err)
	}
}

// TestCheckRequiredTree_FlagsTopLevelRequiredEmpty preserves the pre-
// existing top-level required-empty rejection.
func TestCheckRequiredTree_FlagsTopLevelRequiredEmpty(t *testing.T) {
	t.Parallel()

	requiredCat := &registry.Category{
		ID:          "language",
		DisplayName: "Language",
		Pick:        registry.PickSingle,
		Required:    true,
		Options: []registry.Option{
			{ID: "python", DisplayName: "Python", File: "python/base.md"},
		},
	}
	picks := registry.NewPicks()
	// language is required but unset.

	err := checkRequiredTree(picks, requiredCat, "")
	if err == nil {
		t.Fatal("expected an error for missing top-level required category")
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("error should reference the language key; got: %v", err)
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
