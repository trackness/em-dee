package render

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// threeLevelRegistry loads the three-level fixture under
// `internal/render/testdata/three-level/`. The fixture is a parallel
// copy of `internal/registry/testdata/three-level/` so the registry
// tests and render tests stay independent of each other's fixture
// drift (per CLAUDE.md "Test seams" / "Templates filesystem" rules).
func threeLevelRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	fsys := os.DirFS("testdata/three-level")
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS(three-level) returned error: %v", err)
	}
	return reg
}

// TestRender_ThreeLevel_CLIFullChain exercises the renderer's
// recursive container walk: choose language=python, type=cli,
// cli.framework=typer, cli.consumer=human. The output must contain
// the language base, the type base, and both leaf blocks, in
// renderer-defined order (folder prefix everywhere, container
// chosen-option's base.md immediately before its subtree).
func TestRender_ThreeLevel_CLIFullChain(t *testing.T) {
	t.Parallel()
	reg := threeLevelRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.type"] = registry.NewSingle("cli")
	picks.Values["python.cli.framework"] = registry.NewSingle("typer")
	picks.Values["python.cli.consumer"] = registry.NewSingle("human")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Expected order: python base → cli base → typer → human → docker
	// (docker isn't picked, so it shouldn't be present).
	expectedOrder := []string{
		"Python base block.",
		"CLI type base block.",
		"Typer framework block.",
		"Human consumer block.",
	}
	lastIdx := -1
	for _, marker := range expectedOrder {
		idx := bytes.Index(got, []byte(marker))
		if idx < 0 {
			t.Fatalf("missing block %q in:\n%s", marker, got)
		}
		if idx <= lastIdx {
			t.Fatalf("block %q out of order (idx=%d, prev=%d):\n%s", marker, idx, lastIdx, got)
		}
		lastIdx = idx
	}
	// Sister-type discipline must not leak in.
	if strings.Contains(string(got), "Library type base block.") {
		t.Errorf("library type base.md leaked into cli-picked output")
	}
}

// TestRender_ThreeLevel_LibraryNoSubtree exercises a container option
// whose chosen subtree has no further categories: type=library emits
// just the library base, no framework or consumer blocks.
func TestRender_ThreeLevel_LibraryNoSubtree(t *testing.T) {
	t.Parallel()
	reg := threeLevelRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.type"] = registry.NewSingle("library")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Python base block.", "Library type base block."} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("missing block %q in:\n%s", want, got)
		}
	}
	for _, banned := range []string{"CLI type base block.", "Typer framework block.", "Click framework block.", "Human consumer block.", "Agent consumer block."} {
		if bytes.Contains(got, []byte(banned)) {
			t.Errorf("unexpected block %q present in library-only output:\n%s", banned, got)
		}
	}
}

// TestRender_ThreeLevel_TypeUnsetSkipsContainerSubtree pins the
// renderer's "container with no chosen option emits nothing" rule:
// if the type pick is absent, neither the type's base.md nor any of
// its sub-categories should render.
func TestRender_ThreeLevel_TypeUnsetSkipsContainerSubtree(t *testing.T) {
	t.Parallel()
	reg := threeLevelRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	// python.type deliberately unset.

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(got, []byte("Python base block.")) {
		t.Errorf("python base.md missing:\n%s", got)
	}
	for _, banned := range []string{"CLI type base block.", "Library type base block.", "Typer framework block.", "Human consumer block."} {
		if bytes.Contains(got, []byte(banned)) {
			t.Errorf("unexpected subtree block %q present despite python.type unset:\n%s", banned, got)
		}
	}
}
