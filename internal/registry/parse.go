package registry

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// indexDoc is the YAML DTO for `_index.yaml`. Kept private to the
// package — callers consume the typed Category. Using a DTO with the
// yaml.v3 decoder in `KnownFields(true)` (strict) mode catches schema
// drift loudly per spec §9.1 and the Task 1.3 tradeoff note.
type indexDoc struct {
	DisplayName string        `yaml:"display_name"`
	Pick        string        `yaml:"pick"`
	Required    bool          `yaml:"required"`
	Default     yaml.Node     `yaml:"default"`
	Options     []optionEntry `yaml:"options"`
}

type optionEntry struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"display_name"`
	Description string `yaml:"description"`
	File        string `yaml:"file"`
}

// folderNamePattern matches a category folder name per spec §9.1:
// two-digit prefix, dash, kebab body.
var folderNamePattern = regexp.MustCompile(`^[0-9]{2}-[a-z][a-z0-9-]*$`)

// stripPrefix removes a leading `NN-` from a folder name. Returns the
// input unchanged if no two-digit prefix is present, so callers can
// use this on language-id subfolders (`python`) as well.
func stripPrefix(name string) string {
	if len(name) > 3 && name[2] == '-' && isDigit(name[0]) && isDigit(name[1]) {
		return name[3:]
	}
	return name
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// parseIndex reads and decodes one `_index.yaml` at `dir/_index.yaml`
// into a Category. The folder name's `NN-` prefix is the ordering
// signal (callers sort by folder name before calling this).
func parseIndex(fsys fs.FS, dir string) (*Category, error) {
	idxPath := path.Join(dir, "_index.yaml")
	raw, err := fs.ReadFile(fsys, idxPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", idxPath, err)
	}

	var doc indexDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", idxPath, err)
	}

	cat := &Category{
		Path:        dir,
		ID:          stripPrefix(path.Base(dir)),
		DisplayName: doc.DisplayName,
		Pick:        Pick(doc.Pick),
		Required:    doc.Required,
	}

	for _, o := range doc.Options {
		cat.Options = append(cat.Options, Option{
			ID:          o.ID,
			DisplayName: o.DisplayName,
			Description: o.Description,
			File:        o.File,
		})
	}

	if !doc.Default.IsZero() {
		switch cat.Pick {
		case PickSingle:
			var s string
			if err := doc.Default.Decode(&s); err != nil {
				return nil, fmt.Errorf("parse %s: default must be a string for pick=single: %w", idxPath, err)
			}
			cat.DefaultSingle = s
		case PickMulti:
			var ss []string
			if err := doc.Default.Decode(&ss); err != nil {
				return nil, fmt.Errorf("parse %s: default must be a list for pick=multi: %w", idxPath, err)
			}
			cat.DefaultMulti = ss
		}
	}

	return cat, nil
}

// listCategoryDirs returns the immediate-child entries of `dir` whose
// names match the `NN-name` category-folder pattern, sorted by name
// so the `NN-` prefix gives deterministic render order. Non-matching
// entries are skipped; the hygiene validator in Task 1.4 owns
// reporting them as errors.
func listCategoryDirs(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !folderNamePattern.MatchString(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// listLanguageDirs returns the language subfolders under
// `templates/10-language/`. Names are not `NN-` prefixed (they're
// just `python`, `go`, etc.); ordering matches the option list in the
// language category's `_index.yaml`, which the caller threads in.
func listLanguageDirs(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
