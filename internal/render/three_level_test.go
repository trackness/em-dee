package render

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"testing/fstest"

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

// TestRender_ThreeLevel_ContainerOptionNoBase exercises the
// alternate container-option shape: `file: <opt>/` (no scope base).
// The renderer must NOT emit a scope-base block when the option's
// `file:` doesn't reference one — only the chosen leaf blocks
// underneath should appear.
func TestRender_ThreeLevel_ContainerOptionNoBase(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"templates/10-language/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)},
		"templates/10-language/python/base.md": &fstest.MapFile{Data: []byte("## Python\n\nPython base block.\n")},
		"templates/10-language/python/10-type/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: single
required: false
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: cli/
`)},
		"templates/10-language/python/10-type/cli/10-framework/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Framework"
pick: single
required: false
options:
  - id: typer
    display_name: "Typer"
    description: "Typer"
    file: typer.md
`)},
		"templates/10-language/python/10-type/cli/10-framework/typer.md": &fstest.MapFile{Data: []byte("### Typer\n\nTyper framework block.\n")},
	}
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.type"] = registry.NewSingle("cli")
	picks.Values["python.cli.framework"] = registry.NewSingle("typer")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Must contain both: language base + framework leaf.
	for _, want := range []string{"Python base block.", "Typer framework block."} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("missing block %q in:\n%s", want, got)
		}
	}
	// Must NOT contain a CLI-type base.md — there is none on disk
	// and the option declared `file: cli/` (no scope-base).
	if bytes.Contains(got, []byte("CLI type base block.")) {
		t.Errorf("renderer emitted a CLI scope-base despite file: cli/ declaring none:\n%s", got)
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
