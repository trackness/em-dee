package render

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/trackness/em-dee/internal/registry"
)

// goldenRoot points at the repo-root `testdata/golden/` directory.
// Golden fixtures live at the repo root rather than inside
// `internal/render/testdata/` so they're visible from anywhere in the
// tree (CLI integration tests in Phase 3 will share them) and so the
// regen path (`task golden-update`) doesn't have to know which
// package's testdata to write into.
const goldenRoot = "../../testdata/golden"

// TestGolden walks every `<scenario>/` directory under goldenRoot,
// resolves `selection.yaml` via the same `registry.ResolveSelection`
// path the CLI uses, applies defaults, renders, and asserts byte-
// equality against `expected.md`. When `GOLDEN_UPDATE=1` is set, the
// assertion is replaced with a write so `task golden-update` can
// regenerate fixtures in-place.
//
// Anti-drift guarantee: the only resolution path is
// `registry.ResolveSelection`. If the CLI ever drifts to a different
// resolution shape, this test starts failing before the drift can
// land — golden fixtures and CLI inputs share one code path by
// construction.
func TestGolden(t *testing.T) {
	t.Parallel()

	reg := fixtureRegistry(t)

	entries, err := os.ReadDir(goldenRoot)
	if err != nil {
		t.Fatalf("read %s: %v", goldenRoot, err)
	}

	update := os.Getenv("GOLDEN_UPDATE") == "1"

	scenarios := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		scenarios++
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runGoldenScenario(t, reg, filepath.Join(goldenRoot, name), update)
		})
	}

	if scenarios == 0 {
		t.Fatalf("no golden scenarios found under %s; Phase 2 requires at least one fixture", goldenRoot)
	}
}

// runGoldenScenario loads `<dir>/selection.yaml`, runs ResolveSelection
// + ApplyDefaults + Render, then compares against `<dir>/expected.md`
// (or writes it, under GOLDEN_UPDATE=1).
func runGoldenScenario(t *testing.T, reg *registry.Registry, dir string, update bool) {
	t.Helper()

	selectionPath := filepath.Join(dir, "selection.yaml")
	expectedPath := filepath.Join(dir, "expected.md")

	rawSel, err := os.ReadFile(selectionPath)
	if err != nil {
		t.Fatalf("read %s: %v", selectionPath, err)
	}

	// The selection.yaml dotted-key shape is the same map ResolveSelection
	// expects. yaml.v3 decodes scalars to string and sequences to
	// []interface{}; coerce sequences to []string so ResolveSelection's
	// `[]string` branch fires (rather than its fallback "expected string
	// or list" error).
	var raw map[string]any
	if err := yaml.Unmarshal(rawSel, &raw); err != nil {
		t.Fatalf("parse %s: %v", selectionPath, err)
	}
	m := coerceSelection(raw)

	picks, err := registry.ResolveSelection(reg, m)
	if err != nil {
		t.Fatalf("ResolveSelection(%s): %v", selectionPath, err)
	}
	picks = registry.ApplyDefaults(picks, reg)

	got, err := Render(reg, picks)
	if err != nil {
		t.Fatalf("Render(%s): %v", dir, err)
	}

	if update {
		if err := os.WriteFile(expectedPath, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", expectedPath, err)
		}
		t.Logf("regenerated %s", expectedPath)
		return
	}

	want, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read %s (regenerate with GOLDEN_UPDATE=1): %v", expectedPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch.\nwant:\n%s\ngot:\n%s", expectedPath, want, got)
	}
}

// coerceSelection normalises a yaml-decoded `map[string]any` so that
// list values come back as `[]string` (yaml.v3 decodes them as
// `[]interface{}`). Scalar values stay as-is — ResolveSelection
// already accepts both `string` and `[]string`.
func coerceSelection(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch t := v.(type) {
		case []any:
			ss := make([]string, 0, len(t))
			for _, x := range t {
				if s, ok := x.(string); ok {
					ss = append(ss, s)
				}
			}
			out[k] = ss
		default:
			out[k] = v
		}
	}
	return out
}
