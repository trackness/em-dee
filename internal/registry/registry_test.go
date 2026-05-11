package registry

import (
	"testing"
	"testing/fstest"
)

// TestLoad_EmptyEmbedded asserts the embedded production templates
// filesystem (currently only `.gitkeep`) loads to an empty Registry
// without error. This is the Task 1.2 verification step.
func TestLoad_EmptyEmbedded(t *testing.T) {
	t.Parallel()

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if reg == nil {
		t.Fatal("Load() returned nil registry")
	}
	if len(reg.Categories) != 0 {
		t.Fatalf("expected empty Registry, got %d categories", len(reg.Categories))
	}
}

// TestLoad_EmptyFixture asserts the test-injectable `load` reports an
// empty Registry when pointed at an empty fixture FS — the same
// behaviour as the embedded-empty case, but exercised against a fully
// in-memory filesystem so it's not coupled to the embed directive.
func TestLoad_EmptyFixture(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	reg, err := load(fsys, "templates")
	if err != nil {
		t.Fatalf("load() returned error: %v", err)
	}
	if len(reg.Categories) != 0 {
		t.Fatalf("expected empty Registry, got %d categories", len(reg.Categories))
	}
}
