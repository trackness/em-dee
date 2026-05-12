package registry

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
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
// `NN-name` directory becomes one Category; any category whose options
// point at subdirectories (a *container* per CONTENT-STYLE.md §2.3,
// §2.4) recurses through each option's subtree to collect further
// categories at arbitrary depth.
//
// Depth is bounded by the on-disk tree (no theoretical limit; v1 is
// three levels under language). There is no infinite-loop risk:
// descent only follows option ids, each id appears once per scope, and
// the filesystem is a finite tree.
func walk(fsys fs.FS, root string) (*Registry, error) {
	reg := &Registry{}

	cats, err := walkScope(fsys, root)
	if err != nil {
		return nil, err
	}
	reg.Categories = cats
	return reg, nil
}

// walkScope returns the NN-prefixed categories that live directly
// under `dir`, recursing into any container category's option
// subdirectories so the result is a fully-populated subtree. A
// non-existent `dir` (the empty-templates case) yields a nil slice and
// no error; that's how the embedded-FS placeholder load stays clean.
func walkScope(fsys fs.FS, dir string) ([]*Category, error) {
	subDirs, err := listCategoryDirs(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cats []*Category
	for _, name := range subDirs {
		catDir := path.Join(dir, name)
		cat, err := parseCategoryRecursive(fsys, catDir)
		if err != nil {
			return nil, err
		}
		cats = append(cats, cat)
	}
	return cats, nil
}

// parseCategoryRecursive reads `<catDir>/_index.yaml`, classifies the
// category as leaf or container by inspecting its options' `file:`
// references, and if it's a container recurses into each option's
// subdirectory to collect that subtree's sub-categories.
//
// Container classification rule: a category is a container iff *any*
// option's `file:` contains a `/`. Mixed shapes (some options leaf,
// some container) are passed through unflagged at this layer — the
// validator owns reporting them as a hygiene error so a single
// `task verify` run surfaces every problem at once rather than aborting
// at the first failure.
func parseCategoryRecursive(fsys fs.FS, catDir string) (*Category, error) {
	if _, err := fs.Stat(fsys, path.Join(catDir, "_index.yaml")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: missing _index.yaml", catDir)
		}
		return nil, err
	}
	cat, err := parseIndex(fsys, catDir)
	if err != nil {
		return nil, err
	}

	if !looksLikeContainer(cat) {
		return cat, nil
	}

	cat.IsContainer = true
	cat.Subcategories = map[string][]*Category{}
	for _, opt := range cat.Options {
		optDir := path.Join(catDir, opt.ID)
		subs, err := walkScope(fsys, optDir)
		if err != nil {
			return nil, err
		}
		cat.Subcategories[opt.ID] = subs
	}
	return cat, nil
}

// looksLikeContainer reports whether any option in the category
// references a subdirectory (its `file:` contains a `/`). The validator
// is responsible for rejecting *mixed* shapes (some options leaf, some
// container); here we just need to know whether to recurse.
func looksLikeContainer(cat *Category) bool {
	for _, opt := range cat.Options {
		if strings.Contains(opt.File, "/") {
			return true
		}
	}
	return false
}
