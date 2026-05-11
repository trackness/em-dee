package tui

import (
	"os"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// validFixture loads the registry's testdata/valid tree relative to the
// tui package. We reach across packages because the registry package
// already curates a complete, valid fixture with multiple languages
// and a representative sub-category tree — duplicating it inside the
// tui package would be premature per CLAUDE.md principle 2.
func validFixture(t *testing.T) *registry.Registry {
	t.Helper()
	fsys := os.DirFS("../registry/testdata/valid")
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return reg
}

// TestBuildLanguageForm_ConstructsForm asserts the form-1 builder
// returns a non-nil form for a registry with a language category that
// has options. The Run path is exercised manually (live TTY) and not
// unit-tested.
func TestBuildLanguageForm_ConstructsForm(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	var out string
	form, err := BuildLanguageForm(reg, &out)
	if err != nil {
		t.Fatalf("BuildLanguageForm: %v", err)
	}
	if form == nil {
		t.Fatal("BuildLanguageForm returned nil form")
	}
}

// TestBuildLanguageForm_NilRegistry asserts the builder rejects a nil
// registry with a clear error rather than panicking on a nil deref.
func TestBuildLanguageForm_NilRegistry(t *testing.T) {
	t.Parallel()

	var out string
	if _, err := BuildLanguageForm(nil, &out); err == nil {
		t.Fatal("expected error for nil registry, got nil")
	}
}

// TestBuildLanguageForm_NilOut asserts the builder rejects a nil out
// pointer — without one, huh.Select.Value would panic at .Run() time
// with no useful stack frame.
func TestBuildLanguageForm_NilOut(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	if _, err := BuildLanguageForm(reg, nil); err == nil {
		t.Fatal("expected error for nil out, got nil")
	}
}

// TestBuildLanguageForm_NoLanguageCategory asserts the builder
// surfaces a clear error against a registry with no language
// category (e.g. the production empty-embedded FS until Phase 7).
func TestBuildLanguageForm_NoLanguageCategory(t *testing.T) {
	t.Parallel()

	reg := &registry.Registry{}
	var out string
	if _, err := BuildLanguageForm(reg, &out); err == nil {
		t.Fatal("expected error for empty registry, got nil")
	}
}
