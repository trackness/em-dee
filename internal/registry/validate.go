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

	// Top-level: directory-name pattern + at most one _index.yaml.
	if dirErr := validateFolderNames(fsys, root); dirErr != nil {
		errs = append(errs, dirErr...)
	}

	// Per-category checks.
	for _, cat := range reg.Categories {
		errs = append(errs, validateCategory(cat, fsys)...)
		if cat.Path == path.Join(root, "10-language") {
			errs = append(errs, validateLanguageCategory(cat)...)
			// Language subtrees: each language sub-folder must
			// contain a base.md + its own per-language categories.
			for _, opt := range cat.Options {
				langDir := path.Join(cat.Path, opt.ID)
				if _, err := fs.Stat(fsys, path.Join(langDir, "base.md")); err != nil {
					errs = append(errs, fmt.Errorf("%s: missing base.md", langDir))
				}
				// Each sub-folder under a language must follow the
				// NN-name pattern too.
				if subErr := validateFolderNames(fsys, langDir); subErr != nil {
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

// validateFolderNames asserts every direct-child directory of `dir`
// matches `^[0-9]{2}-[a-z][a-z0-9-]*$`. Used for the top-level
// categories and for each language's subtree. The `templates/
// 10-language/<lang>/` level is exempt — language ids are kebab but
// not NN-prefixed.
func validateFolderNames(fsys fs.FS, dir string) []error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip language-id folders (their parent is templates/
		// 10-language, which is `dir` when called for language
		// subtree validation only via the `langDir` branch — that
		// path is the language-id folder itself, so its children
		// are NN-prefixed sub-categories, which DO need the check).
		// In other words: we always require the NN- prefix when
		// validating a folder's children — the only exception is
		// the `templates/10-language/` direct children (the
		// language ids), and we never recurse into that loop from
		// here because Validate calls this for each language dir
		// separately and `templates/10-language` is checked once at
		// top level where the children are the four language ids.
		// To handle that, we add a path-based exemption: if `dir`
		// ends in `/10-language`, the children are language ids
		// (kebab, no NN-).
		if strings.HasSuffix(dir, "/10-language") || dir == "10-language" {
			if !optionIDPattern.MatchString(e.Name()) {
				errs = append(errs, fmt.Errorf("%s/%s: language folder name must match %s",
					dir, e.Name(), optionIDPattern.String()))
			}
			continue
		}
		if !folderNamePattern.MatchString(e.Name()) {
			errs = append(errs, fmt.Errorf("%s/%s: folder name must match %s",
				dir, e.Name(), folderNamePattern.String()))
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
		seenFile[opt.File] = true

		filePath := path.Join(cat.Path, opt.File)
		if _, err := fs.Stat(fsys, filePath); err != nil {
			errs = append(errs, fmt.Errorf("%s: option %q references missing file %s", cat.Path, opt.ID, opt.File))
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
	sort.SliceStable(errs, func(i, j int) bool {
		return errs[i].Error() < errs[j].Error()
	})

	return errs
}

// validateLanguageCategory enforces spec §9.1's special rules for the
// language category: required: true, pick: single, no default.
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
	return errs
}
