package render

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/trackness/em-dee/internal/registry"
)

// Render concatenates the chosen block files into a single CLAUDE.md
// payload, walking the registry in render order:
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
// order options appear in `picks`; this locks in the determinism
// contract so equivalent selections produce byte-equal output.
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

	blocks, err := renderScope(reg, reg.Categories, "", picks)
	if err != nil {
		return nil, err
	}
	return join(blocks), nil
}

// renderScope emits the blocks for one scope: every leaf category's
// chosen options, plus every container category's recursive subtree.
// `prefix` is the dotted Picks-key prefix in effect (empty at top
// level; `<lang>` after descending into the language container;
// `<lang>.<chosen-type>` after descending into a hypothetical type
// container). Container traversal feeds Picks-key construction the
// chosen *option* id rather than the container category's own id,
// matching CONTENT-STYLE.md §2.3 — the container disappears from the
// user-facing namespace, only the chosen option's name appears in
// dotted refs and Picks keys.
func renderScope(reg *registry.Registry, cats []*registry.Category, prefix string, picks registry.Picks) ([][]byte, error) {
	var out [][]byte
	for _, cat := range cats {
		key := cat.ID
		if prefix != "" {
			key = prefix + "." + cat.ID
		}
		if cat.IsContainer {
			subBlocks, err := renderContainer(reg, cat, key, prefix, picks)
			if err != nil {
				return nil, err
			}
			out = append(out, subBlocks...)
			continue
		}
		catBlocks, err := renderCategory(reg, cat, picks.Values[key])
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", key, err)
		}
		out = append(out, catBlocks...)
	}
	return out, nil
}

// renderContainer emits the chosen option's scope-`base.md` (if the
// container's options point at base.md files), then recurses into the
// chosen option's subtree. If no option is chosen, the entire subtree
// is skipped — a container subtree only makes sense once its anchor
// option is set.
//
// `containerKey` is the dotted Picks-key for the container itself
// (e.g. `language` at the top level; `python.type` once the type
// container lands). `prefix` is the prefix for the chosen option's
// subtree — the rule is "chosen option id replaces the container's
// id in the namespace" (per CONTENT-STYLE.md §2.3, the container is
// elided in user-facing keys).
func renderContainer(reg *registry.Registry, cat *registry.Category, containerKey, prefix string, picks registry.Picks) ([][]byte, error) {
	v := picks.Values[containerKey]
	if v == nil || v.Single == nil || *v.Single == "" {
		return nil, nil
	}
	chosen := *v.Single

	// Locate the chosen option to find its `file:` reference.
	var opt *registry.Option
	for i := range cat.Options {
		if cat.Options[i].ID == chosen {
			opt = &cat.Options[i]
			break
		}
	}
	if opt == nil {
		return nil, fmt.Errorf("render %s: chosen option %q not in container category", containerKey, chosen)
	}

	var out [][]byte
	// Scope base.md: emit only when the option's `file:` actually
	// points at a base.md file. Container options whose `file:` is
	// just the subdirectory (e.g. `cli/`) have no scope base block.
	if strings.HasSuffix(opt.File, "/base.md") {
		base, err := reg.OptionBlock(cat, chosen)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", containerKey, err)
		}
		out = append(out, base)
	}

	// Recurse into the chosen option's subtree. Namespace prefix is
	// the parent prefix + the chosen option's id (the container's own
	// id is elided per CONTENT-STYLE.md §2.3).
	childPrefix := chosen
	if prefix != "" {
		childPrefix = prefix + "." + chosen
	}
	subBlocks, err := renderScope(reg, cat.Subcategories[chosen], childPrefix, picks)
	if err != nil {
		return nil, err
	}
	out = append(out, subBlocks...)
	return out, nil
}

// renderCategory emits the chosen block(s) for one category. Returns
// nil (no blocks) when the cell carries no option ids — whether the
// value is absent, nil, or a non-nil pointer to an empty value (the
// last shape isn't reachable through the public API after Dispatch 1
// but the renderer stays defensive). For multi-pick, options are
// emitted in manifest declaration order regardless of the order the
// caller wrote them in `picks` — the determinism rule, locked in by
// TestRender_MultiPickDeterminism.
func renderCategory(reg *registry.Registry, cat *registry.Category, v *registry.Value) ([][]byte, error) {
	if v == nil {
		return nil, nil // unset
	}

	switch cat.Pick {
	case registry.PickSingle:
		if v.Single == nil || *v.Single == "" {
			return nil, nil
		}
		b, err := reg.OptionBlock(cat, *v.Single)
		if err != nil {
			return nil, err
		}
		return [][]byte{b}, nil

	case registry.PickMulti:
		if v.Multi == nil || len(*v.Multi) == 0 {
			return nil, nil
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
