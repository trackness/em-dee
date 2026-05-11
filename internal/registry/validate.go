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

// optionIDPattern is the kebab-id regex from spec §9.1.
var optionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate walks a parsed Registry plus its source filesystem and
// returns a joined error covering every hygiene rule from spec §9.1
// that fails. `errors.Join` produces a multi-line error so a single
// `task verify` run surfaces every problem at once rather than one
// per cycle. Each wrapped error includes the offending path so the
// noise is actionable.
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

	// Per-category checks. We identify the language category by its
	// (prefix-stripped) id rather than its on-disk path so the
	// `NN-language` folder prefix is owned by parseIndex's
	// stripPrefix and never re-encoded here.
	for _, cat := range reg.Categories {
		errs = append(errs, validateCategory(cat, fsys)...)
		if cat.ID == LanguageCategoryID {
			errs = append(errs, validateLanguageCategory(cat)...)
			// Children of the language root are language-id folders
			// (`python`, `go`, …), which use a different naming rule
			// (kebab, no NN- prefix) — see validateLanguageFolderNames.
			if subErr := validateLanguageFolderNames(fsys, cat.Path); subErr != nil {
				errs = append(errs, subErr...)
			}
			// Language subtrees: each language sub-folder must
			// contain a base.md + its own per-language categories.
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
				for _, sub := range cat.Subcategories[opt.ID] {
					errs = append(errs, validateCategory(sub, fsys)...)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// validateLanguageCategoryIDCollisions enforces "no language option id
// equals any top-level category id" — the invariant the `em-dee show`
// disambiguation rule depends on. Returns a slice of errors so the
// caller can fold them into the joined error like the rest of the
// hygiene checks.
func validateLanguageCategoryIDCollisions(reg *Registry) []error {
	var errs []error
	var langCat *Category
	topLevelIDs := map[string]bool{}
	for _, cat := range reg.Categories {
		topLevelIDs[cat.ID] = true
		if cat.ID == LanguageCategoryID {
			langCat = cat
		}
	}
	if langCat == nil {
		return nil
	}
	for _, opt := range langCat.Options {
		if topLevelIDs[opt.ID] {
			errs = append(errs, fmt.Errorf("%s: language option id %q collides with top-level category id %q (would break show's disambiguation rule)", langCat.Path, opt.ID, opt.ID))
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
			// against its neighbour, breaking the §4.4 trailing-
			// newline contract from the outside. Forbid the input
			// shape rather than paper over it in the renderer:
			// "filesystem is the schema" — if the block has no
			// content, the option shouldn't be in the manifest.
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

// validateLanguageCategory enforces spec §9.1's special rules for the
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
