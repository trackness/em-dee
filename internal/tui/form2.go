// form2.go owns the construction of huh form 2 — the rest of the
// picks plus the final confirm group per spec §5.2 step 2/3. Form 2
// is built *after* form 1 returns so the language is known and the
// group set is concrete; this avoids huh's dynamic-fields corner
// cases per spec §5.2 paragraph 0.

package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/trackness/em-dee/internal/registry"
)

// SecondaryForm holds form 2 plus the bound variables for every
// field. Each category's bound storage is exposed so the caller can
// translate the final state into a registry.Picks after .Run() exits.
// Storing the bindings on the struct (rather than inside opaque huh
// state) is what makes form 2 testable without invoking .Run().
type SecondaryForm struct {
	Form *huh.Form

	// langID is the language chosen in form 1; used both to scope the
	// language-nested categories and to namespace the resulting Picks
	// keys (`<langID>.<subID>`).
	langID string

	// singles maps the selection-key (top-level cat id or
	// `lang.sub` dotted form) to the bound *string. After Run() the
	// pointer holds the chosen option id, or "" if the user accepted
	// no-default explicit-empty.
	singles map[string]*string

	// multis maps the selection-key to the bound *[]string. After
	// Run() the slice holds the chosen option ids; an empty slice
	// means "explicit none."
	multis map[string]*[]string

	// confirmed is the bound *bool for the final confirm group.
	confirmed *bool
}

// BuildSecondaryForm constructs form 2 — the chosen language's
// sub-categories plus the cross-cutting top-level categories, then
// a confirm group summarising what will be rendered.
//
// Defaults flow from `initial` (typically the result of ApplyDefaults
// after seeding form 1's language pick). Per spec §5.2 paragraph 2:
// defaults pre-populate bound variables before .Run() so Enter
// accepts the default. Optional categories without a default present
// an empty selection state.
//
// Construction is split from Run so tests can drive it without a TTY.
func BuildSecondaryForm(reg *registry.Registry, langID string, initial registry.Picks) (*SecondaryForm, error) {
	if reg == nil {
		return nil, errors.New("BuildSecondaryForm: nil registry")
	}
	if langID == "" {
		return nil, errors.New("BuildSecondaryForm: empty langID")
	}

	// Find the language category so we can pull its sub-categories
	// for the chosen language id. A missing language category (e.g.
	// empty registry) is a hard error here — form 1 should have
	// rejected this case already.
	var langCat *registry.Category
	for _, c := range reg.Categories {
		if c.ID == registry.LanguageCategoryID {
			langCat = c
			break
		}
	}
	if langCat == nil {
		return nil, errors.New("BuildSecondaryForm: registry has no language category")
	}

	sf := &SecondaryForm{
		langID:  langID,
		singles: map[string]*string{},
		multis:  map[string]*[]string{},
	}

	var groups []*huh.Group

	// First: the chosen language's sub-categories (10-framework,
	// 20-logging, etc.) in manifest order.
	subs := langCat.Subcategories[langID]
	for _, sub := range subs {
		key := langID + "." + sub.ID
		g, err := buildCategoryGroup(sub, key, initial, sf)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}

	// Then: the cross-cutting categories (every top-level category
	// other than language) in manifest order.
	for _, cat := range reg.Categories {
		if cat.ID == registry.LanguageCategoryID {
			continue
		}
		g, err := buildCategoryGroup(cat, cat.ID, initial, sf)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}

	// Finally: the confirm group. The description is built lazily
	// (via DescriptionFunc) so it reflects the user's selections as
	// they edit prior groups — important for paginated flows where
	// the user can walk back and tweak.
	confirmed := new(bool)
	*confirmed = true // pre-seed Yes so Enter on the default commits
	sf.confirmed = confirmed
	confirm := huh.NewConfirm().
		Title("Render CLAUDE.md?").
		DescriptionFunc(func() string {
			return sf.summarise(reg)
		}, sf).
		Affirmative("Yes, write it").
		Negative("Cancel").
		Value(confirmed)
	groups = append(groups, huh.NewGroup(confirm))

	sf.Form = huh.NewForm(groups...)
	return sf, nil
}

// buildCategoryGroup constructs one huh.Group for a single category.
// The group has one field — Select for single-pick, MultiSelect for
// multi-pick. The bound variable is registered on `sf` keyed by the
// selection key (e.g. `python.logging` or `infra`), with the default
// pre-populated from `initial` so Enter accepts the default per spec
// §5.2 paragraph 2.
func buildCategoryGroup(cat *registry.Category, key string, initial registry.Picks, sf *SecondaryForm) (*huh.Group, error) {
	switch cat.Pick {
	case registry.PickSingle:
		bound := new(string)
		if v, ok := initial.Values[key]; ok && v != nil && v.Single != nil {
			*bound = *v.Single
		}
		sf.singles[key] = bound

		opts := make([]huh.Option[string], 0, len(cat.Options))
		for _, o := range cat.Options {
			opts = append(opts, huh.NewOption(o.DisplayName, o.ID))
		}
		sel := huh.NewSelect[string]().
			Title(cat.DisplayName).
			Options(opts...).
			Value(bound)
		return huh.NewGroup(sel), nil

	case registry.PickMulti:
		bound := new([]string)
		if v, ok := initial.Values[key]; ok && v != nil && v.Multi != nil {
			cp := make([]string, len(*v.Multi))
			copy(cp, *v.Multi)
			*bound = cp
		}
		sf.multis[key] = bound

		opts := make([]huh.Option[string], 0, len(cat.Options))
		for _, o := range cat.Options {
			opts = append(opts, huh.NewOption(o.DisplayName, o.ID))
		}
		ms := huh.NewMultiSelect[string]().
			Title(cat.DisplayName).
			Options(opts...).
			Value(bound)
		return huh.NewGroup(ms), nil

	default:
		return nil, fmt.Errorf("category %q: unsupported pick=%q", cat.ID, cat.Pick)
	}
}

// summarise produces the confirm-group description: a comma-separated
// list of the blocks that will be rendered, in render order (§4.4).
// Reads from the current bound variables so the summary tracks the
// user's edits while form 2 is open.
func (sf *SecondaryForm) summarise(reg *registry.Registry) string {
	picks := sf.Picks()
	var parts []string

	// Language base.
	if sf.langID != "" {
		parts = append(parts, "language/"+sf.langID)
	}

	// Walk in render order for stability.
	for _, cat := range reg.Categories {
		if cat.ID == registry.LanguageCategoryID {
			// Language subtree: only the chosen language's subs.
			for _, sub := range cat.Subcategories[sf.langID] {
				key := sf.langID + "." + sub.ID
				v := picks.Values[key]
				if v == nil {
					continue
				}
				switch sub.Pick {
				case registry.PickSingle:
					if v.Single != nil && *v.Single != "" {
						parts = append(parts, sub.ID+"/"+*v.Single)
					}
				case registry.PickMulti:
					if v.Multi != nil {
						for _, id := range *v.Multi {
							parts = append(parts, sub.ID+"/"+id)
						}
					}
				}
			}
			continue
		}
		v := picks.Values[cat.ID]
		if v == nil {
			continue
		}
		switch cat.Pick {
		case registry.PickSingle:
			if v.Single != nil && *v.Single != "" {
				parts = append(parts, cat.ID+"/"+*v.Single)
			}
		case registry.PickMulti:
			if v.Multi != nil {
				for _, id := range *v.Multi {
					parts = append(parts, cat.ID+"/"+id)
				}
			}
		}
	}

	if len(parts) == 0 {
		return "(no blocks selected)"
	}
	return "blocks: " + strings.Join(parts, ", ")
}

// Picks translates the bound variables back into a registry.Picks.
// Called after Run() returns (or by summarise for the live preview).
//
// huh v2 quirk worth knowing: Select.Value(ptr) calls Accessor which
// calls updateValue, which writes the highlighted (initially first)
// option's value into the bound pointer at construction time. So an
// "unseeded" single-pick field comes back with the first option's
// id rather than an empty string. That's huh v2 behaviour, not ours
// — the UX still requires Enter to commit, so the user sees the
// form before any value is finalised. We pass through whatever's in
// the bound; ApplyDefaults gets a second chance only for keys we
// genuinely omit (empty single-pick or empty multi-pick).
//
// MultiSelect doesn't have this quirk — an empty bound stays empty.
// So an untouched MultiSelect comes back as nil/empty here and is
// omitted from Picks, letting ApplyDefaults fill in the registry
// default. This matches the spec §5.2 "optional categories without
// a default present an empty selection state" intent for multi-pick,
// and approximates it as closely as huh v2 allows for single-pick.
//
// Tradeoff vs. the flag layer: the flag layer treats `--infra=` as
// explicit-empty per spec §5.1, but the interactive flow has no
// natural keystroke for "explicit empty" — leaving a MultiSelect
// untouched and one that the user deliberately cleared look the
// same to huh. Forcing them apart would mean adding a sentinel
// option, which complicates the UI for no v1 benefit.
func (sf *SecondaryForm) Picks() registry.Picks {
	picks := registry.NewPicks()
	picks.Values[registry.LanguageCategoryID] = registry.NewSingle(sf.langID)

	for key, v := range sf.singles {
		if v == nil || *v == "" {
			continue
		}
		picks.Values[key] = registry.NewSingle(*v)
	}
	for key, v := range sf.multis {
		if v == nil || len(*v) == 0 {
			continue
		}
		cp := make([]string, len(*v))
		copy(cp, *v)
		picks.Values[key] = registry.NewMulti(cp)
	}
	return picks
}

// Confirmed reports the final confirm-group answer. Only meaningful
// after Run() has returned without error.
func (sf *SecondaryForm) Confirmed() bool {
	if sf.confirmed == nil {
		return false
	}
	return *sf.confirmed
}

// RunSecondaryForm drives form 2 end-to-end: build, run, translate
// back to Picks. If the user picked No on the confirm group, returns
// ErrCancelled so callers don't need to distinguish "user said no"
// from "user hit Ctrl-C" — both mean "don't write the file."
func RunSecondaryForm(reg *registry.Registry, langID string, initial registry.Picks) (registry.Picks, error) {
	sf, err := BuildSecondaryForm(reg, langID, initial)
	if err != nil {
		return registry.Picks{}, err
	}
	if err := sf.Form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return registry.Picks{}, ErrCancelled
		}
		return registry.Picks{}, fmt.Errorf("run secondary form: %w", err)
	}
	if !sf.Confirmed() {
		return registry.Picks{}, ErrCancelled
	}
	return sf.Picks(), nil
}
