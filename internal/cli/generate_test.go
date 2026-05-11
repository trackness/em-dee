package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
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

// TestGenerate_UnknownOption asserts unknown option ids hard-error.
func TestGenerate_UnknownOption(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=python", "--python-framework=elixir-phoenix", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown framework option")
	}
}

// TestGenerate_MissingRequiredLanguage asserts the helpful error when
// --language is omitted without --use-defaults (interactive lands in
// Phase 4).
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
// language category. Spec §5.3 reserves the language prompt for the
// interactive flow (Phase 4), so non-interactive callers must supply
// --language regardless of --use-defaults.
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

// TestGenerate_ExplicitEmptyRequired asserts --language= (explicit
// empty on a required category) is a hard error per spec §5.1.
func TestGenerate_ExplicitEmptyRequired(t *testing.T) {
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
// guard from spec §6.
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
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--out=" + out, "--force"})
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

// TestGenerate_RegistryLoadErrorSurfaces asserts that a registry-load
// failure at command-construction time is surfaced by RunE rather
// than silently swallowed. The previous behaviour half-populated the
// flag set without any diagnostic — users would see a `--help`
// listing missing `--language` and have no signal that the catalog
// failed to parse. RunE now returns the wrapped error first thing,
// and the command's Long description carries a one-line warning so
// the failure is visible at `--help` time too.
func TestGenerate_RegistryLoadErrorSurfaces(t *testing.T) {
	sentinel := errors.New("simulated registry load failure")
	root := NewRootCmd(Options{registryLoadErr: sentinel})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--dry-run"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error from RunE when registry load fails")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error; got: %v", err)
	}

	// `--help` also carries a one-liner so users see the failure even
	// when they never reach RunE.
	helpBuf := &bytes.Buffer{}
	helpRoot := NewRootCmd(Options{registryLoadErr: sentinel})
	helpRoot.SetOut(helpBuf)
	helpRoot.SetErr(helpBuf)
	helpRoot.SetArgs([]string{"generate", "--help"})
	if err := helpRoot.Execute(); err != nil {
		t.Fatalf("generate --help should not fail; got %v", err)
	}
	if !strings.Contains(helpBuf.String(), "failed to load embedded registry") {
		t.Errorf("expected --help to mention failed registry load; got:\n%s", helpBuf.String())
	}
}

// TestGenerate_HyphenatedLanguageFlag is the regression test for the
// flag-name → selection-key mapping. A language id that contains a
// dash (e.g. `typescript-node`) must produce a flag like
// `--typescript-node-logging` whose value lands under the dotted
// selection key `typescript-node.logging`, not `typescript.node-logging`.
// The mapping is recorded at registration time in `flags.selectionKey`,
// not derived by scanning the flag name, so this passes by construction.
func TestGenerate_HyphenatedLanguageFlag(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{
		"generate",
		"--language=typescript-node",
		"--typescript-node-logging=pino",
		"--dry-run",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "TypeScript") {
		t.Errorf("expected typescript-node base block in output:\n%s", out)
	}
	if !strings.Contains(out, "pino") {
		t.Errorf("expected pino logging block in output:\n%s", out)
	}
}

// TestGenerate_HyphenatedLanguageSelectionKeyMapping asserts the
// flag-name → selection-key map directly, locking in the contract
// that prevents the regression where the first dash was assumed to be
// the namespace separator. Register category flags onto an isolated
// flag set, then inspect the recorded map for the expected entries.
func TestGenerate_HyphenatedLanguageSelectionKeyMapping(t *testing.T) {
	reg := loadFixtureRegistry(t)
	flags := &generateFlags{
		values:       map[string]*string{},
		selectionKey: map[string]string{},
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	registerCategoryFlags(fs, reg, flags)

	cases := map[string]string{
		"language":                "language",
		"infra":                   "infra",
		"python-framework":        "python.framework",
		"python-logging":          "python.logging",
		"typescript-node-logging": "typescript-node.logging",
	}
	for flagName, want := range cases {
		got, ok := flags.selectionKey[flagName]
		if !ok {
			t.Errorf("selectionKey[%q] missing", flagName)
			continue
		}
		if got != want {
			t.Errorf("selectionKey[%q] = %q, want %q", flagName, got, want)
		}
	}
}

// TestGenerate_MultiPickCommaSeparated asserts comma-separated values
// for a multi-pick category go through the resolver and produce both
// blocks.
func TestGenerate_MultiPickCommaSeparated(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"generate", "--language=python", "--use-defaults", "--infra=docker,kubernetes", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Docker") {
		t.Errorf("expected docker block in output:\n%s", out)
	}
	if !strings.Contains(out, "k8s") {
		t.Errorf("expected kubernetes block in output:\n%s", out)
	}
}
