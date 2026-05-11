package registry

import (
	"embed"
	"io/fs"
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
// parsed and validated Registry. For Task 1.2 this is a walk skeleton
// — the manifest parser and validator land in Tasks 1.3 / 1.4. An
// empty `templates/` (the current state) returns an empty Registry
// with no error.
func Load() (*Registry, error) {
	return load(templatesFS, "templates")
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
	return reg, nil
}

// walk descends from `root` and assembles the Registry. Task 1.2
// returns an empty Registry for an empty (or missing) `root`; Task 1.3
// fills this out with `_index.yaml` parsing.
func walk(fsys fs.FS, root string) (*Registry, error) {
	reg := &Registry{}

	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		// A missing root in the embedded production FS (templates/
		// with only `.gitkeep`) is treated as "empty registry, no
		// error" so Task 1.2's verification passes even before
		// templates exist. Real validation in Task 1.4 will catch
		// missing-but-required structure at the manifest level.
		if _, statErr := fs.Stat(fsys, root); statErr != nil {
			return reg, nil
		}
		return nil, err
	}
	_ = entries
	return reg, nil
}
