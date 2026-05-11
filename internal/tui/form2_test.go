package tui

import (
	"reflect"
	"sort"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// TestBuildSecondaryForm_PythonConstructs asserts the builder returns
// a complete form against the testdata/valid fixture for `python`. The
// fixture has python with 10-framework and 20-logging sub-categories,
// plus cross-cutting 20-infra (multi) and 30-ci (multi).
func TestBuildSecondaryForm_PythonConstructs(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildSecondaryForm(reg, "python", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildSecondaryForm: %v", err)
	}
	if sf.Form == nil {
		t.Fatal("Form is nil")
	}

	// The bound storage maps tell us which fields exist. We don't
	// inspect huh's internal group list (it's unexported); the
	// presence of bound vars is the testable proxy.
	wantSingleKeys := []string{"python.framework", "python.logging"}
	for _, k := range wantSingleKeys {
		if _, ok := sf.singles[k]; !ok {
			t.Errorf("missing single-pick binding for %q", k)
		}
	}
	wantMultiKeys := []string{"infra", "ci"}
	for _, k := range wantMultiKeys {
		if _, ok := sf.multis[k]; !ok {
			t.Errorf("missing multi-pick binding for %q", k)
		}
	}
}

// TestBuildSecondaryForm_GoConstructs asserts the same builder works
// for the `go` language fixture (which has no sub-categories under
// it). The cross-cutting groups still appear; the chosen-language
// sub-tree contributes zero groups.
func TestBuildSecondaryForm_GoConstructs(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildSecondaryForm(reg, "go", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildSecondaryForm: %v", err)
	}
	if sf.Form == nil {
		t.Fatal("Form is nil")
	}

	// No language-nested sub-categories under `go` in the fixture.
	for k := range sf.singles {
		t.Errorf("unexpected single-pick binding for go: %q", k)
	}
	wantMultiKeys := []string{"infra", "ci"}
	gotKeys := make([]string, 0, len(sf.multis))
	for k := range sf.multis {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantMultiKeys)
	if !reflect.DeepEqual(gotKeys, wantMultiKeys) {
		t.Errorf("multi keys = %v, want %v", gotKeys, wantMultiKeys)
	}
}

// TestBuildSecondaryForm_SeedsDefaults asserts the bound variables
// are pre-populated from `initial` per spec §5.2 paragraph 2. We
// construct an `initial` Picks with chosen values, build, and read
// the bindings.
func TestBuildSecondaryForm_SeedsDefaults(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)

	initial := registry.NewPicks()
	initial.Values["python.framework"] = registry.NewSingle("fastapi")
	initial.Values["infra"] = registry.NewMulti([]string{"docker"})

	sf, err := BuildSecondaryForm(reg, "python", initial)
	if err != nil {
		t.Fatalf("BuildSecondaryForm: %v", err)
	}

	if got := *sf.singles["python.framework"]; got != "fastapi" {
		t.Errorf("python.framework bound = %q, want %q", got, "fastapi")
	}
	if got := *sf.multis["infra"]; !reflect.DeepEqual(got, []string{"docker"}) {
		t.Errorf("infra bound = %v, want [docker]", got)
	}
	// Unseeded single-pick categories: huh v2's Select.Value pre-fills
	// the bound pointer with the first option's value at construction
	// time (via Accessor → updateValue), so the bound is not "" but
	// rather the first option's id. This is huh v2 behaviour, not
	// ours, and it doesn't violate spec §5.2 paragraph 1: the *UX*
	// still requires Enter to commit. We document this here so a
	// future maintainer doesn't try to "fix" it.
	if got := *sf.singles["python.logging"]; got != "stdlib" {
		t.Errorf("python.logging bound = %q, want %q (huh v2 pre-fills first option)", got, "stdlib")
	}
}

// TestSecondaryForm_PicksTranslation asserts the Picks() helper turns
// bound state back into a registry.Picks with `language` set and each
// chosen category present in the right shape.
func TestSecondaryForm_PicksTranslation(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildSecondaryForm(reg, "python", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildSecondaryForm: %v", err)
	}

	// Simulate what huh would do after the user picks something.
	*sf.singles["python.framework"] = "django"
	*sf.multis["infra"] = []string{"docker", "kubernetes"}

	picks := sf.Picks()
	if got := picks.Values["language"]; got == nil || got.Single == nil || *got.Single != "python" {
		t.Errorf("language pick = %+v, want python", got)
	}
	if got := picks.Values["python.framework"]; got == nil || got.Single == nil || *got.Single != "django" {
		t.Errorf("python.framework pick = %+v, want django", got)
	}
	if got := picks.Values["infra"]; got == nil || got.Multi == nil ||
		!reflect.DeepEqual(*got.Multi, []string{"docker", "kubernetes"}) {
		t.Errorf("infra pick = %+v, want [docker kubernetes]", got)
	}

	// python.logging was never seeded and the user didn't touch the
	// binding here; huh v2's Select.Value pre-fills the bound pointer
	// with the first option's id at construction time. Picks() passes
	// it through. This is the documented v2 behaviour and the user
	// still sees the form before commit.
	if got := picks.Values["python.logging"]; got == nil || got.Single == nil || *got.Single != "stdlib" {
		t.Errorf("python.logging pick = %+v, want stdlib (huh v2 pre-fill)", got)
	}
}

// TestBuildSecondaryForm_Errors asserts the obvious failure modes
// surface a clean error rather than panicking.
func TestBuildSecondaryForm_Errors(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	if _, err := BuildSecondaryForm(nil, "python", registry.NewPicks()); err == nil {
		t.Error("expected error for nil registry, got nil")
	}
	if _, err := BuildSecondaryForm(reg, "", registry.NewPicks()); err == nil {
		t.Error("expected error for empty langID, got nil")
	}
	if _, err := BuildSecondaryForm(&registry.Registry{}, "python", registry.NewPicks()); err == nil {
		t.Error("expected error for empty registry, got nil")
	}
}

// TestSummarise_RenderOrder asserts the confirm-group summary lists
// blocks in render order (§4.4): language base, language subs, then
// cross-cutting categories. This is what the user sees on the
// confirm screen, so it needs to match the actual write order.
func TestSummarise_RenderOrder(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	initial := registry.NewPicks()
	initial.Values["python.framework"] = registry.NewSingle("fastapi")
	initial.Values["python.logging"] = registry.NewSingle("loguru")
	initial.Values["infra"] = registry.NewMulti([]string{"docker", "kubernetes"})
	initial.Values["ci"] = registry.NewMulti([]string{"github-actions"})

	sf, err := BuildSecondaryForm(reg, "python", initial)
	if err != nil {
		t.Fatalf("BuildSecondaryForm: %v", err)
	}

	got := sf.summarise(reg)
	// Render order: language/python, framework/fastapi, logging/loguru,
	// infra/docker, infra/kubernetes, ci/github-actions.
	want := "blocks: language/python, framework/fastapi, logging/loguru, infra/docker, infra/kubernetes, ci/github-actions"
	if got != want {
		t.Errorf("summary mismatch:\n got: %s\nwant: %s", got, want)
	}
}
