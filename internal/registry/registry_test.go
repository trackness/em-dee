package registry

import (
	"os"
	"reflect"
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

// validFixture loads `testdata/valid/templates/` via os.DirFS. This is
// the canonical happy-path fixture used by parse tests in Task 1.3
// and as the clean-baseline row in the hygiene table in Task 1.4.
func validFixture(t *testing.T) *Registry {
	t.Helper()
	fsys := os.DirFS("testdata/valid")
	reg, err := load(fsys, "templates")
	if err != nil {
		t.Fatalf("load(valid fixture) returned error: %v", err)
	}
	return reg
}

// TestLoad_ValidFixture_TopLevelStructure asserts the load surfaces
// the four top-level categories from the fixture in render order.
func TestLoad_ValidFixture_TopLevelStructure(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)

	if len(reg.Categories) != 3 {
		t.Fatalf("expected 3 top-level categories, got %d", len(reg.Categories))
	}

	wantIDs := []string{"language", "infra", "ci"}
	for i, want := range wantIDs {
		if got := reg.Categories[i].ID; got != want {
			t.Errorf("Categories[%d].ID = %q, want %q", i, got, want)
		}
	}
}

// TestLoad_ValidFixture_LanguageCategory asserts the language
// category's required+single+no-default shape is parsed correctly,
// and that options come back in declaration order.
func TestLoad_ValidFixture_LanguageCategory(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	lang := reg.Categories[0]

	if lang.DisplayName != "Language" {
		t.Errorf("DisplayName = %q, want %q", lang.DisplayName, "Language")
	}
	if lang.Pick != PickSingle {
		t.Errorf("Pick = %q, want %q", lang.Pick, PickSingle)
	}
	if !lang.Required {
		t.Error("expected Required=true for language category")
	}
	if lang.DefaultSingle != "" {
		t.Errorf("DefaultSingle = %q, want empty (language forbids default)", lang.DefaultSingle)
	}
	if got, want := len(lang.Options), 2; got != want {
		t.Fatalf("len(Options) = %d, want %d", got, want)
	}
	if lang.Options[0].ID != "python" || lang.Options[1].ID != "go" {
		t.Errorf("option order = [%s, %s], want [python, go]",
			lang.Options[0].ID, lang.Options[1].ID)
	}
}

// TestLoad_ValidFixture_SinglePickWithDefault verifies the python
// language nests a 10-framework single-pick with `default: fastapi`.
func TestLoad_ValidFixture_SinglePickWithDefault(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	lang := reg.Categories[0]

	subs := lang.Subcategories["python"]
	if len(subs) == 0 {
		t.Fatal("expected python subcategories, got none")
	}

	var framework *Category
	for _, c := range subs {
		if c.ID == "framework" {
			framework = c
			break
		}
	}
	if framework == nil {
		t.Fatalf("no 10-framework subcategory under python; got %d subs", len(subs))
	}
	if framework.Pick != PickSingle {
		t.Errorf("Pick = %q, want %q", framework.Pick, PickSingle)
	}
	if framework.DefaultSingle != "fastapi" {
		t.Errorf("DefaultSingle = %q, want %q", framework.DefaultSingle, "fastapi")
	}
	if len(framework.DefaultMulti) != 0 {
		t.Errorf("DefaultMulti = %v, want empty for single-pick", framework.DefaultMulti)
	}
}

// TestLoad_ValidFixture_SinglePickNoDefault verifies 20-logging under
// python parses without a default.
func TestLoad_ValidFixture_SinglePickNoDefault(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	lang := reg.Categories[0]

	var logging *Category
	for _, c := range lang.Subcategories["python"] {
		if c.ID == "logging" {
			logging = c
			break
		}
	}
	if logging == nil {
		t.Fatal("no 20-logging subcategory under python")
	}
	if logging.DefaultSingle != "" {
		t.Errorf("DefaultSingle = %q, want empty", logging.DefaultSingle)
	}
}

// TestLoad_ValidFixture_MultiPickWithDefault verifies 20-infra parses
// as multi-pick with a list default.
func TestLoad_ValidFixture_MultiPickWithDefault(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	infra := reg.Categories[1]

	if infra.Pick != PickMulti {
		t.Errorf("Pick = %q, want %q", infra.Pick, PickMulti)
	}
	if !reflect.DeepEqual(infra.DefaultMulti, []string{"docker"}) {
		t.Errorf("DefaultMulti = %v, want [docker]", infra.DefaultMulti)
	}
	if infra.DefaultSingle != "" {
		t.Errorf("DefaultSingle = %q, want empty for multi-pick", infra.DefaultSingle)
	}
}

// TestLoad_ValidFixture_MultiPickNoDefault verifies 30-ci parses as
// multi-pick with no default.
func TestLoad_ValidFixture_MultiPickNoDefault(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	ci := reg.Categories[2]

	if ci.Pick != PickMulti {
		t.Errorf("Pick = %q, want %q", ci.Pick, PickMulti)
	}
	if len(ci.DefaultMulti) != 0 {
		t.Errorf("DefaultMulti = %v, want empty", ci.DefaultMulti)
	}
}
