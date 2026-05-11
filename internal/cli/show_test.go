package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestShow_LanguageBase exercises `em-dee show language.<lang>` which
// resolves to `<lang>/base.md`.
func TestShow_LanguageBase(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "language.python"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Python") {
		t.Errorf("expected python base.md to contain 'Python'; got:\n%s", buf.String())
	}
}

// TestShow_LanguageNestedOption exercises `<lang>.<cat>.<opt>` which
// resolves to a sub-category option block.
func TestShow_LanguageNestedOption(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "python.logging.loguru"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// loguru fixture is non-empty; just check it produced something.
	if len(strings.TrimSpace(buf.String())) == 0 {
		t.Errorf("expected non-empty loguru block; got empty")
	}
}

// TestShow_TopLevelOption exercises `<cat>.<opt>` for a top-level
// category.
func TestShow_TopLevelOption(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "infra.docker"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(strings.TrimSpace(buf.String())) == 0 {
		t.Errorf("expected non-empty docker block; got empty")
	}
}

// TestShow_UnknownRef must produce a clear error for refs that don't
// resolve.
func TestShow_UnknownRef(t *testing.T) {
	cases := []string{
		"nonexistent",
		"infra.does-not-exist",
		"language.kotlin",
		"python.framework.elixir-phoenix",
		"python.framework", // resolves to a category, not a leaf
	}
	for _, ref := range cases {
		t.Run(ref, func(t *testing.T) {
			reg := loadFixtureRegistry(t)
			root := NewRootCmd(Options{Registry: reg})
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			root.SetErr(buf)
			root.SetArgs([]string{"show", ref})
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for ref %q, got success\noutput: %s", ref, buf.String())
			}
			if !strings.Contains(err.Error(), ref) {
				t.Errorf("error should mention the ref %q; got: %v", ref, err)
			}
		})
	}
}

// TestShow_NoFrameworkInFixture exercises `go.framework.gin` — we
// don't have a go.framework category in the fixture, so this test
// verifies the resolver fails cleanly (rather than the block-found
// happy path).
func TestShow_NoFrameworkInFixture(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "go.framework.gin"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected go.framework.gin to fail in fixture (no framework cat under go)")
	}
}
