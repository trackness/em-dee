package registry

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// threeLevelFixture loads the canonical three-level fixture used by
// every test in this file. Kept as a helper so the load path is
// covered exactly once and each test can focus on its assertion.
//
// Fixture shape:
//
//	templates/
//	  10-language/                  (top-level container)
//	    _index.yaml
//	    python/
//	      base.md                   (required: language-scope base)
//	      10-type/                  (nested container)
//	        _index.yaml
//	        cli/
//	          base.md               (optional: type-scope base)
//	          10-framework/         (leaf)
//	            _index.yaml
//	            typer.md
//	            click.md
//	          20-consumer/          (leaf)
//	            _index.yaml
//	            human.md
//	            agent.md
//	        library/
//	          base.md               (optional: type-scope base, no further sub-cats)
//	      30-logging/               (leaf, language-universal)
//	        _index.yaml
//	        stdlib.md
//	        loguru.md
//	  20-infra/                     (top-level leaf)
//	    _index.yaml
//	    docker.md
func threeLevelFixture(t *testing.T) *Registry {
	t.Helper()
	fsys := os.DirFS("testdata/three-level")
	reg, err := load(fsys, "templates")
	if err != nil {
		t.Fatalf("load(three-level fixture) returned error: %v", err)
	}
	return reg
}

// TestThreeLevel_Loads asserts the canonical three-level fixture
// parses and validates cleanly — the engine accepts arbitrary depth
// without surfacing a hygiene error.
func TestThreeLevel_Loads(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	if len(reg.Categories) != 2 {
		t.Fatalf("expected 2 top-level categories, got %d", len(reg.Categories))
	}
	if reg.Categories[0].ID != "language" {
		t.Errorf("Categories[0].ID = %q, want language", reg.Categories[0].ID)
	}
}

// TestThreeLevel_LanguageIsContainer asserts the language category is
// marked as a container at the top level.
func TestThreeLevel_LanguageIsContainer(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	lang := reg.Categories[0]
	if !lang.IsContainer {
		t.Error("language category should be marked IsContainer")
	}
	if len(lang.Subcategories["python"]) == 0 {
		t.Fatal("python subtree should have categories")
	}
}

// TestThreeLevel_TypeIsContainer asserts the nested `10-type/` folder
// under python parses as a container category whose options point at
// sub-trees.
func TestThreeLevel_TypeIsContainer(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	lang := reg.Categories[0]

	var typeCat *Category
	for _, sub := range lang.Subcategories["python"] {
		if sub.ID == "type" {
			typeCat = sub
			break
		}
	}
	if typeCat == nil {
		t.Fatal("python subtree should contain a `type` category")
	}
	if !typeCat.IsContainer {
		t.Error("python.type should be marked IsContainer")
	}
	if typeCat.Pick != PickSingle {
		t.Errorf("python.type Pick = %q, want single (containers must be single-pick)", typeCat.Pick)
	}
	wantOpts := []string{"cli", "library"}
	gotOpts := make([]string, 0, len(typeCat.Options))
	for _, o := range typeCat.Options {
		gotOpts = append(gotOpts, o.ID)
	}
	if !reflect.DeepEqual(gotOpts, wantOpts) {
		t.Errorf("python.type options = %v, want %v", gotOpts, wantOpts)
	}
}

// TestThreeLevel_CLISubtree exercises the third level: walk through
// container python → container type → option cli → leaf framework /
// leaf consumer. Both leaf sub-categories must surface with their
// option lists intact.
func TestThreeLevel_CLISubtree(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	lang := reg.Categories[0]

	var typeCat *Category
	for _, sub := range lang.Subcategories["python"] {
		if sub.ID == "type" {
			typeCat = sub
		}
	}
	if typeCat == nil {
		t.Fatal("missing python.type")
	}
	cliSubs := typeCat.Subcategories["cli"]
	if len(cliSubs) != 2 {
		t.Fatalf("python.type.cli should have 2 sub-categories, got %d", len(cliSubs))
	}
	subIDs := []string{cliSubs[0].ID, cliSubs[1].ID}
	want := []string{"framework", "consumer"}
	if !reflect.DeepEqual(subIDs, want) {
		t.Errorf("cli sub-cat order = %v, want %v (folder-prefix order)", subIDs, want)
	}
	for _, sub := range cliSubs {
		if sub.IsContainer {
			t.Errorf("cli sub-category %q should be a leaf, got container", sub.ID)
		}
	}
}

// TestThreeLevel_LibraryHasEmptySubtree pins library's shape: it's an
// option of a container that points at `library/base.md` (so the type
// has a base block) but its subtree carries no further categories.
func TestThreeLevel_LibraryHasEmptySubtree(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	lang := reg.Categories[0]

	var typeCat *Category
	for _, sub := range lang.Subcategories["python"] {
		if sub.ID == "type" {
			typeCat = sub
		}
	}
	if typeCat == nil {
		t.Fatal("missing python.type")
	}
	if subs := typeCat.Subcategories["library"]; len(subs) != 0 {
		t.Errorf("library subtree should be empty, got %d categories", len(subs))
	}
}

// validThreeLevelMapFS builds a fully-valid three-level template tree
// in an in-memory MapFS. Each rule test mutates a copy to introduce
// one violation and asserts the error mentions the rule.
func validThreeLevelMapFS() fstest.MapFS {
	return fstest.MapFS{
		"templates/10-language/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)},
		"templates/10-language/python/base.md": &fstest.MapFile{Data: []byte("python base\n")},
		"templates/10-language/python/10-type/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: single
required: false
default: cli
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: cli/base.md
  - id: library
    display_name: "Library"
    description: "Library"
    file: library/base.md
`)},
		"templates/10-language/python/10-type/library/base.md": &fstest.MapFile{Data: []byte("library base\n")},
		"templates/10-language/python/10-type/cli/base.md":     &fstest.MapFile{Data: []byte("cli base\n")},
		"templates/10-language/python/10-type/cli/10-framework/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Framework"
pick: single
required: false
default: typer
options:
  - id: typer
    display_name: "Typer"
    description: "Typer"
    file: typer.md
`)},
		"templates/10-language/python/10-type/cli/10-framework/typer.md": &fstest.MapFile{Data: []byte("typer\n")},
	}
}

// TestThreeLevel_CleanMapFS asserts the in-memory three-level fixture
// loads cleanly. Used as the baseline for the rule-mutation table
// below.
func TestThreeLevel_CleanMapFS(t *testing.T) {
	t.Parallel()
	fsys := validThreeLevelMapFS()
	if _, err := load(fsys, "templates"); err != nil {
		t.Fatalf("clean three-level baseline failed: %v", err)
	}
}

// TestThreeLevel_ValidatorRules drives each new validator rule via a
// table of mutations against the clean three-level baseline.
func TestThreeLevel_ValidatorRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(fstest.MapFS)
		wantSubs []string
	}{
		{
			// CONTENT-STYLE.md §2.4: a category whose options mix
			// leaf-shape and container-shape `file:` values is
			// rejected.
			name: "container category with mixed-shape options",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: single
required: false
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: cli/base.md
  - id: standalone
    display_name: "Standalone"
    description: "Bare-file option mixed into a container shape"
    file: standalone.md
`)}
				m["templates/10-language/python/10-type/standalone.md"] = &fstest.MapFile{Data: []byte("standalone\n")}
			},
			wantSubs: []string{"mixed-shape"},
		},
		{
			// CONTENT-STYLE.md §2.7: `base.md` is not licensed at a
			// leaf-category position. The orphan-style rejection
			// stays mechanical (no special-casing of base.md by
			// presence elsewhere — leaf folders simply must not
			// contain `base.md`).
			name: "leaf category contains a base.md",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/cli/10-framework/base.md"] = &fstest.MapFile{Data: []byte("disallowed\n")}
			},
			wantSubs: []string{"base.md", "leaf"},
		},
		{
			// Container shape: option `file:` must be
			// `<opt.ID>/base.md` or `<opt.ID>/`.
			name: "container option file does not match opt.ID/...",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: single
required: false
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: program/base.md
`)}
				m["templates/10-language/python/10-type/program/base.md"] = &fstest.MapFile{Data: []byte("program\n")}
			},
			wantSubs: []string{"container option", "cli", "file"},
		},
		{
			// Container option declares no scope-base (file: cli/)
			// but a base.md still exists in the subfolder.
			name: "container option declares no base.md but base.md is present",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: single
required: false
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: cli/
`)}
				// cli/base.md is still on disk from the baseline; the
				// mismatch is the violation.
			},
			wantSubs: []string{"cli", "no scope-base"},
		},
		{
			// Container pick must be `single`. A container with
			// pick: multi is rejected.
			name: "container category with pick: multi",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Program type"
pick: multi
required: false
options:
  - id: cli
    display_name: "CLI"
    description: "CLI"
    file: cli/base.md
`)}
			},
			wantSubs: []string{"container", "single"},
		},
		{
			// The depth-walk rule: a hygiene violation deep in the
			// tree (here: an empty display_name in a third-level
			// leaf) must still be surfaced by the recursive
			// validator.
			name: "third-level leaf empty display_name surfaced",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-type/cli/10-framework/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: ""
pick: single
required: false
default: typer
options:
  - id: typer
    display_name: "Typer"
    description: "Typer"
    file: typer.md
`)}
			},
			wantSubs: []string{"display_name"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := validThreeLevelMapFS()
			tc.mutate(fsys)
			_, err := load(fsys, "templates")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			msg := err.Error()
			for _, want := range tc.wantSubs {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing substring %q", msg, want)
				}
			}
		})
	}
}

// TestThreeLevel_KeyIndexCoversDeepCategories asserts the resolver's
// key index registers categories at every depth so ResolveSelection
// accepts dotted keys like `python.cli.framework`.
func TestThreeLevel_KeyIndexCoversDeepCategories(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	index := buildKeyIndex(reg)

	// Sample coverage: top-level leaf, top-level container, language-
	// scope leaf, language-scope container, third-level leaves under
	// the chosen container option.
	for _, key := range []string{
		"infra",
		"language",
		"python.logging",
		"python.type",
		"python.cli.framework",
		"python.cli.consumer",
	} {
		if _, ok := index[key]; !ok {
			t.Errorf("buildKeyIndex missing key %q", key)
		}
	}
}

// TestThreeLevel_ResolveDeepSelection asserts ResolveSelection accepts
// the deep dotted keys, validates the option ids at the leaf, and
// produces a Picks the renderer can consume.
func TestThreeLevel_ResolveDeepSelection(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	picks, err := ResolveSelection(reg, map[string]any{
		"language":             "python",
		"python.type":          "cli",
		"python.cli.framework": "click",
		"python.cli.consumer":  "agent",
	})
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if v := picks.Values["python.cli.framework"]; v == nil || v.Single == nil || *v.Single != "click" {
		t.Errorf("python.cli.framework = %+v, want click", v)
	}
	if v := picks.Values["python.cli.consumer"]; v == nil || v.Single == nil || *v.Single != "agent" {
		t.Errorf("python.cli.consumer = %+v, want agent", v)
	}
}

// TestThreeLevel_ApplyDefaultsDescendsContainer asserts ApplyDefaults
// fills nested defaults: with python and the container's default
// type=cli applied, the third-level leaf's default (typer) gets
// filled too.
func TestThreeLevel_ApplyDefaultsDescendsContainer(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")

	out := ApplyDefaults(picks, reg)

	if v := out.Values["python.type"]; v == nil || v.Single == nil || *v.Single != "cli" {
		t.Errorf("python.type default not applied: %+v", v)
	}
	if v := out.Values["python.cli.framework"]; v == nil || v.Single == nil || *v.Single != "typer" {
		t.Errorf("python.cli.framework default not applied: %+v (expected typer via nested-container default chain)", v)
	}
}
