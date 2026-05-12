package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// loadThreeLevelFixture loads the three-level test fixture used by
// the engine's recursive-walk tests. The fixture is shared with the
// registry-package tests (`internal/registry/testdata/three-level/`)
// so the CLI exercises the same on-disk shape.
func loadThreeLevelFixture(t *testing.T) *registry.Registry {
	t.Helper()
	fsys := os.DirFS("../registry/testdata/three-level")
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return reg
}

// TestShow_ThreeLevel_DeepDottedRef exercises the show resolver's
// recursive walk through container categories: `python.cli.framework.typer`
// elides the type container's own id (per CONTENT-STYLE.md §2.3) and
// resolves through python (language container) → cli (type container
// option) → framework (leaf) → typer (option). The resolved block
// must come back non-empty.
func TestShow_ThreeLevel_DeepDottedRef(t *testing.T) {
	reg := loadThreeLevelFixture(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "python.cli.framework.typer"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Typer framework block.") {
		t.Errorf("expected typer block content; got:\n%s", buf.String())
	}
}

// TestShow_ThreeLevel_ContainerOptBase exercises the
// `<container-opt>.<container-opt>` shape — resolving to the
// type-scope `base.md` (e.g. `python.cli` returns the CLI type's
// base.md content) without specifying any deeper segment.
func TestShow_ThreeLevel_ContainerOptBase(t *testing.T) {
	reg := loadThreeLevelFixture(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"show", "python.type.cli"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "CLI type base block.") {
		t.Errorf("expected cli type base block; got:\n%s", buf.String())
	}
}

// TestShow_ThreeLevel_UnknownDeepRef asserts the resolver reports a
// clear error when a deep dotted ref names a non-existent option or
// sub-category at any depth.
func TestShow_ThreeLevel_UnknownDeepRef(t *testing.T) {
	cases := []string{
		"python.cli.framework.ghost",    // unknown option at the deepest leaf
		"python.cli.no-such-cat.typer",  // unknown sub-category under the chosen container option
		"python.kotlin.framework.typer", // unknown container option (no "kotlin" type)
	}
	for _, ref := range cases {
		t.Run(ref, func(t *testing.T) {
			reg := loadThreeLevelFixture(t)
			root := NewRootCmd(Options{Registry: reg})
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			root.SetErr(buf)
			root.SetArgs([]string{"show", ref})
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for %q, got success\noutput: %s", ref, buf.String())
			}
			if !strings.Contains(err.Error(), ref) {
				t.Errorf("error should mention the ref %q; got: %v", ref, err)
			}
		})
	}
}

// TestList_ThreeLevel_RendersTree asserts `em-dee list` walks the
// three-level fixture all the way down AND emits sub-categories in
// NN-prefix render order. The previous presence-only assertion
// allowed the H2 ordering regression to slip past CI: `framework`
// (10-framework) and `consumer` (20-consumer) were both present, but
// in alphabetical order rather than the documented folder-prefix
// order. The order-pin below catches that on its way past.
func TestList_ThreeLevel_RendersTree(t *testing.T) {
	reg := loadThreeLevelFixture(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()

	// Presence check (each expected substring must appear at least once).
	wants := []string{
		"language",
		"python",
		"type",
		"cli",
		"framework",
		"typer",
		"consumer",
		"human",
		"library",
		"logging",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q\n---\n%s", want, out)
		}
	}

	// Order check: the sub-categories under cli are `10-framework` then
	// `20-consumer`, so the category header line for `framework` must
	// appear before the category header line for `consumer`. Looking
	// for the bracketed `framework [single]` / `consumer [single]`
	// header keeps the assertion from coincidentally matching the
	// option-list lines (`- consumer`) under another category.
	frameworkIdx := strings.Index(out, "framework [single]")
	consumerIdx := strings.Index(out, "consumer [single]")
	if frameworkIdx < 0 || consumerIdx < 0 {
		t.Fatalf("missing one of the expected category headers (framework=%d, consumer=%d):\n%s",
			frameworkIdx, consumerIdx, out)
	}
	if frameworkIdx >= consumerIdx {
		t.Errorf("sub-categories must render in NN-prefix order (framework before consumer); got framework@%d, consumer@%d\n---\n%s",
			frameworkIdx, consumerIdx, out)
	}
}
