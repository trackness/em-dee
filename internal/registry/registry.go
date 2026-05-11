package registry

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
)

// templatesFS embeds the production templates tree at build time. The
// `all:` prefix captures dotfiles and underscored files (e.g.
// `_index.yaml`), which a bare `//go:embed templates/**` would skip.
// The directory must exist at build time even if empty, hence the
// `.gitkeep` placeholder under `templates/`.
//
//go:embed all:templates
var templatesFS embed.FS

// Load reads the embedded templates filesystem and returns a fully
// parsed and validated Registry. An empty `templates/` (until Phase 7
// fills it with finalised content) returns an empty Registry with no
// error.
func Load() (*Registry, error) {
	return load(templatesFS, "templates")
}

// LoadFS is the exported entry point for loading a Registry from an
// arbitrary fs.FS. It's the seam the render package and `em-dee show`
// use to point at fixture trees (e.g. `internal/render/testdata/`)
// without needing the production embedded filesystem. Behaviour is
// otherwise identical to Load.
func LoadFS(fsys fs.FS, root string) (*Registry, error) {
	return load(fsys, root)
}

// load is the test-injectable entry point so unit tests can drive the
// parser/validator against in-memory fixtures (under
// `internal/registry/testdata/`) without needing to re-embed the
// production tree.
func load(fsys fs.FS, root string) (*Registry, error) {
	reg, err := walk(fsys, root)
	if err != nil {
		return nil, err
	}
	if err := Validate(reg, fsys, root); err != nil {
		return nil, err
	}
	reg.fsys = fsys
	return reg, nil
}

// OptionBlock returns the raw bytes of the `.md` block file for the
// given option in the given category. The lookup uses the category's
// Path (the embedded-FS path of the category folder) joined with the
// option's File field. Returns an error if the option id doesn't
// exist in the category or the file can't be read.
//
// This is the single read seam between Registry and the renderer:
// callers don't reach into the embedded FS directly, so the
// fsys+root coupling stays inside the registry package per the
// CLAUDE.md "no hidden coupling" principle.
func (r *Registry) OptionBlock(cat *Category, optionID string) ([]byte, error) {
	if cat == nil {
		return nil, fmt.Errorf("OptionBlock: nil category")
	}
	if r.fsys == nil {
		return nil, fmt.Errorf("OptionBlock: registry has no filesystem (constructed outside Load)")
	}
	for _, opt := range cat.Options {
		if opt.ID == optionID {
			file := path.Join(cat.Path, opt.File)
			raw, err := fs.ReadFile(r.fsys, file)
			if err != nil {
				return nil, fmt.Errorf("OptionBlock: read %s: %w", file, err)
			}
			return raw, nil
		}
	}
	return nil, fmt.Errorf("OptionBlock: option %q not found in category %q", optionID, cat.ID)
}

// walk descends from `root` and assembles the Registry. An empty (or
// missing) root yields an empty Registry with no error — this keeps
// Task 1.2's "empty templates compiles" guarantee. Each top-level
// `NN-name` directory becomes one Category; the `10-language` entry
// recurses into per-language subfolders.
func walk(fsys fs.FS, root string) (*Registry, error) {
	reg := &Registry{}

	dirs, err := listCategoryDirs(fsys, root)
	if err != nil {
		// Treat "root doesn't exist" as empty rather than error so
		// the production embedded FS (only `.gitkeep` for now) loads
		// cleanly. Other I/O errors do propagate.
		if errors.Is(err, fs.ErrNotExist) {
			return reg, nil
		}
		return nil, err
	}

	for _, name := range dirs {
		dir := path.Join(root, name)
		// Explicit hygiene rule (spec §9.1: "every category folder
		// contains exactly one _index.yaml"). Probe first so the
		// error message names the rule rather than relying on the
		// shape of fs.ReadFile's I/O error — a future os.DirFS swap
		// that silently ignored missing files would otherwise go
		// undetected.
		if _, err := fs.Stat(fsys, path.Join(dir, "_index.yaml")); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("%s: missing _index.yaml", dir)
			}
			return nil, err
		}
		cat, err := parseIndex(fsys, dir)
		if err != nil {
			return nil, err
		}
		// Special-case the language category: descend into each
		// language option's folder and collect its sub-categories.
		// Per spec §4.1 the language category is the only one with
		// subcategories; all others stay flat. Match on the parsed
		// (prefix-stripped) id rather than the on-disk folder name so
		// the literal `10-language` lives only inside parseIndex's
		// stripPrefix and is never re-encoded at this layer.
		if cat.ID == LanguageCategoryID {
			cat.Subcategories = map[string][]*Category{}
			for _, opt := range cat.Options {
				langDir := path.Join(dir, opt.ID)
				subDirs, err := listCategoryDirs(fsys, langDir)
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return nil, err
				}
				var subs []*Category
				for _, subName := range subDirs {
					subDir := path.Join(langDir, subName)
					if _, err := fs.Stat(fsys, path.Join(subDir, "_index.yaml")); err != nil {
						if errors.Is(err, fs.ErrNotExist) {
							return nil, fmt.Errorf("%s: missing _index.yaml", subDir)
						}
						return nil, err
					}
					sub, err := parseIndex(fsys, subDir)
					if err != nil {
						return nil, err
					}
					subs = append(subs, sub)
				}
				cat.Subcategories[opt.ID] = subs
			}
		}
		reg.Categories = append(reg.Categories, cat)
	}

	return reg, nil
}
