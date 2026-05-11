package render

import (
	"bytes"
	"fmt"

	"github.com/trackness/em-dee/internal/registry"
)

// Render concatenates the chosen block files into a single CLAUDE.md
// payload, walking the registry in render order per spec §4.4:
//
//	templates/10-language/<lang>/base.md
//	templates/10-language/<lang>/<NN-sub>/<chosen>.md   (each sub-cat)
//	templates/20-infra/<chosen>.md     (multi, manifest order)
//	templates/30-ci/<chosen>.md        (multi, manifest order)
//	... and so on for every cross-cutting category
//
// Block separator is "\n\n" between blocks, with each block's own
// trailing newlines stripped before joining so we never emit triple
// newlines from blocks that already end with "\n". The final output
// terminates with exactly one "\n" (unix-convention), unless the
// output is empty.
//
// Multi-pick ordering is always manifest order regardless of the
// order options appear in `picks`; this locks in the §4.4
// determinism contract so equivalent selections produce byte-equal
// output.
//
// Errors:
//   - unknown option id in picks → error. `ResolveSelection` is the
//     designed-in check for this; if the renderer sees an unknown id
//     it means an invariant was bypassed and silent fallback would
//     hide the bug.
//   - read failure on a block file → error wrapping the cause.
//
// Pure: no `os` calls. All reads go through `Registry.OptionBlock`,
// which uses the fs.FS the registry was loaded from.
func Render(reg *registry.Registry, picks registry.Picks) ([]byte, error) {
	if reg == nil {
		return nil, fmt.Errorf("Render: nil registry")
	}

	var blocks [][]byte

	for _, cat := range reg.Categories {
		if cat.ID == registry.LanguageCategoryID {
			langBlocks, err := renderLanguage(reg, cat, picks)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, langBlocks...)
			continue
		}
		catBlocks, err := renderCategory(reg, cat, picks.Values[cat.ID])
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", cat.ID, err)
		}
		blocks = append(blocks, catBlocks...)
	}

	return join(blocks), nil
}

// renderLanguage emits the language base.md plus each nested
// sub-category's chosen blocks, in sub-category folder-prefix order.
// If no language is chosen (unset or explicit-none), nothing is
// emitted — including no sub-category content, because the subtree
// only makes sense once a language anchors it.
func renderLanguage(reg *registry.Registry, langCat *registry.Category, picks registry.Picks) ([][]byte, error) {
	v := picks.Values[registry.LanguageCategoryID]
	if v == nil || v.Single == nil || *v.Single == "" {
		return nil, nil
	}
	langID := *v.Single

	// Language base block. The language category's options carry the
	// `file: <lang>/base.md` path; reading it via OptionBlock keeps
	// the path-joining logic in one place.
	base, err := reg.OptionBlock(langCat, langID)
	if err != nil {
		return nil, fmt.Errorf("render language: %w", err)
	}
	out := [][]byte{base}

	// Sub-categories in manifest order (the registry already sorts by
	// folder prefix during walk).
	for _, sub := range langCat.Subcategories[langID] {
		key := langID + "." + sub.ID
		subBlocks, err := renderCategory(reg, sub, picks.Values[key])
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", key, err)
		}
		out = append(out, subBlocks...)
	}
	return out, nil
}

// renderCategory emits the chosen block(s) for one category. Returns
// nil (no blocks) for unset / explicit-none. For multi-pick, options
// are emitted in manifest declaration order regardless of the order
// the caller wrote them in `picks` — this is the §4.4 determinism
// rule, locked in by TestRender_MultiPickDeterminism.
func renderCategory(reg *registry.Registry, cat *registry.Category, v *registry.Value) ([][]byte, error) {
	if v == nil {
		return nil, nil // unset
	}

	switch cat.Pick {
	case registry.PickSingle:
		if v.Single == nil || *v.Single == "" {
			return nil, nil // explicit-none
		}
		b, err := reg.OptionBlock(cat, *v.Single)
		if err != nil {
			return nil, err
		}
		return [][]byte{b}, nil

	case registry.PickMulti:
		if v.Multi == nil || len(*v.Multi) == 0 {
			return nil, nil // explicit-none
		}
		chosen := map[string]bool{}
		for _, id := range *v.Multi {
			chosen[id] = true
		}
		// Manifest order, not picks order — determinism contract.
		var out [][]byte
		for _, opt := range cat.Options {
			if !chosen[opt.ID] {
				continue
			}
			delete(chosen, opt.ID)
			b, err := reg.OptionBlock(cat, opt.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		// Anything left in `chosen` is an unknown id. Surface loudly
		// rather than silently drop — ResolveSelection should have
		// caught it, but defensive surfacing beats hidden coupling.
		for id := range chosen {
			return nil, fmt.Errorf("category %q: unknown option %q in picks", cat.ID, id)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("category %q: unsupported pick=%q", cat.ID, cat.Pick)
	}
}

// join concatenates blocks with "\n\n" between them, stripping each
// block's trailing newlines before joining so a block that ends in
// "\n" doesn't produce a triple-newline gap. The final output ends
// with exactly one "\n"; an empty block list yields zero bytes.
func join(blocks [][]byte) []byte {
	if len(blocks) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for i, b := range blocks {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		buf.Write(bytes.TrimRight(b, "\n"))
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
