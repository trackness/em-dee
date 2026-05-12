package cli

import (
	"errors"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/trackness/em-dee/internal/registry"
	"github.com/trackness/em-dee/internal/review"
)

// Options carries the build-time metadata and the (optional) Registry
// override the test suite uses to inject a fixture catalog instead of
// the production embedded one.
//
// Version, Commit, Date flow in from `cmd/em-dee/main.go` via the
// ldflags-populated `version`/`commit`/`date` variables.
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

	// updateHTTPClient and updateExePath are test seams for
	// `em-dee update --check`. Production leaves them nil; the
	// command falls back to a 15s-timeout http.Client and os.Executable.
	updateHTTPClient *http.Client
	updateExePath    string

	// updateApply is the test seam for the `em-dee update` install
	// pipeline (Task 6.4). Production leaves it nil; the command falls
	// back to defaultUpdater which calls selfupdate.Apply. Tests pass a
	// stub that captures the candidate binary bytes without touching
	// the running executable.
	updateApply updaterFunc

	// reviewRunner is the test seam for `em-dee generate`'s Phase 5
	// review path. Production leaves it nil; the generate command
	// falls back to a `&review.ExecRunner{}`. Tests inject a stub
	// runner (see internal/cli/review_test.go) so the parse / present
	// / failure-mode branches can be driven without a real `claude`
	// binary or network.
	reviewRunner review.Runner
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
		Args: cobra.NoArgs,
		// Subcommands set their own SilenceUsage/SilenceErrors as needed.
		// Keep the root permissive so `--help` and `version` print
		// cleanly without the usage footer on user-level errors.
		SilenceUsage: true,
	}

	root.AddCommand(newVersionCmd(opts))
	root.AddCommand(newListCmd(opts))
	root.AddCommand(newShowCmd(opts))
	root.AddCommand(newGenerateCmd(opts))
	root.AddCommand(newUpdateCmd(opts))

	// Make `em-dee` (no subcommand) behave as `em-dee generate` —
	// `em-dee` is the default flow. We re-register the generate flag
	// set onto root and install its RunE; cobra dispatch then treats
	// root and `generate` symmetrically. Each command keeps its own
	// generateFlags state so root and the explicit `generate`
	// subcommand cannot bleed values into each other.
	//
	// Tradeoff (per plan Task 3.5): an alternative is to set
	// rootCmd.RunE = generateCmd.RunE and share one flag struct, but
	// that ties the two together in subtle ways (e.g. cobra's flag
	// inheritance from root → child) and complicates reasoning. The
	// register-twice approach pays a tiny duplication cost in exchange
	// for two cleanly isolated entrypoints.
	registerGenerateFlagsAndRun(root, opts)

	return root
}

// Execute is the single entrypoint from `cmd/em-dee/main.go`. The
// signature is frozen so the version-embedding shape stays stable
// across phases.
//
// Exit code mapping: subcommands that need a non-1 exit code (notably
// `em-dee update --check`'s 0/1/2 three-state) return an
// *exitCodeError; Execute unwraps it. All other errors map to exit
// code 1.
func Execute(version, commit, date string) {
	root := NewRootCmd(Options{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
	if err := root.Execute(); err != nil {
		var ec *exitCodeError
		if errors.As(err, &ec) {
			exit(ec.code)
			return
		}
		// cobra has already written the error to stderr; just propagate
		// a non-zero exit code via os.Exit.
		exit(1)
	}
}
