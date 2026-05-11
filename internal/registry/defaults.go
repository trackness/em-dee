package registry

// ApplyDefaults returns a new Picks with each unset category filled
// from the registry's `default`. The contract (spec §3.3 / §8.2):
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
	// the input picks (not `out`) — semantically identical, but
	// makes the read-vs-write distinction explicit.
	var chosenLang string
	if v, ok := picks.Values["language"]; ok && v != nil && v.Single != nil {
		chosenLang = *v.Single
	}

	for _, cat := range reg.Categories {
		switch cat.ID {
		case "language":
			fillIfUnset(out, "language", cat)
		default:
			fillIfUnset(out, cat.ID, cat)
		}

		// Language subtree: only fill the chosen language's
		// subcategories. If language is unset, skip the entire
		// subtree — defaulting framework / logging without a
		// language to anchor them would be meaningless.
		if cat.ID == "language" && chosenLang != "" {
			for _, sub := range cat.Subcategories[chosenLang] {
				key := chosenLang + "." + sub.ID
				fillIfUnset(out, key, sub)
			}
		}
	}

	return out
}

// fillIfUnset writes the registry default into `out[key]` only if
// `out[key]` is nil (unset). Explicit-none values (non-nil pointer to
// an empty value) are left alone.
func fillIfUnset(out Picks, key string, cat *Category) {
	if _, ok := out.Values[key]; ok {
		// Already present (chosen or explicit-none) — leave alone.
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
