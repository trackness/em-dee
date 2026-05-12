package tui

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// threeLevelFixture loads the three-level fixture used by the schema's
// three-level capability tests. Kept distinct from validFixture (the
// two-level fixture used for everything that doesn't need a container)
// so tests can pin which schema shape they exercise.
func threeLevelFixture(t *testing.T) *registry.Registry {
	t.Helper()
	fsys := os.DirFS("../registry/testdata/three-level")
	reg, err := registry.LoadFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return reg
}

// TestFindContainerSub asserts the detection helper:
//   - returns nil for a language with no container sub-category (the
//     two-level `valid` fixture's python and go);
//   - returns the container for a language with one (the three-level
//     fixture's python).
func TestFindContainerSub(t *testing.T) {
	t.Parallel()

	t.Run("two-level python — no container", func(t *testing.T) {
		t.Parallel()
		reg := validFixture(t)
		if c := FindContainerSub(reg, "python"); c != nil {
			t.Errorf("expected nil for two-level python, got %q", c.ID)
		}
	})

	t.Run("two-level go — no container", func(t *testing.T) {
		t.Parallel()
		reg := validFixture(t)
		if c := FindContainerSub(reg, "go"); c != nil {
			t.Errorf("expected nil for two-level go, got %q", c.ID)
		}
	})

	t.Run("three-level python — container `type`", func(t *testing.T) {
		t.Parallel()
		reg := threeLevelFixture(t)
		c := FindContainerSub(reg, "python")
		if c == nil {
			t.Fatal("expected non-nil container for three-level python")
		}
		if c.ID != "type" {
			t.Errorf("container.ID = %q, want type", c.ID)
		}
		if !c.IsContainer {
			t.Error("returned category is not flagged IsContainer")
		}
	})

	t.Run("unknown language — nil", func(t *testing.T) {
		t.Parallel()
		reg := threeLevelFixture(t)
		if c := FindContainerSub(reg, "kotlin"); c != nil {
			t.Errorf("expected nil for unknown language, got %q", c.ID)
		}
	})

	t.Run("nil registry — nil", func(t *testing.T) {
		t.Parallel()
		if c := FindContainerSub(nil, "python"); c != nil {
			t.Errorf("expected nil for nil registry, got %q", c.ID)
		}
	})
}

// TestBuildTypeForm_NoContainer asserts the builder returns (nil, nil)
// when the language has no container sub-category. Callers branch on
// nil to skip phase 2; an empty form would be a worse UX.
func TestBuildTypeForm_NoContainer(t *testing.T) {
	t.Parallel()
	reg := validFixture(t)
	tf, err := BuildTypeForm(reg, "python", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildTypeForm: %v", err)
	}
	if tf != nil {
		t.Errorf("expected nil form for two-level python (no container), got %+v", tf)
	}
}

// TestBuildTypeForm_WithContainer asserts the builder constructs the
// container-option Select for a language with a container.
func TestBuildTypeForm_WithContainer(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	tf, err := BuildTypeForm(reg, "python", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildTypeForm: %v", err)
	}
	if tf == nil {
		t.Fatal("expected non-nil form for three-level python")
	}
	if tf.Form == nil {
		t.Fatal("TypeForm.Form is nil")
	}
	if tf.ContainerID() != "type" {
		t.Errorf("ContainerID = %q, want type", tf.ContainerID())
	}
	// huh v2 Select.Value pre-fill quirk: the bound *string starts as
	// the first option's id even without seeding. cli is first.
	if got := tf.Chosen(); got != "cli" {
		t.Errorf("Chosen() = %q, want cli (huh v2 pre-fills first option)", got)
	}
}

// TestBuildTypeForm_SeedsInitial asserts a non-empty initial Picks
// pre-populates the bound pointer so the form starts on the seeded
// value rather than the first option.
func TestBuildTypeForm_SeedsInitial(t *testing.T) {
	t.Parallel()
	reg := threeLevelFixture(t)
	initial := registry.NewPicks()
	initial.Values["python.type"] = registry.NewSingle("library")

	tf, err := BuildTypeForm(reg, "python", initial)
	if err != nil {
		t.Fatalf("BuildTypeForm: %v", err)
	}
	if tf == nil {
		t.Fatal("expected non-nil form")
	}
	if got := tf.Chosen(); got != "library" {
		t.Errorf("Chosen() = %q, want library (seeded)", got)
	}
}

// TestBuildTypeForm_Errors asserts the obvious failure modes surface a
// clean error rather than panicking.
func TestBuildTypeForm_Errors(t *testing.T) {
	t.Parallel()

	if _, err := BuildTypeForm(nil, "python", registry.NewPicks()); err == nil {
		t.Error("expected error for nil registry")
	}
	reg := threeLevelFixture(t)
	if _, err := BuildTypeForm(reg, "", registry.NewPicks()); err == nil {
		t.Error("expected error for empty langID")
	}
}

// TestBuildScopeForm_TwoLevelPython covers the legacy two-level case:
// `valid` fixture's python has 10-framework, 20-logging leaves; no
// container; cross-cutting infra (multi) + ci (multi). Group keys must
// be `python.framework`, `python.logging`, `infra`, `ci`.
func TestBuildScopeForm_TwoLevelPython(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildScopeForm(reg, "python", "", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}
	if sf.Form == nil {
		t.Fatal("Form is nil")
	}

	wantSingles := []string{"python.framework", "python.logging"}
	for _, k := range wantSingles {
		if _, ok := sf.singles[k]; !ok {
			t.Errorf("missing single-pick binding for %q", k)
		}
	}
	wantMultis := []string{"infra", "ci"}
	for _, k := range wantMultis {
		if _, ok := sf.multis[k]; !ok {
			t.Errorf("missing multi-pick binding for %q", k)
		}
	}
}

// TestBuildScopeForm_TwoLevelGo asserts the two-level go fixture (no
// language sub-categories) still produces a form with only the
// cross-cutting groups bound.
func TestBuildScopeForm_TwoLevelGo(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildScopeForm(reg, "go", "", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}
	if sf.Form == nil {
		t.Fatal("Form is nil")
	}

	for k := range sf.singles {
		t.Errorf("unexpected single-pick binding for go: %q", k)
	}
	wantKeys := []string{"infra", "ci"}
	gotKeys := make([]string, 0, len(sf.multis))
	for k := range sf.multis {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("multi keys = %v, want %v", gotKeys, wantKeys)
	}
}

// TestBuildScopeForm_ThreeLevelCLI covers the three-level fixture with
// typeID=cli: bindings must include the cli container's leaves keyed
// `python.cli.framework` and `python.cli.consumer`, plus the language-
// universal `python.logging`, plus cross-cutting `infra`.
func TestBuildScopeForm_ThreeLevelCLI(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	sf, err := BuildScopeForm(reg, "python", "cli", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

	wantSingles := []string{"python.cli.framework", "python.cli.consumer", "python.logging"}
	gotSingles := make([]string, 0, len(sf.singles))
	for k := range sf.singles {
		gotSingles = append(gotSingles, k)
	}
	sort.Strings(gotSingles)
	sort.Strings(wantSingles)
	if !reflect.DeepEqual(gotSingles, wantSingles) {
		t.Errorf("single keys = %v, want %v", gotSingles, wantSingles)
	}

	wantMultis := []string{"infra"}
	gotMultis := make([]string, 0, len(sf.multis))
	for k := range sf.multis {
		gotMultis = append(gotMultis, k)
	}
	if !reflect.DeepEqual(gotMultis, wantMultis) {
		t.Errorf("multi keys = %v, want %v", gotMultis, wantMultis)
	}

	if sf.containerID != "type" {
		t.Errorf("containerID = %q, want type", sf.containerID)
	}
	if sf.typeID != "cli" {
		t.Errorf("typeID = %q, want cli", sf.typeID)
	}
}

// TestBuildScopeForm_ThreeLevelLibrary asserts the library type option
// (whose subtree is empty per the fixture) yields no container-option-
// scope leaves; only language-universal and cross-cutting leaves are
// bound.
func TestBuildScopeForm_ThreeLevelLibrary(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	sf, err := BuildScopeForm(reg, "python", "library", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}
	wantSingles := []string{"python.logging"}
	gotSingles := make([]string, 0, len(sf.singles))
	for k := range sf.singles {
		gotSingles = append(gotSingles, k)
	}
	if !reflect.DeepEqual(gotSingles, wantSingles) {
		t.Errorf("single keys = %v, want %v", gotSingles, wantSingles)
	}
}

// TestBuildScopeForm_ThreeLevelEmptyType asserts that passing typeID=""
// against a three-level registry produces a form with only the
// language-universal leaves and cross-cutting top-level leaves (no
// container-option-scope leaves). The containerID is still recorded so
// Picks() knows the container category exists in the registry but no
// option is set.
func TestBuildScopeForm_ThreeLevelEmptyType(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	sf, err := BuildScopeForm(reg, "python", "", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}
	wantSingles := []string{"python.logging"}
	gotSingles := make([]string, 0, len(sf.singles))
	for k := range sf.singles {
		gotSingles = append(gotSingles, k)
	}
	if !reflect.DeepEqual(gotSingles, wantSingles) {
		t.Errorf("single keys = %v, want %v", gotSingles, wantSingles)
	}
	// Container detection still happens; typeID stays empty.
	if sf.containerID != "type" {
		t.Errorf("containerID = %q, want type (detected even without pick)", sf.containerID)
	}
	if sf.typeID != "" {
		t.Errorf("typeID = %q, want empty", sf.typeID)
	}
}

// TestBuildScopeForm_SeedsDefaults asserts the bound variables are
// pre-populated from `initial` — the contract that lets the caller
// pass an ApplyDefaults-defaulted Picks so single-pick fields land on
// the registry default rather than the first option.
func TestBuildScopeForm_SeedsDefaults(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)

	initial := registry.NewPicks()
	initial.Values["python.framework"] = registry.NewSingle("fastapi")
	initial.Values["infra"] = registry.NewMulti([]string{"docker"})

	sf, err := BuildScopeForm(reg, "python", "", initial)
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

	if got := *sf.singles["python.framework"]; got != "fastapi" {
		t.Errorf("python.framework bound = %q, want fastapi", got)
	}
	if got := *sf.multis["infra"]; !reflect.DeepEqual(got, []string{"docker"}) {
		t.Errorf("infra bound = %v, want [docker]", got)
	}
	// Unseeded single-pick falls back to huh v2's first-option pre-fill.
	// stdlib is the first option in `python/20-logging/_index.yaml`.
	if got := *sf.singles["python.logging"]; got != "stdlib" {
		t.Errorf("python.logging bound = %q, want stdlib (huh v2 pre-fills first option)", got)
	}
}

// TestScopeForm_PicksTranslation_TwoLevel asserts Picks() turns bound
// state into a registry.Picks with `language` set and each chosen leaf
// present in the right shape, for the two-level case (no container).
func TestScopeForm_PicksTranslation_TwoLevel(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	sf, err := BuildScopeForm(reg, "python", "", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

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
	// No container case: the typeID should NOT be set in picks.
	if _, ok := picks.Values["python.type"]; ok {
		t.Errorf("unexpected python.type in two-level picks output")
	}
}

// TestScopeForm_PicksTranslation_ThreeLevel asserts the container's
// type pick reaches the Picks output under the `<lang>.<containerID>`
// key, and the container-option-scope leaves are keyed
// `<lang>.<typeID>.<sub>`.
func TestScopeForm_PicksTranslation_ThreeLevel(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	sf, err := BuildScopeForm(reg, "python", "cli", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

	*sf.singles["python.cli.framework"] = "click"
	*sf.singles["python.cli.consumer"] = "agent"
	*sf.singles["python.logging"] = "loguru"
	*sf.multis["infra"] = []string{"docker"}

	picks := sf.Picks()
	if got := picks.Values["language"]; got == nil || got.Single == nil || *got.Single != "python" {
		t.Errorf("language pick = %+v, want python", got)
	}
	if got := picks.Values["python.type"]; got == nil || got.Single == nil || *got.Single != "cli" {
		t.Errorf("python.type pick = %+v, want cli", got)
	}
	if got := picks.Values["python.cli.framework"]; got == nil || got.Single == nil || *got.Single != "click" {
		t.Errorf("python.cli.framework pick = %+v, want click", got)
	}
	if got := picks.Values["python.cli.consumer"]; got == nil || got.Single == nil || *got.Single != "agent" {
		t.Errorf("python.cli.consumer pick = %+v, want agent", got)
	}
	if got := picks.Values["python.logging"]; got == nil || got.Single == nil || *got.Single != "loguru" {
		t.Errorf("python.logging pick = %+v, want loguru", got)
	}
	if got := picks.Values["infra"]; got == nil || got.Multi == nil ||
		!reflect.DeepEqual(*got.Multi, []string{"docker"}) {
		t.Errorf("infra pick = %+v, want [docker]", got)
	}
}

// TestScopeForm_PicksTranslation_ResolveSelectionCompat asserts the
// keys ScopeForm.Picks emits are accepted by ResolveSelection — the
// resolver's index covers the namespace the form produces. This is
// the integration contract between form output and the registry
// resolver introduced by Dispatch 2.
func TestScopeForm_PicksTranslation_ResolveSelectionCompat(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	sf, err := BuildScopeForm(reg, "python", "cli", registry.NewPicks())
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}
	*sf.singles["python.cli.framework"] = "click"
	*sf.singles["python.cli.consumer"] = "agent"
	*sf.singles["python.logging"] = "loguru"
	*sf.multis["infra"] = []string{"docker"}

	picks := sf.Picks()
	// Re-shape Picks as a map[string]any for ResolveSelection (it's
	// the lossy boundary the resolver expects).
	input := map[string]any{}
	for k, v := range picks.Values {
		switch {
		case v.Single != nil:
			input[k] = *v.Single
		case v.Multi != nil:
			input[k] = *v.Multi
		}
	}
	if _, err := registry.ResolveSelection(reg, input); err != nil {
		t.Errorf("ResolveSelection rejected form-produced keys: %v", err)
	}
}

// TestBuildScopeForm_Errors asserts the obvious failure modes surface
// a clean error rather than panicking.
func TestBuildScopeForm_Errors(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	if _, err := BuildScopeForm(nil, "python", "", registry.NewPicks()); err == nil {
		t.Error("expected error for nil registry")
	}
	if _, err := BuildScopeForm(reg, "", "", registry.NewPicks()); err == nil {
		t.Error("expected error for empty langID")
	}
	if _, err := BuildScopeForm(&registry.Registry{}, "python", "", registry.NewPicks()); err == nil {
		t.Error("expected error for empty registry")
	}
}

// TestSummarise_TwoLevelRenderOrder asserts the confirm-group summary
// lists blocks in render order for the two-level case: language base,
// language subs (no container expansion), then cross-cutting.
func TestSummarise_TwoLevelRenderOrder(t *testing.T) {
	t.Parallel()

	reg := validFixture(t)
	initial := registry.NewPicks()
	initial.Values["python.framework"] = registry.NewSingle("fastapi")
	initial.Values["python.logging"] = registry.NewSingle("loguru")
	initial.Values["infra"] = registry.NewMulti([]string{"docker", "kubernetes"})
	initial.Values["ci"] = registry.NewMulti([]string{"github-actions"})

	sf, err := BuildScopeForm(reg, "python", "", initial)
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

	got := sf.summarise(reg)
	want := "blocks: language/python, framework/fastapi, logging/loguru, infra/docker, infra/kubernetes, ci/github-actions"
	if got != want {
		t.Errorf("summary mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestSummarise_ThreeLevelRenderOrder asserts the summary expands the
// container into its chosen-option's leaves in the right place: after
// the language-universal leaves that precede the container in folder-
// prefix order (none here — the type container is `10-type`, so it
// runs first), and before any that come after.
//
// Fixture order: python's subs are `10-type` (container) then
// `30-logging` (leaf). Cross-cutting: `20-infra` only.
func TestSummarise_ThreeLevelRenderOrder(t *testing.T) {
	t.Parallel()

	reg := threeLevelFixture(t)
	initial := registry.NewPicks()
	initial.Values["python.cli.framework"] = registry.NewSingle("click")
	initial.Values["python.cli.consumer"] = registry.NewSingle("agent")
	initial.Values["python.logging"] = registry.NewSingle("loguru")
	initial.Values["infra"] = registry.NewMulti([]string{"docker"})

	sf, err := BuildScopeForm(reg, "python", "cli", initial)
	if err != nil {
		t.Fatalf("BuildScopeForm: %v", err)
	}

	got := sf.summarise(reg)
	want := "blocks: language/python, type/cli, framework/click, consumer/agent, logging/loguru, infra/docker"
	if got != want {
		t.Errorf("summary mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestSummarise_EmptySelection asserts the summary text is a stable
// "(no blocks selected)" sentinel when nothing has been picked. The
// fallback covers the corner case where every binding is empty.
func TestSummarise_EmptySelection(t *testing.T) {
	t.Parallel()

	// Use an empty registry (no language category) — the form
	// construction itself fails for that, so the closest reachable
	// case is a valid registry with an empty langID. But we filter
	// against empty langID in the builder, so we instead test the
	// branch by directly constructing a minimal ScopeForm.
	sf := &ScopeForm{
		singles: map[string]*string{},
		multis:  map[string]*[]string{},
	}
	got := sf.summarise(&registry.Registry{})
	want := "(no blocks selected)"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}
