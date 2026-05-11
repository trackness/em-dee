package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/trackness/em-dee/internal/registry"
)

// loadFixtureRegistry returns the same registry the registry_test
// suite drives — a multi-language, multi-category fixture that exercises
// both single- and multi-pick categories and the language-subcategory
// shape. Kept as a helper so all CLI command tests run against an
// identical, known catalog.
func loadFixtureRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	root := os.DirFS("../registry/testdata/valid")
	reg, err := registry.LoadFS(root, "templates")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return reg
}

// TestList_Human asserts the tree-shaped human output covers every
// category and option, with defaults annotated.
func TestList_Human(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()

	// Top-level categories.
	for _, want := range []string{
		"language",
		"infra",
		"ci",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain category %q\n--- output ---\n%s", want, out)
		}
	}
	// Language nested sub-categories show up under python.
	for _, want := range []string{
		"python",
		"framework",
		"logging",
		"fastapi",
		"docker",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q\n--- output ---\n%s", want, out)
		}
	}
	// Defaults are flagged.
	if !strings.Contains(out, "(default)") {
		t.Errorf("expected output to mark defaults with '(default)'\n--- output ---\n%s", out)
	}
}

// TestList_JSON asserts the documented JSON shape: a list of
// categories with id/display_name/pick/required/default/options/
// subcategories.
func TestList_JSON(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got listPayload
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}

	if len(got.Categories) == 0 {
		t.Fatalf("expected categories, got none")
	}

	// First top-level category should be `language` (folder prefix 10).
	if got.Categories[0].ID != "language" {
		t.Errorf("first category: got %q want language", got.Categories[0].ID)
	}
	if !got.Categories[0].Required {
		t.Errorf("language must be required")
	}
	if got.Categories[0].Pick != "single" {
		t.Errorf("language pick: got %q want single", got.Categories[0].Pick)
	}

	// Locate the python language option and check it carries
	// subcategories.
	var pyOpt *listOption
	for i := range got.Categories[0].Options {
		if got.Categories[0].Options[i].ID == "python" {
			pyOpt = &got.Categories[0].Options[i]
			break
		}
	}
	if pyOpt == nil {
		t.Fatalf("python language option not found in JSON")
	}
	if len(pyOpt.Subcategories) == 0 {
		t.Errorf("python option has no subcategories in JSON")
	}
	// fastapi is the framework default in the fixture.
	var foundFastapi bool
	for _, sub := range pyOpt.Subcategories {
		if sub.ID == "framework" && sub.DefaultSingle == "fastapi" {
			foundFastapi = true
		}
	}
	if !foundFastapi {
		t.Errorf("expected python.framework default to be fastapi\nraw: %s", buf.String())
	}

	// Infra is multi-pick with default [docker].
	var infra *listCategory
	for i := range got.Categories {
		if got.Categories[i].ID == "infra" {
			infra = &got.Categories[i]
			break
		}
	}
	if infra == nil {
		t.Fatalf("infra category not found")
	}
	if infra.Pick != "multi" {
		t.Errorf("infra pick: got %q want multi", infra.Pick)
	}
	if len(infra.DefaultMulti) != 1 || infra.DefaultMulti[0] != "docker" {
		t.Errorf("infra default_multi: got %v want [docker]", infra.DefaultMulti)
	}
}
