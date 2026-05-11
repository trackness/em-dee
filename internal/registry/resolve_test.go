package registry

import (
	"reflect"
	"strings"
	"testing"
)

// TestResolveSelection_SinglePickFromString: a single-pick category
// accepts a string value and produces a chosen-single Value.
func TestResolveSelection_SinglePickFromString(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language":         "python",
		"python.framework": "django",
	}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	lang := out.Values["language"]
	if lang == nil || lang.Single == nil || *lang.Single != "python" {
		t.Errorf("language = %+v, want chosen=python", lang)
	}
	fw := out.Values["python.framework"]
	if fw == nil || fw.Single == nil || *fw.Single != "django" {
		t.Errorf("python.framework = %+v, want chosen=django", fw)
	}
}

// TestResolveSelection_MultiPickFromSlice: a multi-pick category
// accepts []string and produces a chosen-multi Value in registry
// declaration order (not input order).
func TestResolveSelection_MultiPickFromSlice(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	// Input order is reverse of registry order; output must
	// normalize to registry order (determinism rule).
	in := map[string]any{
		"language": "python",
		"infra":    []string{"kubernetes", "docker"},
	}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	infra := out.Values["infra"]
	if infra == nil || infra.Multi == nil {
		t.Fatalf("infra missing or untyped: %+v", infra)
	}
	want := []string{"docker", "kubernetes"}
	if !reflect.DeepEqual(*infra.Multi, want) {
		t.Errorf("infra = %v, want %v (registry declaration order)", *infra.Multi, want)
	}
}

// TestResolveSelection_MultiPickFromCSV: cobra delivers multi-pick
// flags as comma-separated strings when we use plain String flags
// (see plan Task 3.4 tradeoff). The resolver accepts both forms.
func TestResolveSelection_MultiPickFromCSV(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": "python",
		"infra":    "kubernetes,docker",
	}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	want := []string{"docker", "kubernetes"} // normalized to registry order
	got := *out.Values["infra"].Multi
	if !reflect.DeepEqual(got, want) {
		t.Errorf("infra = %v, want %v", got, want)
	}
}

// TestResolveSelection_SinglePickEmptyString: an empty value for a
// single-pick category becomes the explicit-none state (non-nil
// pointer, empty string).
func TestResolveSelection_SinglePickEmptyString(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language":         "python",
		"python.framework": "",
	}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	fw := out.Values["python.framework"]
	if fw == nil || fw.Single == nil || *fw.Single != "" {
		t.Errorf("python.framework = %+v, want explicit-none", fw)
	}
}

// TestResolveSelection_MultiPickEmptySlice: an empty []string becomes
// the explicit-none state for a multi-pick category.
func TestResolveSelection_MultiPickEmptySlice(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": "python",
		"infra":    []string{},
	}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}

	infra := out.Values["infra"]
	if infra == nil || infra.Multi == nil {
		t.Fatalf("infra should be non-nil pointer with empty slice, got %+v", infra)
	}
	if len(*infra.Multi) != 0 {
		t.Errorf("infra = %v, want empty slice", *infra.Multi)
	}
}

// TestResolveSelection_MultiPickEmptyStringInSlice: pins the
// asymmetry called out in L3 of the Phase 1 review: a single-element
// `[]string{""}` is **not** explicit-none (that would be a zero-
// length slice). It's a one-element list with an empty entry, which
// fails option-id validation. `infra: ""` (csv form) → explicit-none;
// `infra: [""]` → `unknown option ""`. Both error sensibly, but the
// asymmetry is real and we pin it.
func TestResolveSelection_MultiPickEmptyStringInSlice(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": "python",
		"infra":    []string{""},
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for []string{\"\"}, got nil")
	}
	if !strings.Contains(err.Error(), `unknown option ""`) {
		t.Errorf("error %q should mention 'unknown option \"\"'", err)
	}
}

// TestResolveSelection_UnknownCategory: a key the registry doesn't
// recognise is a hard error.
func TestResolveSelection_UnknownCategory(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": "python",
		"nosuch":   "x",
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for unknown category, got nil")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("error %q should mention 'nosuch'", err)
	}
}

// TestResolveSelection_UnknownOption: an option id not in the
// category's options list is a hard error.
func TestResolveSelection_UnknownOption(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language":         "python",
		"python.framework": "rails",
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for unknown option, got nil")
	}
	if !strings.Contains(err.Error(), "rails") {
		t.Errorf("error %q should mention 'rails'", err)
	}
}

// TestResolveSelection_WrongType_SingleGivenList: passing a []string
// to a single-pick category is a hard error.
func TestResolveSelection_WrongType_SingleGivenList(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": []string{"python", "go"},
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for list-into-single, got nil")
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("error %q should mention 'language'", err)
	}
}

// TestResolveSelection_WrongType_BogusType: passing an int (or any
// non-string/non-[]string) is a hard error.
func TestResolveSelection_WrongType_BogusType(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": 123,
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for non-string-non-list, got nil")
	}
}

// TestResolveSelection_RequiredEmpty: a required category given an
// empty value is a hard error.
func TestResolveSelection_RequiredEmpty(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{
		"language": "",
	}
	_, err := ResolveSelection(reg, in)
	if err == nil {
		t.Fatal("expected error for required-empty, got nil")
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("error %q should mention 'language'", err)
	}
}

// TestResolveSelection_RequiredOmitted: an omitted required category
// is NOT a resolution error — defaults / interactive handle that.
// `ResolveSelection` only enforces the type and id validity of what
// the user passed.
func TestResolveSelection_RequiredOmitted(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	in := map[string]any{}
	out, err := ResolveSelection(reg, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out.Values["language"]; ok {
		t.Errorf("language should remain unset, got %+v", out.Values["language"])
	}
}

// TestResolveSelection_MultiPickDeterministicOrder: locks in the
// registry-declaration ordering rule by passing inputs in many
// permutations and asserting one canonical output.
func TestResolveSelection_MultiPickDeterministicOrder(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	inputs := []any{
		[]string{"docker", "kubernetes"},
		[]string{"kubernetes", "docker"},
		"docker,kubernetes",
		"kubernetes,docker",
		// Whitespace permitted in csv form.
		" kubernetes , docker ",
	}
	want := []string{"docker", "kubernetes"}
	for _, in := range inputs {
		m := map[string]any{"language": "python", "infra": in}
		out, err := ResolveSelection(reg, m)
		if err != nil {
			t.Fatalf("input %v: error %v", in, err)
		}
		if !reflect.DeepEqual(*out.Values["infra"].Multi, want) {
			t.Errorf("input %v: got %v, want %v", in, *out.Values["infra"].Multi, want)
		}
	}
}
