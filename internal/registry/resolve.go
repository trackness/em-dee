package registry

import (
	"fmt"
	"sort"
	"strings"
)

// ResolveSelection converts a loose map of selection input into a
// fully typed Picks. It is the single resolution entry point shared
// between CLI selection handling (the surviving --language flag) and
// future configuration-file loaders, so the two cannot drift.
//
// Input shape:
//
//   - Key form: top-level category id (`infra`) or nested
//     `<lang-id>.<category-id>` (`python.framework`), matching the
//     dotted-reference grammar.
//   - Value form for single-pick: `string`. A `[]string` for a
//     single-pick category is a hard error. An empty string for a
//     required category is rejected up-front (required-empty); for an
//     optional category it produces a non-nil-empty cell internally,
//     but this shape is no longer constructable through the public
//     surface after Dispatch 1.
//   - Value form for multi-pick: `[]string` OR a comma-separated
//     `string`. Whitespace around csv entries is trimmed.
//
// Errors are aggregated up-front via fmt.Errorf — the resolver is
// strict by design (reject everything before constructing the Picks),
// not a multi-error sink like Validate.
func ResolveSelection(reg *Registry, m map[string]any) (Picks, error) {
	out := NewPicks()

	// Build an index of valid keys → owning category for O(1)
	// lookup. The index covers both top-level (`infra`) and
	// language-nested (`python.framework`) forms.
	index := buildKeyIndex(reg)

	// Deterministic iteration so error messages are stable across runs.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		cat, ok := index[key]
		if !ok {
			return Picks{}, fmt.Errorf("unknown category %q", key)
		}
		val, err := resolveValue(cat, m[key])
		if err != nil {
			return Picks{}, fmt.Errorf("%s: %w", key, err)
		}
		// Required-empty is a hard error here.
		if cat.Required && isEmpty(val) {
			return Picks{}, fmt.Errorf("%s: required category cannot be empty", key)
		}
		out.Values[key] = val
	}

	return out, nil
}

// buildKeyIndex flattens the registry into a `key → *Category` map.
// Top-level categories use their plain ID; language sub-categories
// use `<lang-id>.<sub-id>`.
func buildKeyIndex(reg *Registry) map[string]*Category {
	index := map[string]*Category{}
	for _, cat := range reg.Categories {
		index[cat.ID] = cat
		if cat.ID == LanguageCategoryID {
			for langID, subs := range cat.Subcategories {
				for _, sub := range subs {
					index[langID+"."+sub.ID] = sub
				}
			}
		}
	}
	return index
}

// resolveValue converts one input value (string | []string) into a
// *Value of the right shape, enforcing both the type contract and
// option-id validity against the category's options list. For
// multi-pick, the output slice is normalised to registry declaration
// order (the determinism rule) and de-duplicated.
func resolveValue(cat *Category, raw any) (*Value, error) {
	switch cat.Pick {
	case PickSingle:
		s, err := asString(raw)
		if err != nil {
			return nil, err
		}
		if s == "" {
			// Empty string for an optional category produces a non-nil
			// pointer to "" internally; required categories are
			// rejected one frame up via isEmpty. The cell shape is
			// retained for renderer/fillIfUnset behaviour but is no
			// longer reachable through the public API.
			empty := ""
			return &Value{Single: &empty}, nil
		}
		if !optionExists(cat, s) {
			return nil, fmt.Errorf("unknown option %q", s)
		}
		v := s
		return &Value{Single: &v}, nil

	case PickMulti:
		items, err := asStringList(raw)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			empty := []string{}
			return &Value{Multi: &empty}, nil
		}
		// Validate every id is real.
		seen := map[string]bool{}
		for _, id := range items {
			if !optionExists(cat, id) {
				return nil, fmt.Errorf("unknown option %q", id)
			}
			seen[id] = true
		}
		// Re-emit in registry declaration order (drops duplicates
		// while we're at it).
		ordered := make([]string, 0, len(seen))
		for _, opt := range cat.Options {
			if seen[opt.ID] {
				ordered = append(ordered, opt.ID)
			}
		}
		return &Value{Multi: &ordered}, nil

	default:
		return nil, fmt.Errorf("category has unsupported pick=%q", cat.Pick)
	}
}

// asString accepts a Go value that should be a single string (per the
// single-pick contract).
func asString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []string:
		return "", fmt.Errorf("single-pick category cannot accept a list (got %v)", v)
	default:
		return "", fmt.Errorf("expected string, got %T", raw)
	}
}

// asStringList accepts a `[]string` or a comma-separated `string`.
// Whitespace around csv entries is trimmed; a leading/trailing comma
// is not allowed (it produces an empty entry which fails option-id
// validation). An empty string maps to an empty list.
func asStringList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		// Trim each entry so callers can pass whitespace-padded
		// items uniformly with the csv path.
		out := make([]string, 0, len(v))
		for _, s := range v {
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return []string{}, nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string or list, got %T", raw)
	}
}

// optionExists is the package-internal alias for Category.HasOption.
// Kept so existing resolver call sites stay terse; new consumers
// outside the package use HasOption.
func optionExists(cat *Category, id string) bool {
	return cat.HasOption(id)
}

// isEmpty reports whether a non-nil *Value carries no option ids.
// Only used to enforce "required category cannot be empty" at
// resolution time — `resolveValue` always returns a non-nil *Value
// or an error, so this is only called on non-nil values.
func isEmpty(v *Value) bool {
	if v.Single != nil {
		return *v.Single == ""
	}
	if v.Multi != nil {
		return len(*v.Multi) == 0
	}
	return true
}
