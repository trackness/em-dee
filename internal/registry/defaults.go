package registry

// ApplyDefaults returns a new Picks with each unset category filled
// from the registry's `default`. The contract:
//
//   - nil pointer (unset, or map-absent) → fill with default if one
//     exists.
//   - any non-nil pointer (the user expressed a value, even an empty
//     one) → leave alone. The "explicit empty defeats the default"
//     state is no longer reachable through the public API (Dispatch 1
//     collapse), but defensively leaving non-nil values untouched
//     keeps the function shape simple and avoids surprising any caller
//     that hand-constructs Picks via NewSingle("")/NewMulti(nil).
//
// The function is pure: it deep-copies the input map so the caller's
// Picks is unchanged. Language-nested categories are only considered
// when a language has been chosen (we wouldn't know which subtree to
// walk otherwise).
func ApplyDefaults(picks Picks, reg *Registry) Picks {
	out := Picks{Values: map[string]*Value{}}
	for k, v := range picks.Values {
		out.Values[k] = cloneValue(v)
	}

	for _, cat := range reg.Categories {
		applyDefaultsTree(out, cat, "")
	}

	return out
}

// applyDefaultsTree fills the default for `cat` if it's unset, then
// recurses into container subtrees: a container's chosen option
// determines which sub-tree's defaults to apply. Containers with no
// chosen option skip recursion (defaulting blocks anchored under an
// un-chosen container scope would be meaningless — there is no scope).
//
// `prefix` is the dotted Picks-key prefix in effect; an empty prefix
// means we're at the top level. When descending into a container's
// chosen option, the prefix gains the chosen option's id (eliding the
// container's own id per CONTENT-STYLE.md §2.3).
func applyDefaultsTree(out Picks, cat *Category, prefix string) {
	key := cat.ID
	if prefix != "" {
		key = prefix + "." + cat.ID
	}
	fillIfUnset(out, key, cat)
	if !cat.IsContainer {
		return
	}

	// Identify the chosen option (after defaults have been applied for
	// the container itself). A container with no chosen option — and
	// no default to fill — has an empty subtree's worth of defaults
	// to apply, so we just return.
	v, ok := out.Values[key]
	if !ok || v == nil || v.Single == nil || *v.Single == "" {
		return
	}
	chosen := *v.Single

	childPrefix := chosen
	if prefix != "" {
		childPrefix = prefix + "." + chosen
	}
	for _, sub := range cat.Subcategories[chosen] {
		applyDefaultsTree(out, sub, childPrefix)
	}
}

// fillIfUnset writes the registry default into `out[key]` only if
// `out[key]` is "unset". "Unset" means **nil pointer** — which covers
// both map-absent AND map-present-with-nil-value. Any non-nil *Value
// is left alone, on the principle that the caller spoke for itself.
// The map-present-nil case can arise from generic merge helpers or
// from cloneValue of a nil entry; treating it the same as map-absent
// keeps the contract unambiguous for downstream render/CLI consumers.
func fillIfUnset(out Picks, key string, cat *Category) {
	if v, ok := out.Values[key]; ok && v != nil {
		// Already present and non-nil — leave alone.
		return
	}
	switch cat.Pick {
	case PickSingle:
		if cat.DefaultSingle == "" {
			return
		}
		out.Values[key] = NewSingle(cat.DefaultSingle)
	case PickMulti:
		if len(cat.DefaultMulti) == 0 {
			return
		}
		// Copy the slice so subsequent edits to one Picks don't
		// reach into another via the shared backing array.
		cp := make([]string, len(cat.DefaultMulti))
		copy(cp, cat.DefaultMulti)
		out.Values[key] = NewMulti(cp)
	}
}

// cloneValue deep-copies a *Value so the returned Picks shares no
// pointer state with the input.
func cloneValue(v *Value) *Value {
	if v == nil {
		return nil
	}
	out := &Value{}
	if v.Single != nil {
		s := *v.Single
		out.Single = &s
	}
	if v.Multi != nil {
		cp := make([]string, len(*v.Multi))
		copy(cp, *v.Multi)
		out.Multi = &cp
	}
	return out
}
