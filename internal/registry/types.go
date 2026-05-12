package registry

import "io/fs"

// LanguageCategoryID is the canonical id of the language category —
// the only category in the schema that carries sub-categories. The
// string was previously hardcoded across the CLI and registry
// packages; centralising it here means a future rename is a single
// edit, and the references are grep-able by name.
const LanguageCategoryID = "language"

// Pick is the cardinality of a category — either "single" (choose one
// option) or "multi" (choose any subset, including none). Only these
// two values are valid in an `_index.yaml`.
type Pick string

const (
	// PickSingle marks a single-pick category.
	PickSingle Pick = "single"
	// PickMulti marks a multi-pick category.
	PickMulti Pick = "multi"
)

// Option is one selectable choice inside a Category, derived from a
// row of `options:` in an `_index.yaml`. ID is the kebab identifier;
// File is the basename of the `.md` block file in the same folder.
type Option struct {
	ID          string
	DisplayName string
	Description string
	File        string
}

// Category is one `_index.yaml` worth of selection state. Path is the
// embedded-FS path of the category folder (e.g.
// `templates/10-language` or `templates/10-language/python/20-logging`).
// For Pick == PickSingle, Default (if non-empty) is an option ID; for
// Pick == PickMulti, Default is a list of option IDs. The empty string
// / nil Default means "no default".
//
// IsContainer marks the category as a *container* category in the
// generalised three-level-capable schema (CONTENT-STYLE.md §2.3, §2.4):
// a container's options point at subdirectories rather than `.md`
// files, and each option's subtree holds further categories. The
// language category is the canonical example today (its options point
// at `<lang>/base.md`); after Dispatch 4 the per-language `10-type/`
// folder is the second container. A non-container category is a
// *leaf* — its options point at flat `.md` files in the same folder.
//
// Subcategories is populated only when IsContainer is true: it maps a
// container option ID to that option's ordered list of sub-categories
// (each itself a Category that may, recursively, be another container).
// Non-container categories carry a nil Subcategories.
type Category struct {
	Path          string
	ID            string // last segment of Path with the NN- prefix stripped
	DisplayName   string
	Pick          Pick
	Required      bool
	IsContainer   bool                   // true when options point at subdirectories with their own _index.yaml trees
	DefaultSingle string                 // populated when Pick == PickSingle and a default is set; empty string = no default
	DefaultMulti  []string               // populated when Pick == PickMulti and a default is set; nil or empty slice = no default
	Options       []Option               // declaration order from `_index.yaml`
	Subcategories map[string][]*Category // populated when IsContainer is true; keyed by chosen option ID
}

// Registry is the root view of the templates filesystem in render
// order (`10-language`, `20-infra`, …). Construct one via Load or
// LoadFS — these set the unexported fsys reference that OptionBlock
// reads from. The fsys field retains the source filesystem so
// callers — notably `internal/render` and `em-dee show` — can read
// individual block (`.md`) files via OptionBlock without re-loading
// the manifest. We keep fsys non-exported rather than re-plumbing
// the fs.FS through every call site; the boundary stays in the
// registry package.
type Registry struct {
	Categories []*Category

	fsys fs.FS
}

// Value is the per-category selection cell used inside Picks. Exactly
// one of Single or Multi is non-nil, matching the owning category's
// Pick. Nil *Value (or a Value with both fields nil) means "unset" —
// ApplyDefaults will fill it from the registry's default. A non-nil
// *Value carrying option ids means "the user chose these".
//
// Dispatch 1 collapsed the previous tri-state (unset / explicit-none /
// chosen) to a binary at the API level: callers can no longer assert
// "the user explicitly chose nothing." The internal resolver still
// constructs a non-nil-pointer-to-empty cell when it sees an empty
// input, and the renderer still treats that cell as "emit no block",
// but neither shape is part of the contract exposed to flag, form, or
// future config-file consumers — they always express selections as
// either "absent from the map" or "this list of option ids".
type Value struct {
	Single *string
	Multi  *[]string
}

// NewSingle constructs a Value holding a single-pick selection.
func NewSingle(id string) *Value {
	v := id
	return &Value{Single: &v}
}

// NewMulti constructs a Value holding a multi-pick selection. A nil or
// empty input produces a non-nil pointer to an empty slice; the
// renderer treats this the same as "unset" (emits no block), and the
// distinction is no longer load-bearing in the public API.
func NewMulti(ids []string) *Value {
	if ids == nil {
		ids = []string{}
	}
	return &Value{Multi: &ids}
}

// Picks is a user's selection across all categories. The map key is
// the dotted reference used everywhere in em-dee: top-level
// categories use just their ID (e.g. `language`, `infra`); language-
// nested categories use `<lang-id>.<category-id>` (e.g.
// `python.logging`). This keeps a single uniform shape across single-
// and multi-pick without per-category code in Picks. (Tradeoff
// surfaced in the Task 1.1 commit body — chosen for data-driven
// uniformity per CLAUDE.md principle 2.)
type Picks struct {
	Values map[string]*Value
}

// NewPicks returns an empty Picks ready to receive values.
func NewPicks() Picks {
	return Picks{Values: map[string]*Value{}}
}

// FindCategory returns the top-level Category with the given id, or
// nil if no such category exists. Centralised here so CLI consumers
// (e.g. internal/cli/show.go's reference resolver) don't have to
// duplicate the lookup loop locally.
func (r *Registry) FindCategory(id string) *Category {
	if r == nil {
		return nil
	}
	for _, cat := range r.Categories {
		if cat.ID == id {
			return cat
		}
	}
	return nil
}

// HasOption reports whether the category has an option with the
// given id. Centralised here so CLI consumers (e.g. show.go) and the
// resolver share one implementation.
func (c *Category) HasOption(id string) bool {
	if c == nil {
		return false
	}
	for _, opt := range c.Options {
		if opt.ID == id {
			return true
		}
	}
	return false
}
