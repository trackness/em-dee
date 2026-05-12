package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trackness/em-dee/internal/registry"
)

// listCategory is the JSON-side view of one registry.Category. It is
// the documented machine-readable shape for `em-dee list --json` —
// downstream tooling can rely on the field names and tags as a stable
// contract. Add fields only at the tail.
//
// `pick` is encoded as the string form ("single" / "multi") rather
// than the typed registry.Pick because JSON consumers don't share our
// types. `default_single` / `default_multi` are mutually exclusive per
// the category's pick — both are emitted (empty when unset) so the
// shape is uniform regardless of pick mode.
type listCategory struct {
	ID            string       `json:"id"`
	DisplayName   string       `json:"display_name"`
	Pick          string       `json:"pick"`
	Required      bool         `json:"required"`
	DefaultSingle string       `json:"default_single,omitempty"`
	DefaultMulti  []string     `json:"default_multi,omitempty"`
	Options       []listOption `json:"options"`
}

// listOption mirrors registry.Option for JSON output. The
// Subcategories field is non-empty only for language options.
type listOption struct {
	ID            string         `json:"id"`
	DisplayName   string         `json:"display_name"`
	Description   string         `json:"description,omitempty"`
	Subcategories []listCategory `json:"subcategories,omitempty"`
}

// listPayload is the top-level JSON document.
type listPayload struct {
	Categories []listCategory `json:"categories"`
}

// newListCmd builds `em-dee list`. Human form is a tree; `--json`
// emits the documented machine-readable shape.
func newListCmd(opts Options) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every category and option in the registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := resolveRegistry(opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writeListJSON(cmd.OutOrStdout(), reg)
			}
			return writeListHuman(cmd.OutOrStdout(), reg)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// resolveRegistry returns the test-injected Registry if set, otherwise
// calls the production loader. Centralised so every subcommand uses
// the same lookup.
func resolveRegistry(opts Options) (*registry.Registry, error) {
	if opts.Registry != nil {
		return opts.Registry, nil
	}
	return registry.Load()
}

// writeListHuman renders the human-readable category tree. Indentation
// is two spaces per level; option lines carry "(default)" when the
// option id matches the category's default.
func writeListHuman(w io.Writer, reg *registry.Registry) error {
	for _, cat := range reg.Categories {
		if err := writeCategoryHuman(w, cat, 0); err != nil {
			return err
		}
	}
	return nil
}

func writeCategoryHuman(w io.Writer, cat *registry.Category, depth int) error {
	indent := strings.Repeat(" ", depth)
	req := ""
	if cat.Required {
		req = " (required)"
	}
	if _, err := fmt.Fprintf(w, "%s%s [%s]%s\n", indent, cat.ID, cat.Pick, req); err != nil {
		return err
	}
	for _, opt := range cat.Options {
		marker := ""
		switch cat.Pick {
		case registry.PickSingle:
			if opt.ID == cat.DefaultSingle {
				marker = " (default)"
			}
		case registry.PickMulti:
			for _, d := range cat.DefaultMulti {
				if d == opt.ID {
					marker = " (default)"
					break
				}
			}
		}
		if _, err := fmt.Fprintf(w, "%s  - %s%s\n", indent, opt.ID, marker); err != nil {
			return err
		}
		// Container option carries a nested subtree of further
		// categories. Recurse so the tree view renders the full
		// catalog at any depth.
		//
		// Order: the registry walk yields subs in NN-prefix folder
		// order (see registry.listCategoryDirs), which IS the
		// documented render order. Sorting by id here corrupts that
		// invariant — alphabetical-by-id only happens to match
		// NN-prefix order when option ids are kebab-equivalent. Iterate
		// the slice directly.
		if subs, ok := cat.Subcategories[opt.ID]; ok {
			for _, sub := range subs {
				if err := writeCategoryHuman(w, sub, depth+2); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// writeListJSON serialises the registry into the documented shape and
// writes it as a trailing-newline-terminated JSON document.
func writeListJSON(w io.Writer, reg *registry.Registry) error {
	payload := listPayload{}
	for _, cat := range reg.Categories {
		payload.Categories = append(payload.Categories, categoryToJSON(cat))
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(payload)
}

func categoryToJSON(cat *registry.Category) listCategory {
	out := listCategory{
		ID:            cat.ID,
		DisplayName:   cat.DisplayName,
		Pick:          string(cat.Pick),
		Required:      cat.Required,
		DefaultSingle: cat.DefaultSingle,
		DefaultMulti:  cat.DefaultMulti,
	}
	for _, opt := range cat.Options {
		jopt := listOption{
			ID:          opt.ID,
			DisplayName: opt.DisplayName,
			Description: opt.Description,
		}
		// Language category attaches subcategories per option.
		if subs, ok := cat.Subcategories[opt.ID]; ok {
			for _, sub := range subs {
				jopt.Subcategories = append(jopt.Subcategories, categoryToJSON(sub))
			}
		}
		out.Options = append(out.Options, jopt)
	}
	return out
}
