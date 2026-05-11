package cli

import (
	"github.com/spf13/cobra"

	"github.com/trackness/em-dee/internal/registry"
)

// Options carries the build-time metadata and the (optional) Registry
// override the test suite uses to inject a fixture catalog instead of
// the production embedded one.
//
// Version, Commit, Date flow in from `cmd/em-dee/main.go` via the
// ldflags-populated `version`/`commit`/`date` variables (spec §12.7).
//
// Registry is normally nil — the command tree calls `registry.Load()`
// lazily so that subcommands which do not touch the catalog (e.g.
// `em-dee version`) never pay the cost. Tests pass a pre-built fixture
// Registry so output is exercised against a known shape.
type Options struct {
	Version  string
	Commit   string
	Date     string
	Registry *registry.Registry
}

// NewRootCmd builds the cobra command tree. It is the testable entry
// point: tests call this directly, set args via `cmd.SetArgs`, and
// capture output via `cmd.SetOut` / `cmd.SetErr`. Production code uses
// `Execute` which wraps this and runs against `os.Args` /
// `os.Stdout` / `os.Stderr`.
func NewRootCmd(opts Options) *cobra.Command {
	root := &cobra.Command{
		Use:   "em-dee",
		Short: "Generate CLAUDE.md files from a curated block catalog",
		Long: "em-dee generates CLAUDE.md files for new projects from a curated, " +
			"embedded set of opinionated markdown blocks. Run without args for " +
			"the interactive flow, or pass flags for non-interactive generation.",
		// Subcommands set their own SilenceUsage/SilenceErrors as needed.
		// Keep the root permissive so `--help` and `version` print
		// cleanly without the usage footer on user-level errors.
		SilenceUsage: true,
	}

	root.AddCommand(newVersionCmd(opts))

	return root
}

// Execute is the single entrypoint from `cmd/em-dee/main.go`. The
// signature is frozen per spec §12.7 so the version-embedding shape
// stays stable across phases.
func Execute(version, commit, date string) {
	root := NewRootCmd(Options{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
	if err := root.Execute(); err != nil {
		// cobra has already written the error to stderr; just propagate
		// a non-zero exit code via os.Exit.
		exit(1)
	}
}
