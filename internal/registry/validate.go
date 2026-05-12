package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// optionIDPattern is the kebab-id regex applied to every option id.
var optionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate walks a parsed Registry plus its source filesystem and
// returns a joined error covering every hygiene rule that fails.
// `errors.Join` produces a multi-line error so a single `task verify`
// run surfaces every problem at once rather than one per cycle. Each
// wrapped error includes the offending path so the noise is
// actionable.
func Validate(reg *Registry, fsys fs.FS, root string) error {
	var errs []error

	// Top-level: directory-name pattern (NN- prefix; language root
	// excluded — its children are language-id folders, not categories).
	if dirErr := validateCategoryFolderNames(fsys, root); dirErr != nil {
		errs = append(errs, dirErr...)
	}

	// Cross-category invariant: no language option id may collide
	// with a top-level category id. The `em-dee show` resolver
	// disambiguates `<lang>.<cat>.<opt>` vs `<cat>.<opt>` by asking
	// "is the first segment a known language id?". A future language
	// id `infra` would silently shadow the existing `infra.docker`
	// top-level form, so pin the invariant at load time. See
	// internal/cli/show.go's disambiguation rule for the consumer.
	errs = append(errs, validateLanguageCategoryIDCollisions(reg)...)

	// At-most-one container per top-level scope. The form's type-axis
	// is single-slot per scope (BuildScopeForm picks one container,
	// FindContainerSub picks one container — same single slot). A
	// second container in the same scope would have one of the two
	// helpers pick it inconsistently, so reject the shape at load time.
	errs = append(errs, validateAtMostOneContainerInScope(reg.Categories, "<root>")...)

	// Per-category checks. We identify the language category by its
	// (prefix-stripped) id rather than its on-disk path so the
	// `NN-language` folder prefix is owned by parseIndex's
	// stripPrefix and never re-encoded here.
	for _, cat := range reg.Categories {
		errs = append(errs, validateCategoryTree(cat, fsys)...)
		if cat.ID == LanguageCategoryID {
			errs = append(errs, validateLanguageCategory(cat)...)
			// Children of the language root are language-id folders
			// (`python`, `go`, …), which use a different naming rule
			// (kebab, no NN- prefix) — see validateLanguageFolderNames.
			if subErr := validateLanguageFolderNames(fsys, cat.Path); subErr != nil {
				errs = append(errs, subErr...)
			}
			// Language subtrees: each language sub-folder must
			// contain a base.md (the required language-base position
			// per CONTENT-STYLE.md §2.7).
			for _, opt := range cat.Options {
				langDir := path.Join(cat.Path, opt.ID)
				if _, err := fs.Stat(fsys, path.Join(langDir, "base.md")); err != nil {
					errs = append(errs, fmt.Errorf("%s: missing base.md", langDir))
				}
				// Each sub-folder under a language must follow the
				// NN-name pattern (regular category-folder rules).
				if subErr := validateCategoryFolderNames(fsys, langDir); subErr != nil {
					errs = append(errs, subErr...)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// validateCategoryTree runs validateCategory on `cat` and, when `cat`
// is a container, additionally enforces the container-shape rules
// (CONTENT-STYLE.md §2.3, §2.4) before recursing through every
// option's subtree applying the same rules at every depth. Container
// option subtree folders must themselves follow the NN-name folder
// convention (every category folder at every depth obeys section 3 of
// CONTENT-STYLE.md).
func validateCategoryTree(cat *Category, fsys fs.FS) []error {
	errs := validateCategory(cat, fsys)
	errs = append(errs, validateShape(cat, fsys)...)
	if !cat.IsContainer {
		return errs
	}
	errs = append(errs, validateContainer(cat, fsys)...)
	// Per-subtree scope-collision check: within each container
	// option's subtree, no sub-category's id may equal a sibling
	// container's option id (in that same scope). The show resolver
	// tries category-id match before falling back to elided-container
	// option match; a collision would render the option unreachable
	// via `<scope>.<id>`. v1's nested scopes are partitioned by their
	// parent so collisions are rare, but the rule is mechanical and
	// pinning it here prevents Dispatch 4's catalog migration from
	// silently introducing one.
	for _, opt := range cat.Options {
		errs = append(errs, validateScopeIDCollisions(cat.Subcategories[opt.ID], cat.Path, opt.ID)...)
	}
	for _, opt := range cat.Options {
		// At-most-one container per nested scope. Same invariant as
		// the top-level rule, pinned per container-option subtree so
		// e.g. a language's sub-categories can hold at most one
		// container (the type-axis slot).
		errs = append(errs, validateAtMostOneContainerInScope(cat.Subcategories[opt.ID], cat.Path+"."+opt.ID)...)
	}
	for _, opt := range cat.Options {
		// The container's subtree lives at <cat.Path>/<opt.ID>. Its
		// direct children must be NN-prefixed category folders (or,
		// for the language container specifically, language-id
		// folders — see validateLanguageFolderNames above).
		if cat.ID != LanguageCategoryID {
			optDir := path.Join(cat.Path, opt.ID)
			if subErr := validateCategoryFolderNames(fsys, optDir); subErr != nil {
				errs = append(errs, subErr...)
			}
		}
		// Scope folder orphan-scan: a container-option subfolder
		// holds at most a `base.md` plus NN-prefixed sub-categories.
		// A stray `.md` here is not referenced by any `_index.yaml`
		// and would silently never render. The language root has the
		// same shape — handled by validateCategory's existing
		// orphan-scan at cat.Path — but for nested containers
		// (e.g. python/10-type/cli/) the option subfolder isn't a
		// category folder, so this scan is its only defender.
		errs = append(errs, validateScopeFolder(fsys, path.Join(cat.Path, opt.ID))...)
		for _, sub := range cat.Subcategories[opt.ID] {
			errs = append(errs, validateCategoryTree(sub, fsys)...)
		}
	}
	return errs
}

// validateScopeIDCollisions reports a collision between a
// sub-category's id and any inner-container's option id within the
// same scope. `parentPath` and `parentOpt` name the enclosing
// container/option for the error message so a violation points at
// the specific scope rather than a generic name.
func validateScopeIDCollisions(scope []*Category, parentPath, parentOpt string) []error {
	var errs []error
	catIDs := map[string]bool{}
	for _, cat := range scope {
		catIDs[cat.ID] = true
	}
	for _, cat := range scope {
		if !cat.IsContainer {
			continue
		}
		for _, opt := range cat.Options {
			if catIDs[opt.ID] {
				errs = append(errs, fmt.Errorf("%s: container option id %q in scope %s.%s collides with a sibling category id (would break show's disambiguation rule)", cat.Path, opt.ID, parentPath, parentOpt))
			}
		}
	}
	return errs
}

// validateAtMostOneContainerInScope pins the invariant the form layer
// relies on: a given scope (e.g. one language's sub-categories) holds
// at most one container category. The form's "type axis" is implicit
// in BuildScopeForm and FindContainerSub — both helpers pick a single
// container per scope; a second container would silently lose one
// side of the pair (BuildScopeForm last-wins, FindContainerSub
// first-wins). Pinning the invariant here means the divergence can't
// matter: any registry with two containers in one scope fails to load.
//
// `scopeLabel` names the scope for the error message (e.g.
// "python" for a language sub-tree, or "<root>" for the top level).
func validateAtMostOneContainerInScope(scope []*Category, scopeLabel string) []error {
	var containers []string
	for _, cat := range scope {
		if cat.IsContainer {
			containers = append(containers, cat.ID)
		}
	}
	if len(containers) <= 1 {
		return nil
	}
	return []error{fmt.Errorf("scope %s: at most one container category is allowed per scope (got %d: %v); the form's type-axis is single-slot and a second container would be unreachable",
		scopeLabel, len(containers), containers)}
}

// validateScopeFolder rejects stray `.md` files at a container
// option's subfolder root. The only `.md` file licensed at this
// scope is `base.md` (governed by validateContainer's `file:` rule);
// every other `.md` would be unreachable from the manifest.
func validateScopeFolder(fsys fs.FS, dir string) []error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		// Missing subdirectory is reported elsewhere (the
		// missing-file check in validateCategory). Other I/O errors
		// are swallowed here for the same defensive reason —
		// validateCategoryFolderNames also returns nil on read error.
		return nil
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "base.md" {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: orphan .md file %s at container-option scope (only base.md is licensed at this level)", dir, name))
	}
	return errs
}

// validateShape pins the mixed-shape rule (CONTENT-STYLE.md §2.4): a
// category's options must all be leaf-shaped (`file:` is a bare
// filename) OR all be container-shaped (`file:` is a path containing
// `/`). The reasoning is that a category models one selection axis —
// half-leaf, half-container makes the type-rendering branch
// non-deterministic. The shape is fully determined by the option
// `file:` values, so the rule is mechanical.
//
// Additionally, when the category is a leaf, validateShape rejects a
// `base.md` file at the category root: per §2.7, `base.md` is only
// licensed at language- and type-scope positions, never inside a
// category folder.
func validateShape(cat *Category, fsys fs.FS) []error {
	var errs []error

	if len(cat.Options) >= 2 {
		hasLeaf := false
		hasContainer := false
		for _, opt := range cat.Options {
			if strings.Contains(opt.File, "/") {
				hasContainer = true
			} else if opt.File != "" {
				hasLeaf = true
			}
		}
		if hasLeaf && hasContainer {
			errs = append(errs, fmt.Errorf("%s: mixed-shape category (some options point at files, others at subdirectories); a category must be all-leaf or all-container", cat.Path))
		}
	}

	if !cat.IsContainer {
		// Per §2.7: per-leaf-category `base.md` is not licensed.
		baseStat, err := fs.Stat(fsys, path.Join(cat.Path, "base.md"))
		if err == nil && !baseStat.IsDir() {
			errs = append(errs, fmt.Errorf("%s: base.md is not licensed inside a leaf category folder (CONTENT-STYLE.md §2.7); move it to the language or type scope", cat.Path))
		}
	}

	return errs
}

// validateContainer enforces the rules that bind container categories
// only (CONTENT-STYLE.md §2.3, §2.4). The category-classification has
// already happened in walkScope; here we just check the implications.
//
//   - `pick` must be `single` — a container expresses a "which
//     subtree applies" question.
//   - Each option's `file:` is either `<opt.ID>/base.md` (when the
//     subtree exposes a scope-level base block) or `<opt.ID>/` (when
//     it doesn't). The first segment must equal `opt.ID` so the walk
//     and the file reference agree on the subdirectory name.
//   - When the `file:` is `<opt.ID>/base.md`, that file must exist
//     (the missing-file check in validateCategory catches this).
//   - When the `file:` is `<opt.ID>/`, no `base.md` may exist inside
//     the subdirectory — the `file:` told the renderer there is no
//     scope-base, and a stray base.md would never be reached.
func validateContainer(cat *Category, fsys fs.FS) []error {
	var errs []error
	if cat.Pick != PickSingle {
		errs = append(errs, fmt.Errorf("%s: container category must have pick: single, got %q", cat.Path, cat.Pick))
	}
	for _, opt := range cat.Options {
		// Container option file must be `<opt.ID>/base.md` or `<opt.ID>/`.
		wantBase := opt.ID + "/base.md"
		wantDir := opt.ID + "/"
		switch opt.File {
		case wantBase, wantDir:
			// ok
		default:
			errs = append(errs, fmt.Errorf("%s: container option %q: file must be %q or %q, got %q", cat.Path, opt.ID, wantBase, wantDir, opt.File))
			continue
		}
		// When file: is `<opt.ID>/`, no base.md may exist in the
		// subdirectory — the option declared "no scope-base".
		if opt.File == wantDir {
			baseStat, err := fs.Stat(fsys, path.Join(cat.Path, opt.ID, "base.md"))
			if err == nil && !baseStat.IsDir() {
				errs = append(errs, fmt.Errorf("%s: container option %q declares no scope-base (file: %s) but %s/%s/base.md exists; either reference it via file: %s or delete the file", cat.Path, opt.ID, wantDir, cat.Path, opt.ID, wantBase))
			}
		}
	}
	return errs
}

// validateLanguageCategoryIDCollisions enforces "no top-level
// container option id equals any top-level category id" — the
// invariant the `em-dee show` disambiguation rule depends on. The
// resolver tries a category-id match first, then falls back to a
// top-level container's option ids; a collision would make one of
// the two reachable forms shadow the other silently.
//
// The function's name is historical (the only top-level container at
// v1 is `language`); the check covers every top-level container. The
// rule is identical at every depth via the generalised resolver but
// only the top level needs explicit cross-category enforcement,
// because nested ids are already partitioned by their parent scope.
func validateLanguageCategoryIDCollisions(reg *Registry) []error {
	var errs []error
	topLevelIDs := map[string]bool{}
	for _, cat := range reg.Categories {
		topLevelIDs[cat.ID] = true
	}
	for _, cat := range reg.Categories {
		if !cat.IsContainer {
			continue
		}
		for _, opt := range cat.Options {
			if topLevelIDs[opt.ID] {
				errs = append(errs, fmt.Errorf("%s: container option id %q collides with top-level category id %q (would break show's disambiguation rule)", cat.Path, opt.ID, opt.ID))
			}
		}
	}
	return errs
}

// validateCategoryFolderNames asserts every direct-child directory of
// `dir` matches `^[0-9]{2}-[a-z][a-z0-9-]*$`. Used for the top-level
// categories tree and for each language subtree's NN-prefixed
// sub-categories. The language-root (`templates/10-language/`) has a
// different rule for *its* direct children — see
// validateLanguageFolderNames.
func validateCategoryFolderNames(fsys fs.FS, dir string) []error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !folderNamePattern.MatchString(e.Name()) {
			errs = append(errs, fmt.Errorf("%s/%s: folder name must match %s",
				dir, e.Name(), folderNamePattern.String()))
		}
	}
	return errs
}

// validateLanguageFolderNames asserts every direct-child directory of
// `dir` (which must be the language root `templates/10-language/`)
// matches the kebab option-id pattern. Language ids are kebab but
// **not** NN-prefixed — language render order comes from the option
// list in `templates/10-language/_index.yaml`, not from folder names.
func validateLanguageFolderNames(fsys fs.FS, dir string) []error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !optionIDPattern.MatchString(e.Name()) {
			errs = append(errs, fmt.Errorf("%s/%s: language folder name must match %s",
				dir, e.Name(), optionIDPattern.String()))
		}
	}
	return errs
}

// validateCategory runs every per-category hygiene rule. Returns a
// slice of errors; callers join via errors.Join.
func validateCategory(cat *Category, fsys fs.FS) []error {
	var errs []error

	if cat.DisplayName == "" {
		errs = append(errs, fmt.Errorf("%s: display_name must be non-empty", cat.Path))
	}

	switch cat.Pick {
	case PickSingle, PickMulti:
		// ok
	default:
		errs = append(errs, fmt.Errorf("%s: pick must be 'single' or 'multi', got %q", cat.Path, cat.Pick))
	}

	// Option-level checks.
	seenID := map[string]bool{}
	// seenFile tracks the **basenames** of files referenced by options
	// in this category folder, used by the (non-recursive) orphan
	// scan below. Language-option `file:` values are path-prefixed
	// (e.g. `python/base.md`); those references point to files in
	// language subfolders, not this category's flat root, so they
	// would never match anything in the orphan scan. We deliberately
	// skip writing path-bearing entries to keep seenFile structurally
	// aligned with what the orphan scan reads.
	seenFile := map[string]bool{}
	for _, opt := range cat.Options {
		if opt.DisplayName == "" {
			errs = append(errs, fmt.Errorf("%s: option %q: display_name must be non-empty", cat.Path, opt.ID))
		}
		if !optionIDPattern.MatchString(opt.ID) {
			errs = append(errs, fmt.Errorf("%s: option id %q must match %s", cat.Path, opt.ID, optionIDPattern.String()))
		}
		if seenID[opt.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate option id %q", cat.Path, opt.ID))
		}
		seenID[opt.ID] = true
		// Pin `file:` as non-empty here rather than relying on the
		// downstream missing-file check to fire indirectly: an empty
		// path joins to the category's own directory, which fs.Stat
		// returns as a directory rather than a missing entry. The
		// resulting error path (the category folder itself) is
		// confusing, so reject the empty input shape with a clear
		// rule-named message instead.
		if len(opt.File) == 0 {
			errs = append(errs, fmt.Errorf("%s: option %q: file must be non-empty", cat.Path, opt.ID))
			// Skip the orphan-scan registration for the empty path;
			// writing `seenFile[""] = true` is harmless today (no .md
			// can match) but would become wrong if seenFile gained any
			// other consumer.
			continue
		}
		if !strings.Contains(opt.File, "/") {
			// Flat-category reference: pin against the orphan scan.
			seenFile[opt.File] = true
		}

		filePath := path.Join(cat.Path, opt.File)
		info, err := fs.Stat(fsys, filePath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: option %q references missing file %s", cat.Path, opt.ID, opt.File))
		} else if !info.IsDir() && info.Size() == 0 {
			// Block files are concatenated by render.join with "\n\n"
			// glue. A zero-byte block would produce a leading "\n\n"
			// against its neighbour, breaking the trailing-newline
			// contract from the outside. Forbid the input shape
			// rather than paper over it in the renderer: "filesystem
			// is the schema" — if the block has no content, the
			// option shouldn't be in the manifest.
			errs = append(errs, fmt.Errorf("%s: option %q references empty block file %s (zero bytes)", cat.Path, opt.ID, opt.File))
		}
	}

	// Orphan .md check: every .md file under cat.Path (non-recursive,
	// at the category root only) must be referenced by some option's
	// `file`. Exception: base.md under templates/10-language/<lang>/
	// — but those live one directory above any per-language category
	// root, so they don't even appear in this listing.
	entries, err := fs.ReadDir(fsys, cat.Path)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			if !seenFile[name] {
				errs = append(errs, fmt.Errorf("%s: orphan .md file %s (not referenced by any option)", cat.Path, name))
			}
		}
	}

	// Default validation.
	switch cat.Pick {
	case PickSingle:
		if cat.DefaultSingle != "" && !seenID[cat.DefaultSingle] {
			errs = append(errs, fmt.Errorf("%s: default %q does not reference a valid option id", cat.Path, cat.DefaultSingle))
		}
	case PickMulti:
		seenDef := map[string]bool{}
		for _, d := range cat.DefaultMulti {
			if !seenID[d] {
				errs = append(errs, fmt.Errorf("%s: default %q does not reference a valid option id", cat.Path, d))
			}
			if seenDef[d] {
				errs = append(errs, fmt.Errorf("%s: default has duplicate entry %q", cat.Path, d))
			}
			seenDef[d] = true
		}
	}

	// Deterministic order so test substring assertions are stable.
	// sort.Slice is sufficient — the comparator is a total order on
	// strings, so stable-vs-unstable produces identical output.
	sort.Slice(errs, func(i, j int) bool {
		return errs[i].Error() < errs[j].Error()
	})

	return errs
}

// validateLanguageCategory enforces the special rules for the
// language category: required: true, pick: single, no default, and
// each option's `file:` is exactly `<opt.ID>/base.md`. The last rule
// pins the implicit invariant that the walk relies on
// (`path.Join(langRoot, opt.ID)` finds the language subtree) — break
// it and `walk` looks in the wrong folder while the validator's file-
// existence check passes because `opt.File`'s parent disagrees with
// `opt.ID`.
func validateLanguageCategory(cat *Category) []error {
	var errs []error
	if !cat.Required {
		errs = append(errs, fmt.Errorf("%s: language category must have required: true", cat.Path))
	}
	if cat.Pick != PickSingle {
		errs = append(errs, fmt.Errorf("%s: language category must have pick: single", cat.Path))
	}
	if cat.DefaultSingle != "" || len(cat.DefaultMulti) > 0 {
		errs = append(errs, fmt.Errorf("%s: language category must not have a default", cat.Path))
	}
	for _, opt := range cat.Options {
		want := opt.ID + "/base.md"
		if opt.File != want {
			errs = append(errs, fmt.Errorf("%s: language option %q: file must be %q, got %q",
				cat.Path, opt.ID, want, opt.File))
		}
	}
	return errs
}
