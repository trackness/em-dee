package render

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// fixtureRegistry loads `internal/render/testdata/templates/` via
// os.DirFS. This is a render-package-local fixture, deliberately
// independent of `internal/registry/templates/` so render tests don't
// churn as the production catalog is filled in (Phase 7).
func fixtureRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	fsys := os.DirFS("testdata")
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS(fixture) returned error: %v", err)
	}
	return reg
}

// TestRender_LanguageOnly: pick a language, leave every optional
// category unset. Output is exactly the language base block plus a
// trailing newline; no optional blocks emitted, no spurious whitespace.
func TestRender_LanguageOnly(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	want := "## Python\n\nPython base block.\n"
	if string(got) != want {
		t.Fatalf("Render output mismatch\nwant:\n%q\ngot:\n%q", want, string(got))
	}
}

// TestRender_LanguageWithNestedSinglePick: language + one nested
// single-pick category populated. Output is base + framework.
func TestRender_LanguageWithNestedSinglePick(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.framework"] = registry.NewSingle("fastapi")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	want := "## Python\n\nPython base block.\n\n### FastAPI\n\nFastAPI framework block.\n"
	if string(got) != want {
		t.Fatalf("Render output mismatch\nwant:\n%q\ngot:\n%q", want, string(got))
	}
}

// TestRender_OptionalUnsetCategoriesSkipped: only language is set;
// optional categories (python.logging, infra, ci) are unset. None of
// them appear in the output and no blank lines are emitted for them.
func TestRender_OptionalUnsetCategoriesSkipped(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	// No empty blocks, no docker, no GH Actions, etc.
	for _, marker := range []string{"Docker block.", "GH Actions block.", "FastAPI", "loguru block."} {
		if bytes.Contains(got, []byte(marker)) {
			t.Errorf("unexpected block content %q in output:\n%s", marker, got)
		}
	}
	// No more than one consecutive blank line internally.
	if bytes.Contains(got, []byte("\n\n\n")) {
		t.Errorf("output has triple newline (spurious blank line):\n%q", got)
	}
}

// TestRender_OptionalExplicitlyEmptySkipped: language + python.logging
// set to explicit-none. python.logging block should not appear.
func TestRender_OptionalExplicitlyEmptySkipped(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.logging"] = registry.NewSingle("") // explicit-none

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if bytes.Contains(got, []byte("loguru block.")) || bytes.Contains(got, []byte("stdlib block.")) {
		t.Errorf("logging block appeared despite explicit-none:\n%s", got)
	}
}

// TestRender_MultiPick: two options selected; both emitted in
// manifest order, separated by \n\n.
func TestRender_MultiPick(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["infra"] = registry.NewMulti([]string{"docker", "kubernetes"})

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	// Both blocks present, in manifest order, separated by blank line.
	want := "## Python\n\nPython base block.\n\n## Docker\n\nDocker block.\n\n## Kubernetes\n\nKubernetes block.\n"
	if string(got) != want {
		t.Fatalf("Render output mismatch\nwant:\n%q\ngot:\n%q", want, string(got))
	}
}

// TestRender_MultiPickDeterminism: when the user passes selections in
// reverse manifest order, the output is byte-identical to the
// in-order case. This is the §4.4 determinism rule.
func TestRender_MultiPickDeterminism(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)

	mkPicks := func(order []string) registry.Picks {
		p := registry.NewPicks()
		p.Values["language"] = registry.NewSingle("python")
		p.Values["infra"] = registry.NewMulti(order)
		return p
	}

	inOrder, err := Render(reg, mkPicks([]string{"docker", "kubernetes"}))
	if err != nil {
		t.Fatalf("Render in-order error: %v", err)
	}
	reverseOrder, err := Render(reg, mkPicks([]string{"kubernetes", "docker"}))
	if err != nil {
		t.Fatalf("Render reverse-order error: %v", err)
	}
	if !bytes.Equal(inOrder, reverseOrder) {
		t.Fatalf("multi-pick output not order-stable\nin-order:\n%s\nreverse:\n%s", inOrder, reverseOrder)
	}
	// And the order is manifest order (docker before kubernetes).
	dockerIdx := bytes.Index(inOrder, []byte("Docker block."))
	k8sIdx := bytes.Index(inOrder, []byte("Kubernetes block."))
	if dockerIdx < 0 || k8sIdx < 0 || dockerIdx > k8sIdx {
		t.Errorf("manifest order not preserved: docker=%d kubernetes=%d\n%s", dockerIdx, k8sIdx, inOrder)
	}
}

// TestRender_EveryCategoryPopulated: language + nested + cross-cutting
// all present. The order matches §4.4 exactly.
func TestRender_EveryCategoryPopulated(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	picks.Values["python.framework"] = registry.NewSingle("fastapi")
	picks.Values["python.logging"] = registry.NewSingle("loguru")
	picks.Values["infra"] = registry.NewMulti([]string{"docker", "kubernetes"})
	picks.Values["ci"] = registry.NewMulti([]string{"github-actions"})

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	// Expected order: python base, framework, logging, docker, k8s, gh-actions.
	expectedOrder := []string{
		"Python base block.",
		"FastAPI framework block.",
		"loguru block.",
		"Docker block.",
		"Kubernetes block.",
		"GH Actions block.",
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
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output does not end with newline: %q", got[len(got)-min(10, len(got)):])
	}
	if bytes.Contains(got, []byte("\n\n\n")) {
		t.Errorf("triple newline found in output:\n%s", got)
	}
}

// TestRender_TrailingNewline: output ends with exactly one '\n' and
// does not double-up trailing newlines from block files.
func TestRender_TrailingNewline(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output missing trailing newline: %q", got)
	}
	if bytes.HasSuffix(got, []byte("\n\n")) {
		t.Errorf("output has duplicated trailing newline: %q", got)
	}
}

// TestRender_NoBlocks: with no language picked, Render returns empty
// output (no leading whitespace, no error). This shouldn't happen in
// production (CLI rejects required-empty) but the renderer is pure and
// should handle it cleanly.
func TestRender_NoBlocks(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty output, got %q", got)
	}
}

// TestRender_UnknownOptionIsError: if Picks references an option id
// that isn't in the manifest, Render returns a clear error rather
// than silently dropping the block. (This shouldn't reach Render in
// production — ResolveSelection rejects unknown ids — but the
// renderer surfacing the invariant beats hiding it.)
func TestRender_UnknownOptionIsError(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)
	picks := registry.NewPicks()
	picks.Values["language"] = registry.NewSingle("python")
	// Bypass ResolveSelection by stuffing an unknown id straight in.
	picks.Values["infra"] = registry.NewMulti([]string{"ghost-cloud"})

	_, err := Render(reg, picks)
	if err == nil {
		t.Fatal("expected error for unknown option id, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-cloud") {
		t.Errorf("error should mention offending option id; got: %v", err)
	}
}
