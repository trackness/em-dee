package registry

import (
	"strings"
	"testing"
	"testing/fstest"
)

// validMapFS builds a clean baseline templates tree in an
// fstest.MapFS. Each test mutates a copy of this map to introduce one
// hygiene violation; assertions look for an error substring specific
// to that rule.
func validMapFS() fstest.MapFS {
	return fstest.MapFS{
		"templates/10-language/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)},
		"templates/10-language/python/base.md": &fstest.MapFile{Data: []byte("python base\n")},
		"templates/10-language/python/10-framework/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Framework"
pick: single
required: false
default: fastapi
options:
  - id: fastapi
    display_name: "FastAPI"
    description: "FastAPI"
    file: fastapi.md
`)},
		"templates/10-language/python/10-framework/fastapi.md": &fstest.MapFile{Data: []byte("fastapi\n")},
		"templates/20-infra/_index.yaml": &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
default: [docker]
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
`)},
		"templates/20-infra/docker.md": &fstest.MapFile{Data: []byte("docker\n")},
	}
}

// TestValidate_CleanBaseline asserts the clean baseline passes.
func TestValidate_CleanBaseline(t *testing.T) {
	t.Parallel()

	fsys := validMapFS()
	if _, err := load(fsys, "templates"); err != nil {
		t.Fatalf("clean baseline failed to load: %v", err)
	}
}

// TestValidate_Rules drives one row per hygiene rule from spec §9.1.
// Each row mutates the baseline MapFS, runs load(), and asserts the
// returned error contains the expected substring.
func TestValidate_Rules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(fstest.MapFS)
		wantSubs []string // every substring must appear in the joined error
	}{
		{
			name: "bad folder name pattern",
			mutate: func(m fstest.MapFS) {
				// Add a non-conforming top-level folder.
				m["templates/badname/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: x
pick: single
options:
  - id: a
    display_name: a
    description: a
    file: a.md
`)}
				m["templates/badname/a.md"] = &fstest.MapFile{Data: []byte("a\n")}
			},
			wantSubs: []string{"folder name", "badname"},
		},
		{
			name: "options[].file missing on disk",
			mutate: func(m fstest.MapFS) {
				delete(m, "templates/20-infra/docker.md")
			},
			wantSubs: []string{"docker.md", "missing"},
		},
		{
			name: "orphan .md (not base.md under language)",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/orphan.md"] = &fstest.MapFile{Data: []byte("nope\n")}
			},
			wantSubs: []string{"orphan", "orphan.md"},
		},
		{
			name: "language sub-folder missing base.md",
			mutate: func(m fstest.MapFS) {
				delete(m, "templates/10-language/python/base.md")
			},
			wantSubs: []string{"base.md"},
		},
		{
			name: "pick not in {single, multi}",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: triple
required: false
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"pick", "triple"},
		},
		{
			name: "empty display_name on category",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: ""
pick: multi
required: false
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"display_name"},
		},
		{
			name: "empty display_name on option",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
options:
  - id: docker
    display_name: ""
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"display_name", "docker"},
		},
		{
			name: "option id not kebab",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
options:
  - id: Docker_Compose
    display_name: "DC"
    description: "DC"
    file: docker.md
`)}
			},
			wantSubs: []string{"id", "Docker_Compose"},
		},
		{
			name: "duplicate option id",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
  - id: docker
    display_name: "Docker again"
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"duplicate", "docker"},
		},
		{
			name: "single-pick default references unknown id",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/python/10-framework/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Framework"
pick: single
required: false
default: nosuchthing
options:
  - id: fastapi
    display_name: "FastAPI"
    description: "FastAPI"
    file: fastapi.md
`)}
			},
			wantSubs: []string{"default", "nosuchthing"},
		},
		{
			name: "multi-pick default references unknown id",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
default: [docker, nope]
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"default", "nope"},
		},
		{
			name: "multi-pick default with duplicate id",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
default: [docker, docker]
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: docker.md
`)}
			},
			wantSubs: []string{"default", "duplicate"},
		},
		{
			name: "language category has default",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
default: python
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)}
			},
			wantSubs: []string{"language", "default"},
		},
		{
			name: "language category required != true",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: false
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)}
			},
			wantSubs: []string{"language", "required"},
		},
		{
			name: "language category pick != single",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: multi
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
`)}
			},
			wantSubs: []string{"language", "single"},
		},
		{
			// H2 (review): the plan's Task 1.4 list explicitly
			// includes "Missing _index.yaml" — pin the rule to a
			// hygiene-style error rather than relying on
			// fs.ReadFile's I/O error shape.
			name: "missing _index.yaml in a category folder",
			mutate: func(m fstest.MapFS) {
				delete(m, "templates/20-infra/_index.yaml")
			},
			wantSubs: []string{"20-infra", "_index.yaml"},
		},
		{
			// M1 (review): pin the orphan-scan behaviour at the
			// language root. A stray .md directly under
			// `templates/10-language/` is not referenced by any
			// option and must fire the orphan rule.
			name: "orphan .md directly under templates/10-language",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/stray.md"] = &fstest.MapFile{Data: []byte("stray\n")}
			},
			wantSubs: []string{"orphan", "stray.md"},
		},
		{
			// PR #3 round-2 review: an empty options[].file slips
			// past the missing-file check because path.Join with ""
			// resolves back to the category directory, which exists
			// as a dir. Pin len(opt.File) > 0 directly with a clear
			// error so the offending option is named.
			name: "options[].file is empty string",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Infra"
pick: multi
required: false
options:
  - id: docker
    display_name: "Docker"
    description: "Docker"
    file: ""
`)}
			},
			wantSubs: []string{"docker", "file", "non-empty"},
		},
		{
			// PR #3 review observation 5: a zero-byte block file
			// would cause render.join to emit a "\n\n" gap with
			// nothing between, breaking the §4.4 trailing-newline
			// contract. Pin the rule at the validator so the
			// renderer doesn't have to defend against it.
			name: "option file: is zero bytes",
			mutate: func(m fstest.MapFS) {
				m["templates/20-infra/docker.md"] = &fstest.MapFile{Data: []byte{}}
			},
			wantSubs: []string{"docker.md", "empty", "zero"},
		},
		{
			// PR #4 review M2: a language option id that collides
			// with a top-level category id (e.g. language option
			// `infra` while the top-level `infra` category exists)
			// breaks the `em-dee show` disambiguation rule. Pin the
			// invariant at the validator.
			name: "language option id collides with top-level category id",
			mutate: func(m fstest.MapFS) {
				m["templates/10-language/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: python/base.md
  - id: infra
    display_name: "Infra-as-a-language"
    description: "Hypothetical collision"
    file: infra/base.md
`)}
				m["templates/10-language/infra/base.md"] = &fstest.MapFile{Data: []byte("infra-lang\n")}
			},
			wantSubs: []string{"infra", "collides", "language"},
		},
		{
			// M3 (review): the language option `file:` must be
			// `<opt.ID>/base.md` — pin the invariant that `walk`
			// silently depends on.
			name: "language option file: does not match <opt.ID>/base.md",
			mutate: func(m fstest.MapFS) {
				// Set file: to a path that disagrees with opt.ID.
				m["templates/10-language/_index.yaml"] = &fstest.MapFile{Data: []byte(`display_name: "Language"
pick: single
required: true
options:
  - id: python
    display_name: "Python"
    description: "Python"
    file: backend-python/base.md
`)}
				// Keep the actual file in place so the
				// missing-file rule isn't the one firing.
				m["templates/10-language/backend-python/base.md"] = &fstest.MapFile{Data: []byte("python base\n")}
			},
			wantSubs: []string{"language", "python", "file"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := validMapFS()
			tc.mutate(fsys)
			_, err := load(fsys, "templates")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			msg := err.Error()
			for _, want := range tc.wantSubs {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing substring %q", msg, want)
				}
			}
		})
	}
}
