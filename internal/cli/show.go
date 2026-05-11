package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trackness/em-dee/internal/registry"
)

// newShowCmd builds `em-dee show <ref>` per spec §5.1's dotted-ref
// grammar.
//
// Reference forms (resolved left-to-right against the registry, not by
// string-manipulating filesystem paths):
//
//   - `language.<lang>` → reads the language option's File (the
//     `<lang>/base.md` block).
//   - `<lang>.<cat>.<opt>` → language subcategory option block.
//   - `<cat>.<opt>` → top-level category option block.
//
// Disambiguation rule (per plan Task 3.3): if the first segment is a
// known language id, treat the ref as the language-nested form;
// otherwise treat the first segment as a top-level category id. v1's
// catalog has no collision between language ids (`go`, `python`,
// `typescript-node`, `rust`) and top-level category ids (`infra`,
// `ci`, `tooling`), so this rule is unambiguous in practice.
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
// Category/option lookup uses registry.Registry.FindCategory and
// registry.Category.HasOption — the helpers are shared with the
// resolver inside the registry package, so the show form and the
// flag-derived selection form can't drift.
func resolveShowRef(reg *registry.Registry, ref string) ([]byte, error) {
	segs := strings.Split(ref, ".")
	if len(segs) < 2 {
		return nil, fmt.Errorf("show %s: reference must have at least two dotted segments (e.g. language.python or infra.docker)", ref)
	}

	// Form 1: `language.<lang>` — first segment is the literal word
	// "language" and the registry has a category with id "language".
	if segs[0] == "language" && len(segs) == 2 {
		langCat := reg.FindCategory("language")
		if langCat == nil {
			return nil, fmt.Errorf("show %s: no `language` category in registry", ref)
		}
		if !langCat.HasOption(segs[1]) {
			return nil, fmt.Errorf("show %s: language %q not found", ref, segs[1])
		}
		return reg.OptionBlock(langCat, segs[1])
	}

	// Form 2: first segment is a known language id. Walk
	// language.Subcategories[<lang>] for the second-segment category,
	// then resolve the third-segment option.
	langCat := reg.FindCategory("language")
	if langCat != nil && langCat.HasOption(segs[0]) {
		if len(segs) != 3 {
			return nil, fmt.Errorf("show %s: language-nested reference must be <lang>.<category>.<option>", ref)
		}
		subs := langCat.Subcategories[segs[0]]
		var sub *registry.Category
		for _, s := range subs {
			if s.ID == segs[1] {
				sub = s
				break
			}
		}
		if sub == nil {
			return nil, fmt.Errorf("show %s: language %q has no sub-category %q", ref, segs[0], segs[1])
		}
		if !sub.HasOption(segs[2]) {
			return nil, fmt.Errorf("show %s: option %q not found in %s.%s", ref, segs[2], segs[0], segs[1])
		}
		return reg.OptionBlock(sub, segs[2])
	}

	// Form 3: top-level `<cat>.<opt>` (exactly two segments).
	if len(segs) == 2 {
		cat := reg.FindCategory(segs[0])
		if cat == nil {
			return nil, fmt.Errorf("show %s: no category %q in registry", ref, segs[0])
		}
		if !cat.HasOption(segs[1]) {
			return nil, fmt.Errorf("show %s: option %q not found in category %q", ref, segs[1], segs[0])
		}
		return reg.OptionBlock(cat, segs[1])
	}

	return nil, fmt.Errorf("show %s: reference does not match any known form (language.<lang>, <lang>.<cat>.<opt>, or <cat>.<opt>)", ref)
}
