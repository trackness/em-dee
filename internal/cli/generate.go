package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/trackness/em-dee/internal/registry"
	"github.com/trackness/em-dee/internal/render"
)

// generateFlags holds the wired-up state for one invocation. The
// per-category flags live in a map keyed by the registry's selection
// key — top-level category id (`infra`) or `<lang-id>.<sub-id>`
// (`python.framework`). This shape feeds directly into
// registry.ResolveSelection.
//
// Tri-state handling: pflag's `Changed()` predicate lets us tell
// "user passed --foo=" (changed, empty value) apart from "user didn't
// pass --foo at all" (not changed). The non-interactive flag→Picks
// builder consults Changed() to seed the map only for flags the user
// actually mentioned.
//
// Flag-name format tradeoff (per plan Task 3.4): pflag does not
// support dots in long flag names because dots collide with its
// shorthand-grouping. We use dashes for namespaced flags (e.g.
// `--python-framework` rather than `--python.framework`), but the
// resolver still consumes dotted keys. We never parse the flag name
// back into a selection key — both halves are known at registration
// time, so we record the mapping then and consult it at consumption
// time. This makes hyphenated language ids (e.g. `typescript-node`)
// round-trip by construction. See selectionKey below.
type generateFlags struct {
	out         string
	force       bool
	dryRun      bool
	useDefaults bool
	// review-related flags are accepted-but-unused in Phase 3 per the
	// plan; behaviour lands in Phase 5.
	noReview      bool
	review        bool
	reviewOut     string
	reviewTimeout string

	// values holds the raw string captured by each per-category flag.
	// The map key is the flag's *registered name* (with dashes, see
	// the flag-name tradeoff above).
	values map[string]*string

	// selectionKey maps a registered flag name (e.g. `python-framework`
	// or `typescript-node-logging`) to the dotted selection key the
	// resolver consumes (e.g. `python.framework`,
	// `typescript-node.logging`). Built at flag-registration time in
	// registerCategoryFlags where both halves are already known —
	// avoids the parse-the-name-back ambiguity that breaks hyphenated
	// language ids when the first dash isn't the namespace separator.
	selectionKey map[string]string

	// regLoadErr captures a registry-load failure from
	// registerGenerateFlagsAndRun. RunE returns this as its first act
	// so the user gets a clear error instead of a half-populated flag
	// set. Nil on the happy path. `--help` still works (cobra prints
	// the registered flags + the warning we splice into cmd.Long).
	regLoadErr error
}

// newGenerateCmd builds `em-dee generate`. The flag set is built
// dynamically from the registry so adding a category or language only
// touches the templates filesystem — there are no per-category Go
// edits required to expose a new flag.
func newGenerateCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CLAUDE.md from flag-supplied picks",
		Args:  cobra.NoArgs,
		// Don't print usage on user-level errors (unknown option ids,
		// missing required category). Usage noise drowns the actual
		// error in scripted use.
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	registerGenerateFlagsAndRun(cmd, opts)
	return cmd
}

// registerGenerateFlagsAndRun wires the generate flag set onto an
// arbitrary *cobra.Command and installs the pipeline as its RunE. It
// is the seam Task 3.5 uses to make the root command behave as
// `generate` when invoked with no subcommand: the root gets the same
// flags and the same RunE, so the two entrypoints are byte-equivalent
// (the integration test `TestRoot_DefaultsToGenerate` locks this in).
//
// The flag-state struct lives in this closure so each command gets
// its own state — sharing one across root and generate would mean a
// flag set on root could leak into a subsequent generate invocation,
// which cobra's child-vs-root flag resolution makes hard to reason
// about. One generateFlags per command, period.
func registerGenerateFlagsAndRun(cmd *cobra.Command, opts Options) {
	flags := &generateFlags{
		values:       map[string]*string{},
		selectionKey: map[string]string{},
	}

	// Behaviour flags.
	cmd.Flags().StringVar(&flags.out, "out", "CLAUDE.md", "path to write CLAUDE.md")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing file (backed up to CLAUDE.md.bak.<unix-ts>)")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "write to stdout instead of disk; skips existing-file check")
	cmd.Flags().BoolVar(&flags.useDefaults, "use-defaults", false, "accept all defaults; only requires --language (or interactive, Phase 4)")

	// Accepted-but-unused review flags. Wired in Phase 5; declaring
	// them now avoids "unknown flag" errors when users iterate on
	// scripts before the review path lands.
	cmd.Flags().BoolVar(&flags.noReview, "no-review", false, "skip the claude review (Phase 5)")
	cmd.Flags().BoolVar(&flags.review, "review", false, "run the claude review (Phase 5)")
	cmd.Flags().StringVar(&flags.reviewOut, "review-out", "", "write review JSON to this path (Phase 5)")
	cmd.Flags().StringVar(&flags.reviewTimeout, "review-timeout", "", "review subprocess timeout (Phase 5)")
	_ = cmd.Flags().MarkHidden("no-review")
	_ = cmd.Flags().MarkHidden("review")
	_ = cmd.Flags().MarkHidden("review-out")
	_ = cmd.Flags().MarkHidden("review-timeout")

	// Per-category flags, derived from the registry. We resolve the
	// registry once at command-construction time. If Load() fails
	// (production embedded FS broken, schema drift, etc.), record the
	// error on `flags.regLoadErr` so RunE can surface it as its first
	// act — silently half-populating the flag set at `--help` time
	// would leave the user staring at a flag list missing
	// `--language` with no diagnostic, which is worse than a clear
	// failure.
	//
	// `--help` itself still works (cobra prints whatever flags were
	// registered) and we splice a one-liner into cmd.Long so the user
	// can tell the catalog failed to load even when they never reach
	// RunE.
	if reg, err := resolveRegistry(opts); err == nil {
		registerCategoryFlags(cmd.Flags(), reg, flags)
	} else {
		flags.regLoadErr = err
		warning := fmt.Sprintf("\n\nWARNING: failed to load embedded registry; per-category flags are unavailable. Underlying error: %v", err)
		if cmd.Long != "" {
			cmd.Long += warning
		} else {
			cmd.Long = cmd.Short + warning
		}
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// Surface a registry-load failure recorded at construction
		// time before doing any other work — the rest of the pipeline
		// assumes a populated category-flag set.
		if flags.regLoadErr != nil {
			return fmt.Errorf("load registry: %w", flags.regLoadErr)
		}
		reg, err := resolveRegistry(opts)
		if err != nil {
			return err
		}
		return runGenerate(cmd, reg, flags)
	}
}

// registerCategoryFlags walks the registry and registers one
// StringVar per category (top-level and language-nested). The flag
// names use dashes throughout per the flag-name format tradeoff.
//
// Each flag's bound pointer is stored in flags.values keyed by the
// flag name. flags.selectionKey records the parallel mapping from
// flag name to the dotted selection-key the resolver consumes —
// recorded at registration time where both halves are still
// independently known, so hyphenated language ids (e.g.
// `typescript-node`) round-trip correctly.
func registerCategoryFlags(fs *pflag.FlagSet, reg *registry.Registry, flags *generateFlags) {
	for _, cat := range reg.Categories {
		flagName := cat.ID
		v := new(string)
		flags.values[flagName] = v
		flags.selectionKey[flagName] = cat.ID
		fs.StringVar(v, flagName, "", flagUsage(cat))

		// Language gets its sub-categories registered with the
		// `<lang>-<sub>` form, one per option in the language category.
		if cat.ID == registry.LanguageCategoryID {
			for _, opt := range cat.Options {
				subs := cat.Subcategories[opt.ID]
				for _, sub := range subs {
					subFlagName := opt.ID + "-" + sub.ID
					sv := new(string)
					flags.values[subFlagName] = sv
					flags.selectionKey[subFlagName] = opt.ID + "." + sub.ID
					fs.StringVar(sv, subFlagName, "", flagUsage(sub))
				}
			}
		}
	}
}

// flagUsage builds a short help string for a category flag. Lists the
// available option ids so `--help` is self-documenting.
func flagUsage(cat *registry.Category) string {
	ids := make([]string, 0, len(cat.Options))
	for _, opt := range cat.Options {
		ids = append(ids, opt.ID)
	}
	def := ""
	switch cat.Pick {
	case registry.PickSingle:
		if cat.DefaultSingle != "" {
			def = fmt.Sprintf(" [default %s]", cat.DefaultSingle)
		}
	case registry.PickMulti:
		if len(cat.DefaultMulti) > 0 {
			def = fmt.Sprintf(" [default %s]", strings.Join(cat.DefaultMulti, ","))
		}
	}
	return fmt.Sprintf("%s (%s)%s: %s",
		cat.DisplayName, cat.Pick, def, strings.Join(ids, "|"))
}

// runGenerate is the actual pipeline. Separating it from the cobra
// closure keeps RunE small and lets tests poke individual stages
// later if needed.
func runGenerate(cmd *cobra.Command, reg *registry.Registry, flags *generateFlags) error {
	// Build the selection map from flags that the user actually set
	// (Changed()==true). This is the tri-state seam: an unchanged
	// flag is "unset" (omitted from the map); a changed flag with
	// any value (including empty) becomes an entry.
	selection := map[string]any{}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		key, ok := flags.selectionKey[f.Name]
		if !ok {
			return // behaviour flag, not a category flag
		}
		selection[key] = f.Value.String()
	})

	// Phase 3 has no interactive flow. If language is unset and
	// --use-defaults wasn't given, error clearly.
	if _, hasLang := selection[registry.LanguageCategoryID]; !hasLang && !flags.useDefaults {
		return fmt.Errorf("language is required; pass --language=<id> or run interactively (interactive flow lands in Phase 4)")
	}

	// Resolve flag map → Picks. ResolveSelection enforces option-id
	// validity and tri-state required-empty rejection.
	picks, err := registry.ResolveSelection(reg, selection)
	if err != nil {
		return err
	}

	// Fill defaults for omitted categories.
	picks = registry.ApplyDefaults(picks, reg)

	// Required-category check after defaults: a required category
	// with no value at this point (no default, no user pick) is a
	// hard error. ResolveSelection already rejected explicit-empty
	// required, so this catches the "omitted entirely" case.
	for _, cat := range reg.Categories {
		if !cat.Required {
			continue
		}
		v, ok := picks.Values[cat.ID]
		if !ok || v == nil {
			return fmt.Errorf("%s: required category not set and no default available", cat.ID)
		}
		// Explicit-empty already rejected in ResolveSelection; defensive:
		if cat.Pick == registry.PickSingle && (v.Single == nil || *v.Single == "") {
			return fmt.Errorf("%s: required category resolved to empty", cat.ID)
		}
		if cat.Pick == registry.PickMulti && (v.Multi == nil || len(*v.Multi) == 0) {
			return fmt.Errorf("%s: required category resolved to empty", cat.ID)
		}
	}

	// Render to bytes.
	content, err := render.Render(reg, picks)
	if err != nil {
		return err
	}

	// Dry-run: stdout, skip existing-file check.
	if flags.dryRun {
		_, err := cmd.OutOrStdout().Write(content)
		return err
	}

	// Existing-file handling per spec §6.
	if err := writeOutput(cmd.ErrOrStderr(), flags.out, content, flags.force); err != nil {
		return err
	}
	return nil
}

// writeOutput implements the existing-file rules from spec §6:
//   - error if target exists and !force
//   - rename existing to CLAUDE.md.bak.<unix-ts> when force, then write
//   - print backup path to stderr
//   - never delete, always rename
func writeOutput(stderr io.Writer, path string, content []byte, force bool) error {
	if _, err := os.Stat(path); err == nil {
		// File exists.
		if !force {
			return fmt.Errorf("%s exists. Pass --force to overwrite (current file will be backed up) or --out to write elsewhere", path)
		}
		backup := path + ".bak." + strconv.FormatInt(time.Now().Unix(), 10)
		if err := os.Rename(path, backup); err != nil {
			return fmt.Errorf("backup %s: %w", path, err)
		}
		fmt.Fprintf(stderr, "backed up existing %s to %s\n", path, backup)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
