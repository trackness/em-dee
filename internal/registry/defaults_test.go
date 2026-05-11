package registry

import (
	"reflect"
	"testing"
)

// testRegistry constructs an in-memory Registry covering the cases
// ApplyDefaults must handle: single-pick with default, single-pick
// without default, multi-pick with default, multi-pick without
// default, language with sub-categories, and a synthetic required-
// with-default category (forward-compat — not in v1 catalog but part
// of the contract).
func testRegistry() *Registry {
	return &Registry{
		Categories: []*Category{
			{
				Path:     "templates/10-language",
				ID:       "language",
				Pick:     PickSingle,
				Required: true,
				Options:  []Option{{ID: "python"}, {ID: "go"}},
				Subcategories: map[string][]*Category{
					"python": {
						{
							Path:          "templates/10-language/python/10-framework",
							ID:            "framework",
							Pick:          PickSingle,
							DefaultSingle: "fastapi",
							Options:       []Option{{ID: "fastapi"}, {ID: "django"}},
						},
						{
							Path:    "templates/10-language/python/20-logging",
							ID:      "logging",
							Pick:    PickSingle,
							Options: []Option{{ID: "stdlib"}, {ID: "loguru"}},
						},
					},
					"go": {},
				},
			},
			{
				Path:         "templates/20-infra",
				ID:           "infra",
				Pick:         PickMulti,
				DefaultMulti: []string{"docker"},
				Options:      []Option{{ID: "docker"}, {ID: "kubernetes"}},
			},
			{
				Path:    "templates/30-ci",
				ID:      "ci",
				Pick:    PickMulti,
				Options: []Option{{ID: "github-actions"}},
			},
			// Forward-compat: required+default. v1 catalog has none
			// (language is the only required category and forbids
			// default), but ApplyDefaults must still fill this in.
			{
				Path:          "templates/99-synthetic",
				ID:            "synthetic",
				Pick:          PickSingle,
				Required:      true,
				DefaultSingle: "alpha",
				Options:       []Option{{ID: "alpha"}, {ID: "beta"}},
			},
		},
	}
}

// TestApplyDefaults_UnsetFilledFromDefault: a category that is unset
// (nil pointer) gets the registry default applied.
func TestApplyDefaults_UnsetFilledFromDefault(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	// language=python so the python subtree is considered.
	picks.Values["language"] = NewSingle("python")

	out := ApplyDefaults(picks, testRegistry())

	got := out.Values["python.framework"]
	if got == nil || got.Single == nil {
		t.Fatalf("python.framework missing or untyped: %+v", got)
	}
	if *got.Single != "fastapi" {
		t.Errorf("python.framework = %q, want %q", *got.Single, "fastapi")
	}

	infra := out.Values["infra"]
	if infra == nil || infra.Multi == nil {
		t.Fatalf("infra missing or untyped: %+v", infra)
	}
	if !reflect.DeepEqual(*infra.Multi, []string{"docker"}) {
		t.Errorf("infra = %v, want [docker]", *infra.Multi)
	}
}

// TestApplyDefaults_ExplicitNonePreserved: an explicit-empty Picks
// value (non-nil pointer, empty value) is left alone.
func TestApplyDefaults_ExplicitNonePreserved(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")
	// Explicit-none on python.framework
	empty := ""
	picks.Values["python.framework"] = &Value{Single: &empty}
	// Explicit-none on infra (non-nil empty slice).
	picks.Values["infra"] = NewMulti(nil)

	out := ApplyDefaults(picks, testRegistry())

	got := out.Values["python.framework"]
	if got == nil || got.Single == nil || *got.Single != "" {
		t.Errorf("python.framework expected explicit-empty, got %+v", got)
	}
	infra := out.Values["infra"]
	if infra == nil || infra.Multi == nil || len(*infra.Multi) != 0 {
		t.Errorf("infra expected explicit-empty, got %+v", infra)
	}
}

// TestApplyDefaults_ChosenPreserved: a chosen value isn't overwritten
// by the default.
func TestApplyDefaults_ChosenPreserved(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")
	picks.Values["python.framework"] = NewSingle("django")
	picks.Values["infra"] = NewMulti([]string{"kubernetes"})

	out := ApplyDefaults(picks, testRegistry())

	if *out.Values["python.framework"].Single != "django" {
		t.Errorf("python.framework = %q, want %q",
			*out.Values["python.framework"].Single, "django")
	}
	if !reflect.DeepEqual(*out.Values["infra"].Multi, []string{"kubernetes"}) {
		t.Errorf("infra = %v, want [kubernetes]", *out.Values["infra"].Multi)
	}
}

// TestApplyDefaults_UnsetNoDefaultStaysUnset: a category that is
// unset and has no default in the registry stays unset.
func TestApplyDefaults_UnsetNoDefaultStaysUnset(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")

	out := ApplyDefaults(picks, testRegistry())

	if _, ok := out.Values["python.logging"]; ok {
		t.Errorf("python.logging should remain unset (no default), got %+v",
			out.Values["python.logging"])
	}
	if _, ok := out.Values["ci"]; ok {
		t.Errorf("ci should remain unset (no default), got %+v",
			out.Values["ci"])
	}
}

// TestApplyDefaults_RequiredWithDefault: a required category with a
// default gets the default in non-interactive mode. (v1 catalog has
// none; this exercises the forward-compat branch.)
func TestApplyDefaults_RequiredWithDefault(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")

	out := ApplyDefaults(picks, testRegistry())

	syn := out.Values["synthetic"]
	if syn == nil || syn.Single == nil || *syn.Single != "alpha" {
		t.Errorf("synthetic = %+v, want chosen=alpha", syn)
	}
}

// TestApplyDefaults_NonDestructive: ApplyDefaults must not mutate the
// caller's input Picks.
func TestApplyDefaults_NonDestructive(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")

	_ = ApplyDefaults(picks, testRegistry())

	if _, ok := picks.Values["python.framework"]; ok {
		t.Errorf("ApplyDefaults mutated input Picks (filled python.framework): %+v",
			picks.Values["python.framework"])
	}
	if _, ok := picks.Values["infra"]; ok {
		t.Errorf("ApplyDefaults mutated input Picks (filled infra): %+v",
			picks.Values["infra"])
	}
}

// TestApplyDefaults_NilPointerInMapTreatedAsUnset: a key explicitly
// present with a nil *Value must be treated the same as map-absent
// (= "unset", per the Value docstring). Pins the resolution to H1
// from the Phase 1 review: nil pointer is the canonical "unset"
// predicate, not map-key-presence.
func TestApplyDefaults_NilPointerInMapTreatedAsUnset(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")
	// Explicitly nil — could arise from a generic merge helper or
	// from cloning a Picks that had a nil entry. This counts as
	// "unset" and ApplyDefaults must fill the default.
	picks.Values["infra"] = nil

	out := ApplyDefaults(picks, testRegistry())

	infra := out.Values["infra"]
	if infra == nil || infra.Multi == nil {
		t.Fatalf("infra default not applied to nil-in-map entry: %+v", infra)
	}
	if !reflect.DeepEqual(*infra.Multi, []string{"docker"}) {
		t.Errorf("infra = %v, want [docker]", *infra.Multi)
	}
}

// TestApplyDefaults_DeepCopiesMultiSlice: mutating the returned
// Picks's multi-slice in place must not affect the input. Pins the
// non-destructive contract beyond the map-key check in
// TestApplyDefaults_NonDestructive (which only verifies keys aren't
// added to the input).
func TestApplyDefaults_DeepCopiesMultiSlice(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	picks.Values["language"] = NewSingle("python")
	picks.Values["infra"] = NewMulti([]string{"docker", "kubernetes"})

	out := ApplyDefaults(picks, testRegistry())

	(*out.Values["infra"].Multi)[0] = "MUTATED"
	if (*picks.Values["infra"].Multi)[0] != "docker" {
		t.Errorf("input slice aliased into output: %v", *picks.Values["infra"].Multi)
	}
}

// TestApplyDefaults_LanguageUnsetSkipsSubtree: if no language is
// chosen, ApplyDefaults must not fill in any language-nested
// categories — there's no subtree to apply defaults from.
func TestApplyDefaults_LanguageUnsetSkipsSubtree(t *testing.T) {
	t.Parallel()

	picks := NewPicks()
	// language is unset.

	out := ApplyDefaults(picks, testRegistry())

	for k := range out.Values {
		if k == "python.framework" || k == "go.framework" {
			t.Errorf("unexpected language-nested fill: %s", k)
		}
	}
	// Cross-cutting defaults still apply.
	if out.Values["infra"] == nil {
		t.Error("infra default should still apply with no language chosen")
	}
}
