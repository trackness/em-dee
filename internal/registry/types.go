package registry

import "io/fs"

// LanguageCategoryID is the canonical id of the language category —
// the only category in the schema that carries sub-categories. The
// string was previously hardcoded across the CLI and registry
// packages; centralising it here means a future rename (unlikely in
// v1, but the spec doesn't forbid one) is a single edit, and the
// references are grep-able by name.
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
// Subcategories is populated only for the top-level `language`
// category: it maps a language option ID to that language's ordered
// list of sub-categories (10-framework, 20-logging, …). All other
// categories carry a nil Subcategories.
type Category struct {
	Path          string
	ID            string // last segment of Path with the NN- prefix stripped
	DisplayName   string
	Pick          Pick
	Required      bool
	DefaultSingle string                 // populated when Pick == PickSingle and a default is set; empty string = no default
	DefaultMulti  []string               // populated when Pick == PickMulti and a default is set; nil or empty slice = no default
	Options       []Option               // declaration order from `_index.yaml`
	Subcategories map[string][]*Category // language-only: keyed by language option ID
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

// Value is the tri-state per-category selection cell used inside
// Picks. Exactly one of Single or Multi is non-nil, matching the
// owning category's Pick. Nil pointer = "unset" (default-eligible);
// non-nil pointer to an empty value = "explicit none" (default
// suppressed); non-nil pointer to a non-empty value = "chosen".
type Value struct {
	Single *string
	Multi  *[]string
}

// NewSingle constructs a Value holding a chosen single-pick option.
// Pass "" to record an explicit-none.
func NewSingle(id string) *Value {
	v := id
	return &Value{Single: &v}
}

// NewMulti constructs a Value holding a chosen multi-pick selection.
// Pass nil or an empty slice to record an explicit-none.
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
