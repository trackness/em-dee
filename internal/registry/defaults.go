package registry

// ApplyDefaults returns a new Picks with each unset category filled
// from the registry's `default`. The contract:
//
//   - nil pointer (unset) → fill with default if one exists.
//   - non-nil pointer to empty value (explicit-none) → leave alone.
//   - non-nil pointer to non-empty value (chosen) → leave alone.
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

	// Resolve which language has been chosen, if any. We read from
	// the input picks (not `out`) — semantically identical in v1
	// (the language category has no default, so the input's language
	// value is what the output sees too). Forward-compat hazard: if a
	// language default is ever introduced, reading from `picks` would
	// miss the just-filled default and a downstream consumer of the
	// language subtree wouldn't see it here. Re-read from `out` after
	// `fillIfUnset` if that changes.
	var chosenLang string
	if v, ok := picks.Values[LanguageCategoryID]; ok && v != nil && v.Single != nil {
		chosenLang = *v.Single
	}

	for _, cat := range reg.Categories {
		switch cat.ID {
		case LanguageCategoryID:
			fillIfUnset(out, LanguageCategoryID, cat)
		default:
			fillIfUnset(out, cat.ID, cat)
		}

		// Language subtree: only fill the chosen language's
		// subcategories. If language is unset, skip the entire
		// subtree — defaulting framework / logging without a
		// language to anchor them would be meaningless.
		if cat.ID == LanguageCategoryID && chosenLang != "" {
			for _, sub := range cat.Subcategories[chosenLang] {
				key := chosenLang + "." + sub.ID
				fillIfUnset(out, key, sub)
			}
		}
	}

	return out
}

// fillIfUnset writes the registry default into `out[key]` only if
// `out[key]` is "unset". "Unset" means **nil pointer** — which covers
// both map-absent AND map-present-with-nil-value. Explicit-none values
// (non-nil pointer to an empty value) are left alone. The
// map-present-nil case can arise from generic merge helpers or from
// cloneValue of a nil entry; treating it the same as map-absent keeps
// the contract unambiguous for downstream render/CLI consumers.
func fillIfUnset(out Picks, key string, cat *Category) {
	if v, ok := out.Values[key]; ok && v != nil {
		// Already present and non-nil (chosen or explicit-none) —
		// leave alone.
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
