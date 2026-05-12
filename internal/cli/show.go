package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trackness/em-dee/internal/registry"
)

// newShowCmd builds `em-dee show <ref>` for the dotted-ref grammar.
//
// Reference forms (resolved left-to-right against the registry, not by
// string-manipulating filesystem paths):
//
//   - `<container>.<opt>` → a container category's option block. The
//     canonical example is `language.<lang>`, which reads the
//     `<lang>/base.md` block.
//   - `<lang>.<cat>.<opt>` → language subcategory option block.
//   - `<lang>.<type>.<cat>.<opt>` (arbitrary depth) → resolves through
//     a chain of container options, eliding container category ids
//     per CONTENT-STYLE.md §2.3. For example `python.cli.framework.typer`
//     traverses python → 10-type (container, opt=cli) → 10-framework
//     (leaf, opt=typer) once that subtree exists.
//   - `<cat>.<opt>` → top-level category option block.
//
// Disambiguation rule (per plan Task 3.3): the resolver walks the
// dotted segments left-to-right. At each step it matches the next
// segment against either a *category id* in the current scope (leaf
// or container) or, when the current category is a container, an
// *option id* (which descends into the container's subtree, eliding
// the container's own id). The validator guarantees ids are
// non-colliding (option ids unique within a category, language option
// ids non-colliding with top-level category ids), so the walk is
// unambiguous.
func newShowCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <ref>",
		Short: "Print one block's markdown content to stdout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := resolveRegistry(opts)
			if err != nil {
				return err
			}
			content, err := resolveShowRef(reg, args[0])
			if err != nil {
				return err
			}
			// Block content already ends with a newline by convention
			// (markdown blocks are POSIX text files); write as-is.
			_, err = cmd.OutOrStdout().Write(content)
			return err
		},
	}
	return cmd
}

// resolveShowRef walks the registry following the dotted-ref grammar
// and returns the raw bytes of the resolved option's `.md` block.
// Errors include the full input ref so the user sees what failed.
//
// Algorithm: walk dotted segments left-to-right. The first segment is
// a category id within the top-level scope; subsequent segments
// alternate "option id of the current category" and "category id
// within the chosen subtree". When the current category is a
// container, choosing an option means descending into that option's
// subtree (the container's own id is elided per CONTENT-STYLE.md
// §2.3). The terminus is always an option id of a leaf category,
// whose `.md` block is returned.
//
// A two-segment ref where the first segment is a container — most
// commonly `language.<lang>` — resolves to the container option's
// scope base.md (e.g. `<lang>/base.md`). This is the "show me the
// scope-level discipline block" form.
func resolveShowRef(reg *registry.Registry, ref string) ([]byte, error) {
	segs := strings.Split(ref, ".")
	if len(segs) < 2 {
		return nil, fmt.Errorf("show %s: reference must have at least two dotted segments (e.g. language.python or infra.docker)", ref)
	}

	// First-segment disambiguation: the segment is either a top-level
	// category id OR an option of a top-level container category whose
	// own id has been elided (per CONTENT-STYLE.md §2.3 the container
	// is silent in the namespace; only the chosen option appears).
	// The most common elision is the `language` container: the user
	// writes `python.logging.loguru` rather than `language.python.logging.loguru`.
	//
	// Category-id match wins when both are possible — the validator
	// guarantees there's no collision (a language option id may not
	// equal a top-level category id), so the "both possible" case is
	// excluded at load time.
	if cat := reg.FindCategory(segs[0]); cat != nil {
		return walkShowRef(reg, cat, segs[1:], ref)
	}

	// Try every top-level container in declaration order. The first
	// container whose options include segs[0] owns the ref.
	for _, cat := range reg.Categories {
		if !cat.IsContainer {
			continue
		}
		if !cat.HasOption(segs[0]) {
			continue
		}
		// Treat segs[0] as the chosen option, then descend.
		return walkShowRefViaContainer(reg, cat, segs, ref)
	}

	return nil, fmt.Errorf("show %s: no category %q in registry", ref, segs[0])
}

// walkShowRefViaContainer handles the elided-container shape: the
// caller has identified `cat` as a top-level container whose option
// is `segs[0]`. Two outcomes:
//
//   - One segment total → return the option's scope base.md (same as
//     `<container>.<opt>`); this case is unusual because callers can
//     just write `language.python` instead.
//   - Two or more segments → descend into the option's subtree and
//     resolve the remainder via walkShowRef.
func walkShowRefViaContainer(reg *registry.Registry, cat *registry.Category, segs []string, ref string) ([]byte, error) {
	optID := segs[0]
	if len(segs) == 1 {
		return reg.OptionBlock(cat, optID)
	}
	subs := cat.Subcategories[optID]
	subID := segs[1]
	var sub *registry.Category
	for _, s := range subs {
		if s.ID == subID {
			sub = s
			break
		}
	}
	if sub == nil {
		return nil, fmt.Errorf("show %s: %q has no sub-category %q", ref, optID, subID)
	}
	return walkShowRef(reg, sub, segs[2:], ref)
}

// walkShowRef consumes the remaining segments against a current
// category. The recursion has two terminating shapes:
//
//   - One segment left, current category is a leaf → resolve as that
//     category's option id, return its block.
//   - One segment left, current category is a container → resolve as
//     a container option id (the option's `file:` must point at a
//     scope-base block; otherwise the ref is incomplete).
//
// And one descending shape:
//
//   - Two or more segments left, current category is a container →
//     match the next segment as a container option id, then descend
//     into that option's subtree. The following segment names a
//     category within the subtree; that's the new current category
//     and the recursion continues.
func walkShowRef(reg *registry.Registry, cat *registry.Category, remaining []string, ref string) ([]byte, error) {
	if len(remaining) == 0 {
		return nil, fmt.Errorf("show %s: reference is incomplete (no option after category %q)", ref, cat.ID)
	}

	if !cat.IsContainer {
		// Leaf: exactly one segment must remain, naming the option.
		if len(remaining) != 1 {
			return nil, fmt.Errorf("show %s: leaf category %q expects exactly one option segment, got %d extra", ref, cat.ID, len(remaining)-1)
		}
		optID := remaining[0]
		if !cat.HasOption(optID) {
			return nil, fmt.Errorf("show %s: option %q not found in category %q", ref, optID, cat.ID)
		}
		return reg.OptionBlock(cat, optID)
	}

	// Container: the next segment is an option id.
	optID := remaining[0]
	if !cat.HasOption(optID) {
		return nil, fmt.Errorf("show %s: option %q not found in container category %q", ref, optID, cat.ID)
	}

	// One segment left → the user wants the option's scope-base block
	// (e.g. `language.python` → `python/base.md`).
	if len(remaining) == 1 {
		return reg.OptionBlock(cat, optID)
	}

	// More segments → descend into the chosen option's subtree.
	subs := cat.Subcategories[optID]
	subID := remaining[1]
	var sub *registry.Category
	for _, s := range subs {
		if s.ID == subID {
			sub = s
			break
		}
	}
	if sub == nil {
		return nil, fmt.Errorf("show %s: container %q option %q has no sub-category %q", ref, cat.ID, optID, subID)
	}
	return walkShowRef(reg, sub, remaining[2:], ref)
}
