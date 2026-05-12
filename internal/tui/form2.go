// form2.go owns the post-language interactive flow. Dispatch 3 splits
// the old single form-2 into two distinct phases to accommodate the
// three-level container schema introduced by Dispatch 2:
//
//   - Phase 2 (optional): the *type form* — one huh.Select over the
//     language's container sub-category's options, when one exists.
//     Today's two-level production catalog has none, so this phase is
//     skipped; the three-level test fixtures do, so this phase fires.
//   - Phase 3: the *scope form* — one huh.Group per leaf category in
//     the user-visible scope (language's non-container leaves + the
//     chosen container option's leaves + cross-cutting top-level
//     categories), plus a confirm group.
//
// The scope form's selection-key namespace tracks the registry's:
// language-scope leaves are `<lang>.<sub>`; container-option-scope
// leaves are `<lang>.<chosen-type>.<sub>` (the container's own id
// elided per CONTENT-STYLE.md §2.3). The resolver Dispatch 2 added
// already accepts these keys; the form just has to produce them.

package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/trackness/em-dee/internal/registry"
)

// TypeForm wraps the phase-2 huh form (the container-option Select)
// plus the bound storage. Construction is split from Run so unit tests
// can verify the bindings without a TTY.
type TypeForm struct {
	Form *huh.Form

	// langID + containerID identify the container category being picked.
	// The resulting selection key is `<langID>.<containerID>`.
	langID      string
	containerID string

	// chosen is the bound *string the huh.Select writes into.
	chosen *string
}

// ScopeForm wraps the phase-3 huh form (all remaining categories +
// confirm) plus the bound storage. Storing the bindings on the struct
// keeps the form testable without invoking .Run().
type ScopeForm struct {
	Form *huh.Form

	// langID is the language chosen in form 1; namespaces every
	// language-scope and container-option-scope selection key.
	langID string

	// typeID is the optional container-option id (empty when the
	// registry has no container category under the language). When
	// non-empty, container-option-scope leaves are keyed
	// `<langID>.<typeID>.<sub.ID>`.
	typeID string

	// containerID is the id of the container category whose option was
	// picked in phase 2 (empty when no container exists). Used by
	// summarise() and Picks() to surface the type pick alongside the
	// scope-form leaves so the user sees the full selection set on the
	// confirm screen.
	containerID string

	// singles maps a selection-key to the bound *string for a single-
	// pick leaf. Keys are `<lang>.<sub>` for language-scope leaves and
	// `<lang>.<type>.<sub>` for container-option-scope leaves and the
	// plain `<cat>` for cross-cutting top-level categories.
	singles map[string]*string

	// multis maps a selection-key to the bound *[]string for a
	// multi-pick leaf. Same keying as singles.
	multis map[string]*[]string

	// confirmed is the bound *bool for the final confirm group.
	confirmed *bool
}

// FindContainerSub returns the container sub-category under the given
// language (if any). A language has at most one container in v1 (the
// `10-type/` slot — CONTENT-STYLE.md §2.3); the schema doesn't enforce
// "at most one" mechanically, but the form treats only the first as
// the type axis. A second container under the same language is a
// schema error that the validator would surface separately.
//
// Returns nil when no container sub-category exists — the current
// two-level production catalog and the `valid` test fixture both hit
// this path. Exposed so the CLI's runInteractive can detect the
// container case before deciding whether to drive the type form.
func FindContainerSub(reg *registry.Registry, langID string) *registry.Category {
	if reg == nil || langID == "" {
		return nil
	}
	langCat := reg.FindCategory(registry.LanguageCategoryID)
	if langCat == nil {
		return nil
	}
	for _, sub := range langCat.Subcategories[langID] {
		if sub.IsContainer {
			return sub
		}
	}
	return nil
}

// BuildTypeForm constructs phase 2 — the container-option Select for
// the language's container sub-category. Returns nil + nil error when
// the language has no container sub-category (caller skips the phase
// and proceeds straight to BuildScopeForm with empty typeID). Returns
// nil + non-nil error for a malformed registry or argument.
//
// `initial` seeds the bound pointer so a `--use-defaults`-flavoured
// flow lands on the existing default; the seed key is
// `<langID>.<containerID>`.
//
// `useDefaults` interaction: this builder doesn't itself consume the
// flag (no UX bifurcation at construction time). Callers decide
// whether to run the form. The lean rule (documented at the
// runInteractive call site in internal/cli/generate.go): if
// useDefaults is set AND the container has a default, skip running
// this form and let ApplyDefaults fill the cell. If useDefaults is
// set AND the container has no default, run this form anyway — the
// language-conditional type pick has no sensible silent default and
// asking once is cheaper than guessing wrong silently.
func BuildTypeForm(reg *registry.Registry, langID string, initial registry.Picks) (*TypeForm, error) {
	if reg == nil {
		return nil, errors.New("BuildTypeForm: nil registry")
	}
	if langID == "" {
		return nil, errors.New("BuildTypeForm: empty langID")
	}
	container := FindContainerSub(reg, langID)
	if container == nil {
		// Not an error — the caller is expected to use this to detect
		// "no type axis exists for this language" by checking the
		// returned form for nil. We surface the no-container case as
		// (nil, nil) so the caller's branch is readable: `if tf, err :=
		// BuildTypeForm(...); err == nil && tf == nil { skip }`.
		return nil, nil
	}
	if container.Pick != registry.PickSingle {
		// Defensive: the validator enforces container.Pick == single,
		// but the form-side cost of failing soft here is exactly one
		// confusing UX. Surface loudly.
		return nil, fmt.Errorf("BuildTypeForm: container %q is not single-pick (got %q)", container.ID, container.Pick)
	}

	tf := &TypeForm{
		langID:      langID,
		containerID: container.ID,
		chosen:      new(string),
	}

	// Seed from initial if present. Key shape mirrors the registry's
	// key index (langID.containerID).
	key := langID + "." + container.ID
	if v, ok := initial.Values[key]; ok && v != nil && v.Single != nil {
		*tf.chosen = *v.Single
	}

	opts := make([]huh.Option[string], 0, len(container.Options))
	for _, o := range container.Options {
		opts = append(opts, huh.NewOption(o.DisplayName, o.ID))
	}
	sel := huh.NewSelect[string]().
		Title(container.DisplayName).
		Options(opts...).
		Value(tf.chosen)

	tf.Form = huh.NewForm(huh.NewGroup(sel))
	return tf, nil
}

// ContainerID reports the id of the container category whose option
// was picked. Empty if the form wasn't constructed.
func (tf *TypeForm) ContainerID() string {
	if tf == nil {
		return ""
	}
	return tf.containerID
}

// Chosen reports the picked container-option id. Only meaningful after
// Run() returns without error.
func (tf *TypeForm) Chosen() string {
	if tf == nil || tf.chosen == nil {
		return ""
	}
	return *tf.chosen
}

// RunTypeForm drives phase 2 end-to-end and returns the chosen
// container-option id, or "" + nil when the registry has no container
// sub-category under the language (caller proceeds directly to the
// scope form with empty typeID).
//
// On user cancellation returns "" + ErrCancelled so callers can map to
// exit 130 the same way as the language form.
func RunTypeForm(reg *registry.Registry, langID string, initial registry.Picks) (string, error) {
	tf, err := BuildTypeForm(reg, langID, initial)
	if err != nil {
		return "", err
	}
	if tf == nil {
		return "", nil
	}
	if err := tf.Form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", fmt.Errorf("run type form: %w", err)
	}
	return tf.Chosen(), nil
}

// BuildScopeForm constructs phase 3 — the user-visible category scope
// (language-universal leaves + chosen container-option's leaves +
// cross-cutting top-level leaves) plus a confirm group. typeID may be
// empty when the registry has no container sub-category for the
// language; in that case container-option-scope leaves contribute no
// groups.
//
// Group order matches render order so the on-screen scan order tracks
// the eventual on-disk write order: for each language sub-category in
// folder-prefix order — if it's a container, emit groups for its
// chosen option's leaves (in folder-prefix order); else emit a leaf
// group. Then cross-cutting categories in top-level folder-prefix
// order.
//
// Defaults flow from `initial`. The caller seeds initial via
// ApplyDefaults so single-pick fields land on the registry default
// and multi-pick fields are pre-populated when a default list exists.
func BuildScopeForm(reg *registry.Registry, langID, typeID string, initial registry.Picks) (*ScopeForm, error) {
	if reg == nil {
		return nil, errors.New("BuildScopeForm: nil registry")
	}
	if langID == "" {
		return nil, errors.New("BuildScopeForm: empty langID")
	}

	langCat := reg.FindCategory(registry.LanguageCategoryID)
	if langCat == nil {
		return nil, errors.New("BuildScopeForm: registry has no language category")
	}

	sf := &ScopeForm{
		langID:  langID,
		typeID:  typeID,
		singles: map[string]*string{},
		multis:  map[string]*[]string{},
	}

	var groups []*huh.Group

	// Identify the one container category in this scope (if any) up
	// front so the loop below doesn't have to track first/last/any.
	// The validator enforces at-most-one container per scope, so
	// FindContainerSub's "first match wins" is canonical and unique —
	// recording its id here keeps BuildScopeForm and FindContainerSub
	// behaviourally identical even if a future scope ever held a
	// second container (it would fail to load, but the form code
	// remains correct in isolation).
	//
	// Note on L3 (containerID set with empty typeID): Picks() guards
	// on `typeID != "" && containerID != ""`, so the wrong key is
	// never emitted. The id is recorded eagerly to mirror the
	// FindContainerSub contract used by the CLI's runTypePhase
	// (which detects "the container slot exists" before deciding to
	// run phase 2 even when typeID would end up empty). Tests pin
	// this contract via TestBuildScopeForm_ThreeLevelEmptyType.
	if container := FindContainerSub(reg, langID); container != nil {
		sf.containerID = container.ID
	}

	// Language sub-categories. Container subs contribute their chosen
	// option's leaves (when typeID is set); non-container subs
	// contribute themselves directly.
	for _, sub := range langCat.Subcategories[langID] {
		if !sub.IsContainer {
			key := langID + "." + sub.ID
			g, err := buildCategoryGroup(sub, key, initial, sf)
			if err != nil {
				return nil, err
			}
			groups = append(groups, g)
			continue
		}
		// Container sub: descend into the chosen option's subtree if
		// typeID is set. The validator guarantees this is the same
		// category FindContainerSub returns above (at-most-one).
		if typeID == "" {
			// No type picked — container's subtree is dark. The user
			// will see only language-universal leaves + cross-cutting.
			continue
		}
		for _, leaf := range sub.Subcategories[typeID] {
			if leaf.IsContainer {
				// v1 doesn't surface a fourth level in the form. The
				// schema technically allows it; if it ever ships, the
				// form needs a phase 4. For now, surface loudly so a
				// future schema change can't sneak past silently.
				return nil, fmt.Errorf("BuildScopeForm: deeper nesting under %s.%s.%s not yet supported by the form", langID, typeID, leaf.ID)
			}
			key := langID + "." + typeID + "." + leaf.ID
			g, err := buildCategoryGroup(leaf, key, initial, sf)
			if err != nil {
				return nil, err
			}
			groups = append(groups, g)
		}
	}

	// Cross-cutting top-level categories (everything except language).
	// Manifest order, leaves only — top-level containers other than
	// language don't exist in v1 and the form would need a re-think if
	// they did. Surface loudly rather than silently emit a partial form.
	for _, cat := range reg.Categories {
		if cat.ID == registry.LanguageCategoryID {
			continue
		}
		if cat.IsContainer {
			return nil, fmt.Errorf("BuildScopeForm: top-level container %q is not supported by the form", cat.ID)
		}
		g, err := buildCategoryGroup(cat, cat.ID, initial, sf)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}

	// Confirm group with a live-updating summary so the user can walk
	// back through previous groups and the summary tracks edits.
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

// buildCategoryGroup constructs one huh.Group for one leaf category.
// Single-pick → huh.Select; multi-pick → huh.MultiSelect. The bound
// variable is registered on `sf` under `key` and seeded from `initial`.
//
// Empty descriptions on an Option are accepted; huh renders them as a
// blank line, but the validator keeps every option's description
// non-empty (Task 1.4) so this is effectively unreachable.
func buildCategoryGroup(cat *registry.Category, key string, initial registry.Picks, sf *ScopeForm) (*huh.Group, error) {
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
// list of the blocks that will be rendered, in render order. Reads
// from the current bound variables so the summary tracks the user's
// edits while the scope form is open.
//
// Render order matches render.Render's walk: language base, language
// subs (containers expanded into the chosen option's subtree), then
// cross-cutting top-level categories.
func (sf *ScopeForm) summarise(reg *registry.Registry) string {
	picks := sf.Picks()
	var parts []string

	if sf.langID != "" {
		parts = append(parts, "language/"+sf.langID)
	}

	langCat := reg.FindCategory(registry.LanguageCategoryID)
	if langCat != nil {
		for _, sub := range langCat.Subcategories[sf.langID] {
			if !sub.IsContainer {
				parts = appendCategoryParts(parts, sub, picks.Values[sf.langID+"."+sub.ID], sub.ID)
				continue
			}
			// Container: include the type pick itself (so the user sees
			// `type/cli`) and the chosen option's leaves.
			if sf.typeID == "" {
				continue
			}
			parts = append(parts, sub.ID+"/"+sf.typeID)
			for _, leaf := range sub.Subcategories[sf.typeID] {
				if leaf.IsContainer {
					continue
				}
				key := sf.langID + "." + sf.typeID + "." + leaf.ID
				parts = appendCategoryParts(parts, leaf, picks.Values[key], leaf.ID)
			}
		}
	}

	// Cross-cutting top-level categories.
	for _, cat := range reg.Categories {
		if cat.ID == registry.LanguageCategoryID {
			continue
		}
		parts = appendCategoryParts(parts, cat, picks.Values[cat.ID], cat.ID)
	}

	if len(parts) == 0 {
		return "(no blocks selected)"
	}
	return "blocks: " + strings.Join(parts, ", ")
}

// appendCategoryParts adds one or more `<label>/<optID>` entries to
// `parts` for the chosen value of a leaf category. label is the
// user-facing prefix (typically the category's id). Returns the new
// slice so the caller chains without juggling pointer semantics.
func appendCategoryParts(parts []string, cat *registry.Category, v *registry.Value, label string) []string {
	if v == nil {
		return parts
	}
	switch cat.Pick {
	case registry.PickSingle:
		if v.Single != nil && *v.Single != "" {
			parts = append(parts, label+"/"+*v.Single)
		}
	case registry.PickMulti:
		if v.Multi != nil {
			for _, id := range *v.Multi {
				parts = append(parts, label+"/"+id)
			}
		}
	}
	return parts
}

// Picks translates the bound variables back into a registry.Picks. The
// language pick is always set; the container pick (if any) is set when
// typeID is non-empty; every non-empty leaf binding contributes one
// entry under its selection key.
//
// huh v2 quirk reminder (see also doc.go and form.go): Select.Value
// pre-fills the bound pointer with the first option's id at
// construction time, so an "untouched" single-pick comes back with a
// non-empty value. MultiSelect doesn't have this quirk. Picks treats
// an empty binding (both shapes) as "unset" by omission — the resolver
// + ApplyDefaults are the sole source of truth for what the renderer
// sees.
func (sf *ScopeForm) Picks() registry.Picks {
	picks := registry.NewPicks()
	picks.Values[registry.LanguageCategoryID] = registry.NewSingle(sf.langID)
	if sf.typeID != "" && sf.containerID != "" {
		picks.Values[sf.langID+"."+sf.containerID] = registry.NewSingle(sf.typeID)
	}

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
func (sf *ScopeForm) Confirmed() bool {
	if sf == nil || sf.confirmed == nil {
		return false
	}
	return *sf.confirmed
}

// RunScopeForm drives phase 3 end-to-end: build, run, translate back
// to Picks. If the user picked No on the confirm group, returns
// ErrCancelled so callers don't need to distinguish "user said no"
// from "user hit Ctrl-C" — both mean "don't write the file."
//
// typeID may be empty when no type container exists for the language;
// callers honour that by passing "" through from RunTypeForm.
func RunScopeForm(reg *registry.Registry, langID, typeID string, initial registry.Picks) (registry.Picks, error) {
	sf, err := BuildScopeForm(reg, langID, typeID, initial)
	if err != nil {
		return registry.Picks{}, err
	}
	if err := sf.Form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return registry.Picks{}, ErrCancelled
		}
		return registry.Picks{}, fmt.Errorf("run scope form: %w", err)
	}
	if !sf.Confirmed() {
		return registry.Picks{}, ErrCancelled
	}
	return sf.Picks(), nil
}
