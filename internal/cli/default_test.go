package cli

import (
	"bytes"
	"testing"
)

// TestRoot_DefaultsToGenerate asserts that `em-dee --language=python
// --use-defaults --dry-run` (no `generate` subcommand) produces the
// same output as the explicit form. Locks in the Task 3.5 contract.
func TestRoot_DefaultsToGenerate(t *testing.T) {
	reg := loadFixtureRegistry(t)

	runRoot := func(args []string) string {
		root := NewRootCmd(Options{Registry: reg})
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute %v: %v", args, err)
		}
		return buf.String()
	}

	implicit := runRoot([]string{"--language=python", "--use-defaults", "--dry-run"})
	explicit := runRoot([]string{"generate", "--language=python", "--use-defaults", "--dry-run"})

	if implicit != explicit {
		t.Errorf("implicit and explicit generate output diverge\nimplicit:\n%s\nexplicit:\n%s", implicit, explicit)
	}
}

// TestRoot_NoArgsErrorsAboutLanguage asserts that bare `em-dee` runs
// generate (which then errors clearly about missing language in
// Phase 3). The interactive flow lands in Phase 4 (Task 4.4).
func TestRoot_NoArgsErrorsAboutLanguage(t *testing.T) {
	reg := loadFixtureRegistry(t)
	root := NewRootCmd(Options{Registry: reg})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for bare em-dee in Phase 3")
	}
}
