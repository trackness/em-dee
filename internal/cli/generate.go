package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/trackness/em-dee/internal/registry"
	"github.com/trackness/em-dee/internal/render"
	"github.com/trackness/em-dee/internal/review"
	"github.com/trackness/em-dee/internal/tui"
)

// generateFlags holds the wired-up state for one invocation. Dispatch 1
// of the schema-restructure work removed the per-category flag layer:
// the only surface left is `--language=<id>` (the single exception that
// skips the interactive language prompt) plus the behaviour flags.
// Form 2's per-category selection moves entirely into the interactive
// flow in Dispatch 3.
type generateFlags struct {
	out         string
	force       bool
	dryRun      bool
	useDefaults bool
	// Review-related flags. `noReview` flips the default `--review=true`
	// off. `reviewOut` is an optional path the parsed JSON (or the
	// unstructured sentinel shape) is written to. `reviewTimeout`
	// overrides the default subprocess deadline; empty means "use the
	// default".
	noReview      bool
	review        bool
	reviewOut     string
	reviewTimeout string

	// language is the one surviving category-level flag. It is the
	// non-interactive entry point that skips form 1 of the interactive
	// flow when the language is known up front. Empty string means
	// "not supplied"; we use cobra's Changed() predicate to tell
	// "--language=python" (set) from "no --language" (unset) and
	// "--language=" (explicit empty) from both.
	language string
}

// newGenerateCmd builds `em-dee generate`. The flag set is now static —
// the per-category flag wiring (one flag per registry category) was
// removed in Dispatch 1; adding or renaming a category no longer needs
// a Go edit either way, since selection now flows entirely through the
// interactive form.
func newGenerateCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CLAUDE.md from interactive picks (or --language for the non-interactive happy path)",
		Args:  cobra.NoArgs,
		// Don't print usage on user-level errors (missing required
		// category, etc.). Usage noise drowns the actual error in
		// scripted use.
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	registerGenerateFlagsAndRun(cmd, opts)
	return cmd
}

// registerGenerateFlagsAndRun wires the generate flag set onto an
// arbitrary *cobra.Command and installs the pipeline as its RunE. It
// is the seam that makes the root command behave as `generate` when
// invoked with no subcommand: the root gets the same flags and the
// same RunE, so the two entrypoints are byte-equivalent (the
// integration test `TestRoot_DefaultsToGenerate` locks this in).
//
// The flag-state struct lives in this closure so each command gets
// its own state — sharing one across root and generate would mean a
// flag set on root could leak into a subsequent generate invocation,
// which cobra's child-vs-root flag resolution makes hard to reason
// about. One generateFlags per command, period.
func registerGenerateFlagsAndRun(cmd *cobra.Command, opts Options) {
	flags := &generateFlags{}

	// Behaviour flags.
	cmd.Flags().StringVar(&flags.out, "out", "CLAUDE.md", "path to write CLAUDE.md")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing file (backed up to CLAUDE.md.bak.<unix-ts>)")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "write to stdout instead of disk; skips existing-file check")
	cmd.Flags().BoolVar(&flags.useDefaults, "use-defaults", false, "accept registry defaults for every category except --language (which must still be supplied non-interactively)")

	// Review flags. `--review` defaults to true; users pass `--no-review`
	// to skip. cobra synthesises `--no-review` from the `--review`
	// BoolVar via the "--no-<name>" convention, but we expose both
	// names explicitly so `--help` lists them.
	cmd.Flags().BoolVar(&flags.review, "review", true, "run the claude review after writing")
	cmd.Flags().BoolVar(&flags.noReview, "no-review", false, "skip the claude review (overrides --review)")
	cmd.Flags().StringVar(&flags.reviewOut, "review-out", "", "write the parsed review JSON to this path")
	cmd.Flags().StringVar(&flags.reviewTimeout, "review-timeout", "", "override the review subprocess deadline (Go duration, default 60s)")

	// --language is the one surviving category-level flag. It skips
	// form 1 of the interactive flow when the language is known. All
	// other category picks come from the interactive form in Dispatch
	// 3 (or registry defaults under --use-defaults).
	cmd.Flags().StringVar(&flags.language, "language", "", "language id (e.g. python, go); skips the interactive language prompt")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		reg, err := resolveRegistry(opts)
		if err != nil {
			return err
		}
		return runGenerate(cmd, reg, flags, opts)
	}
}

// runGenerate is the actual pipeline. Separating it from the cobra
// closure keeps RunE small and lets tests poke individual stages
// later if needed. `opts` flows through so the optional review-runner
// injection (Options.reviewRunner) reaches finishGenerate.
func runGenerate(cmd *cobra.Command, reg *registry.Registry, flags *generateFlags, opts Options) error {
	// Build the initial selection map from --language only — the per-
	// category flags are gone. We use cobra's Changed() predicate so
	// "no --language" (unset) and "--language=" (explicit empty) are
	// distinguishable, since the latter is still meaningful: it lets
	// the user request a clear "language is required" error in
	// scripted mode rather than the interactive prompt.
	selection := map[string]any{}
	if cmd.Flags().Changed("language") {
		selection[registry.LanguageCategoryID] = flags.language
	}

	// Interactive dispatch. We enter interactive mode when --language is
	// unset AND stdin/stdout are TTYs. --use-defaults in a TTY context
	// still runs form 1 (the language category has no default), then
	// skips form 2 and lets ApplyDefaults fill the rest. On a non-TTY
	// (pipe, CI, redirect), fall through to the hard-error path so
	// scripted use stays predictable.
	if _, hasLang := selection[registry.LanguageCategoryID]; !hasLang && isInteractive() {
		picks, err := runInteractive(reg, flags.useDefaults)
		if err != nil {
			if errors.Is(err, tui.ErrCancelled) {
				// Distinct from a generic failure: the user chose to
				// abort. Print a brief note and exit 130 (POSIX
				// convention for SIGINT-style cancellation; lets
				// shell loops `set -e` the same way ^C would have).
				fmt.Fprintln(cmd.ErrOrStderr(), "cancelled")
				return &exitCodeError{code: 130, msg: "cancelled by user"}
			}
			return err
		}
		return finishGenerate(cmd, reg, picks, flags, opts)
	}

	// Non-interactive path. `--language` is required: `--use-defaults`
	// is defined as "prompt only for language" (still interactive). On
	// a non-TTY, neither branch can interact, so we surface a single
	// clear message up front instead of letting the pipeline hit the
	// deeper "required category not set" error later — scripts get one
	// diagnostic, not two.
	if _, hasLang := selection[registry.LanguageCategoryID]; !hasLang {
		if flags.useDefaults {
			return fmt.Errorf("--use-defaults still requires --language=<id> in non-interactive mode; interactive mode needs a TTY")
		}
		return fmt.Errorf("language is required; pass --language=<id> or run interactively (interactive needs a TTY)")
	}

	// Resolve flag map → Picks. ResolveSelection enforces option-id
	// validity and required-empty rejection (`--language=` on the
	// required language category is still a hard error).
	picks, err := registry.ResolveSelection(reg, selection)
	if err != nil {
		return err
	}

	// Fill defaults for omitted categories.
	picks = registry.ApplyDefaults(picks, reg)

	return finishGenerate(cmd, reg, picks, flags, opts)
}

// runInteractive drives the multi-phase huh flow under Dispatch 3's
// three-level-capable schema:
//
//  1. RunLanguageForm — always runs (language category has no default).
//  2. RunTypeForm — runs iff the chosen language has a container
//     sub-category (e.g. `python/10-type/`). Returns "" when no
//     container exists; the production catalog hits that path today.
//  3. ApplyDefaults — seeds the rest from registry defaults.
//  4. RunScopeForm — runs unless --use-defaults short-circuits.
//
// Returns the final Picks or an error (notably tui.ErrCancelled on
// user abort or No-on-confirm).
//
// `--use-defaults` rule (documented contract):
//
//   - phase 1 always runs (language is required, no default).
//   - phase 2 (type form) runs iff the container category has no
//     usable default. The container's `required:` and `default:`
//     drive the test: if a default is present, accept it silently; if
//     not, surface the prompt regardless of --use-defaults so the user
//     gets to express the type-conditional pick once. (Type-conditional
//     content has no sensible silent default — guessing "every project
//     is a CLI" is the wrong shape.) Cancellation in phase 2 maps the
//     same way as phase 1: ErrCancelled bubbles up.
//   - phase 3 (scope form) is skipped under --use-defaults.
//
// UX tradeoff (preserved from PR #5 review L2 and revisited under
// Dispatch 3): the --use-defaults path commits the file immediately
// after the last interactive phase — no confirm group, no preview of
// what's about to be written. The safety net is the existing-file
// rule, which still rejects a write over an existing CLAUDE.md
// without --force. If users report the no-preview UX as sharp in
// practice, the v2 move is to surface the render-order summary as a
// final-line confirmation before the write rather than re-adding the
// confirm group (which would put us on the path to ignoring
// --use-defaults).
func runInteractive(reg *registry.Registry, useDefaults bool) (registry.Picks, error) {
	lang, err := tui.RunLanguageForm(reg)
	if err != nil {
		return registry.Picks{}, err
	}

	// Seed picks with the language pick, then apply defaults so any
	// container category under the language has its default cell
	// filled. Ordering constraint (load-bearing): the language pick
	// MUST be in picks before ApplyDefaults so the chosen-language
	// sub-tree's defaults flow — see registry/defaults.go's
	// container-descent logic. Applying defaults BEFORE phase 2 is the
	// new wrinkle: it gives the type form a seeded binding so the
	// huh.Select lands on the registry default rather than the first
	// option (huh v2 first-option pre-fill quirk).
	picks := registry.NewPicks()
	picks.Values[registry.LanguageCategoryID] = registry.NewSingle(lang)
	picks = registry.ApplyDefaults(picks, reg)

	// Phase 2: type pick when a container sub-category exists.
	typeID, containerID, ranTypeForm, err := runTypePhase(reg, lang, picks, useDefaults)
	if err != nil {
		return registry.Picks{}, err
	}
	if ranTypeForm && typeID != "" && containerID != "" {
		// The user expressed a type pick. Seed it into picks (it may
		// be the same as the default or different) and re-apply
		// defaults so the chosen option's subtree's defaults populate
		// (the prior ApplyDefaults walked only the default-option's
		// subtree; a different chosen option needs a second pass).
		picks.Values[lang+"."+containerID] = registry.NewSingle(typeID)
		picks = registry.ApplyDefaults(picks, reg)
	}

	if useDefaults {
		return picks, nil
	}

	// Resolve the effective typeID for phase 3 from picks (covers
	// both the "user picked" and the "ApplyDefaults filled" cases).
	effectiveType := ""
	if containerID != "" {
		if v := picks.Values[lang+"."+containerID]; v != nil && v.Single != nil {
			effectiveType = *v.Single
		}
	}

	return tui.RunScopeForm(reg, lang, effectiveType, picks)
}

// runTypePhase is the phase-2 helper extracted from runInteractive so
// the --use-defaults short-circuit logic is readable in one place.
// Returns (typeID, containerID, ranTypeForm, err):
//
//   - containerID is the id of the container sub-category under the
//     language (e.g. `type`); empty when no container exists.
//   - typeID is the chosen container-option id; only valid when
//     ranTypeForm is true.
//   - ranTypeForm reports whether the type form actually ran (or was
//     short-circuited via the useDefaults+default path).
//   - err is non-nil on hard failure or user cancellation (ErrCancelled
//     wrapped via tui).
//
// The short-circuit rule (documented contract): when useDefaults is
// set AND the container has a default, we don't prompt — the prior
// ApplyDefaults already populated the cell from the registry default.
// When useDefaults is set AND the container has no default, we prompt
// anyway — the contract is "skip everything except the picks you
// genuinely can't infer from defaults", and a no-default container is
// by construction the second case.
//
// `initial` is the current picks state, used to seed the type form's
// bound pointer so the highlighted option matches what's already in
// picks (typically the registry default after ApplyDefaults has run).
func runTypePhase(reg *registry.Registry, lang string, initial registry.Picks, useDefaults bool) (string, string, bool, error) {
	container := tui.FindContainerSub(reg, lang)
	if container == nil {
		return "", "", false, nil
	}
	if useDefaults && container.DefaultSingle != "" {
		return "", container.ID, false, nil
	}
	typeID, err := tui.RunTypeForm(reg, lang, initial)
	if err != nil {
		return "", container.ID, false, err
	}
	return typeID, container.ID, true, nil
}

// finishGenerate is the shared tail of both the interactive and
// non-interactive paths: required-category check, render, write (or
// dry-run to stdout), success line, then the claude review.
func finishGenerate(cmd *cobra.Command, reg *registry.Registry, picks registry.Picks, flags *generateFlags, opts Options) error {
	// Required-category check after defaults: a required category
	// with no value at this point (no default, no user pick) is a
	// hard error. ResolveSelection already rejected required-empty,
	// so this catches the "omitted entirely" case.
	for _, cat := range reg.Categories {
		if !cat.Required {
			continue
		}
		v, ok := picks.Values[cat.ID]
		if !ok || v == nil {
			return fmt.Errorf("%s: required category not set and no default available", cat.ID)
		}
		// Defensive: a non-nil pointer to an empty value can still
		// arise for required categories if a future caller bypasses
		// ResolveSelection.
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

	// --review-timeout parse upfront so we fail before writing if the
	// duration string is bogus. Empty means "use the default".
	timeout := review.DefaultTimeout
	if flags.reviewTimeout != "" {
		d, err := time.ParseDuration(flags.reviewTimeout)
		if err != nil {
			return fmt.Errorf("--review-timeout: %w", err)
		}
		timeout = d
	}

	// Dry-run: stdout, skip existing-file check and success line and
	// review (the file isn't written, so review would be meaningless).
	// The success line is a side-channel for human users; piping
	// dry-run output into another tool shouldn't get it mixed in.
	if flags.dryRun {
		_, err := cmd.OutOrStdout().Write(content)
		return err
	}

	// Existing-file handling.
	if err := writeOutput(cmd.ErrOrStderr(), flags.out, content, flags.force); err != nil {
		return err
	}

	// Success line goes to stderr (out of scripted-capture's way) so
	// `em-dee generate > out` doesn't produce a CLAUDE.md
	// path-confused tee.
	blocks := countBlocks(reg, picks)
	fmt.Fprintln(cmd.ErrOrStderr(), tui.SuccessLine(flags.out, blocks, len(content)))

	// Review. --no-review wins over --review (which is on by default),
	// so any --no-review short-circuits.
	if flags.noReview {
		return nil
	}
	return runReview(cmd, content, flags, opts, timeout)
}

// runReview drives the review flow: pick the runner (from Options or
// a default ExecRunner), build the prompt, call the runner under a
// timeout, parse, present, write `--review-out`, and return either
// nil (exit 0) or an exitCodeError{code: 2} on verdict:problems.
//
// Failure-mode mapping — `claude` not on PATH, timeout, non-zero
// exit — all print a note on stderr and return nil so exit stays 0.
// Parse failure (tier 3) also returns nil.
//
// Exit-code tradeoff (plan Task 5.5): rather than calling os.Exit(2)
// from inside RunE — which would skip cobra's deferred cleanup and
// muddy test invocations — we return a `*exitCodeError{code: 2}`. The
// root-level Execute (already in place for `em-dee update --check`)
// unwraps it and calls os.Exit(2). cobra's default exit-on-error is 1,
// so the exitCodeError seam is the cleanest way to thread exit 2
// through.
func runReview(cmd *cobra.Command, content []byte, flags *generateFlags, opts Options, timeout time.Duration) error {
	runner := opts.reviewRunner
	if runner == nil {
		runner = &review.ExecRunner{}
	}

	prompt := review.BuildPrompt(content)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stdout, runnerStderr, err := runner.Run(ctx, prompt)

	// Failure modes. Each prints a note and returns nil → exit 0.
	if err != nil {
		switch {
		case errors.Is(err, review.ErrClaudeNotFound):
			fmt.Fprintln(cmd.ErrOrStderr(), "note: claude CLI not found; skipping review")
		case errors.Is(err, review.ErrTimeout):
			fmt.Fprintf(cmd.ErrOrStderr(), "note: claude review timed out after %s\n", timeout)
		default:
			fmt.Fprintln(cmd.ErrOrStderr(), "claude review failed:")
			// Trailing-newline coalescing: the runner's stderr may or may
			// not end with a newline. We always emit exactly one so the
			// next stderr line ("note: ..." or shell prompt in dev) sits
			// on its own line. Fprintln always appends \n, so we add one
			// only when the captured stderr didn't end with one already.
			if len(runnerStderr) > 0 {
				// Write is best-effort: a failed stderr write means the
				// caller's pipe is broken and nothing useful we'd print
				// afterwards would land either. Discard explicitly so
				// errcheck stays happy without silently hiding it via _.
				_, _ = cmd.ErrOrStderr().Write(runnerStderr)
				if runnerStderr[len(runnerStderr)-1] != '\n' {
					fmt.Fprintln(cmd.ErrOrStderr())
				}
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
			}
		}
		return nil
	}

	// Parse the runner output. Parse() never returns an error — tier 3
	// is the fallback. So the only failure surface here is os write
	// from Present, which we ignore (presentation is best-effort).
	res, _ := review.Parse(stdout)

	// Detect terminal width for Present's wrap. Falls back to 100 cols
	// (review.Present handles termWidth <= 0). We probe stderr's fd
	// because that's where Present's output is being sent.
	w := terminalWidth(cmd.ErrOrStderr())

	review.Present(cmd.ErrOrStderr(), res, w)

	// --review-out. The on-disk shape differs slightly from the
	// in-memory ReviewResult: tier 3 produces the unstructured sentinel
	// JSON (verdict / summary / raw / issues) verbatim.
	//
	// Soft-note-on-write-failure (deliberate choice): the exit-code
	// contract is keyed off the verdict only. `--review-out` write
	// failure is a developer-facing diagnostic rather than a CI-grade
	// contract. If a CI pipeline grows a hard dependency on the file
	// existing, this is the place to flip to fail-loud (return an
	// *exitCodeError{code: 1} instead of writing a note and falling
	// through). Until that user shows up, the failure-tolerant shape
	// avoids surprising scripted callers whose primary signal is the
	// verdict.
	if flags.reviewOut != "" {
		if err := writeReviewOut(flags.reviewOut, res); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "note: writing --review-out failed: %v\n", err)
			// Fall through — failing to write the side-file should not
			// invalidate the verdict-based exit-code mapping below.
		}
	}

	// Exit code mapping by verdict.
	switch res.Verdict {
	case review.VerdictProblems:
		return &exitCodeError{code: 2, msg: "claude review reported problems"}
	default:
		return nil
	}
}

// writeReviewOut serialises a ReviewResult to JSON at path. For tier
// 1/2 results, the natural ReviewResult shape is used (the `raw` field
// is omitted via `omitempty` when empty). For tier 3 (unstructured),
// the sentinel shape is produced verbatim — `issues` is guaranteed to
// be a JSON array (`[]`) even if the parsed value left it nil, so
// consumers that always type the field as an array don't break.
func writeReviewOut(path string, res review.ReviewResult) error {
	// Marshal in a stable shape. The struct already encodes verdict /
	// summary / issues / raw via JSON tags; we just need to ensure
	// `issues` is non-nil so it serialises as `[]` not `null`.
	out := res
	if out.Issues == nil {
		out.Issues = []review.Issue{}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// terminalWidth returns the column count for w if it's a TTY, else 0.
// The cli layer is the right place to do this — the review package
// stays I/O-pure by taking termWidth as an int.
func terminalWidth(w io.Writer) int {
	type fdHaver interface{ Fd() uintptr }
	f, ok := w.(fdHaver)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// countBlocks returns the number of block files that contributed to
// the rendered content for `picks`. Mirrors the renderer's walk order
// so the success line's count matches what's actually in the file.
//
// Walk order matches render.renderScope: each container category
// contributes the chosen option's scope base.md (when its `file:`
// points at a base.md) plus its subtree, recursively.
func countBlocks(reg *registry.Registry, picks registry.Picks) int {
	n := 0
	for _, cat := range reg.Categories {
		n += countCategoryTree(cat, picks, "")
	}
	return n
}

// countCategoryTree mirrors render.renderScope's single-category leg.
// Leaf categories contribute their selected option(s); container
// categories contribute the chosen option's scope base.md (when its
// `file:` points at a base.md) plus the recursive subtree of the
// chosen option.
func countCategoryTree(cat *registry.Category, picks registry.Picks, prefix string) int {
	key := cat.ID
	if prefix != "" {
		key = prefix + "." + cat.ID
	}
	if !cat.IsContainer {
		return countCategoryBlocks(cat, picks.Values[key])
	}

	v := picks.Values[key]
	if v == nil || v.Single == nil || *v.Single == "" {
		return 0
	}
	chosen := *v.Single

	n := 0
	for _, opt := range cat.Options {
		if opt.ID != chosen {
			continue
		}
		if strings.HasSuffix(opt.File, "/base.md") {
			n++
		}
		break
	}

	childPrefix := chosen
	if prefix != "" {
		childPrefix = prefix + "." + chosen
	}
	for _, sub := range cat.Subcategories[chosen] {
		n += countCategoryTree(sub, picks, childPrefix)
	}
	return n
}

// countCategoryBlocks returns the number of block files a leaf
// category contributes given its picked value. Mirrors
// render.renderCategory.
func countCategoryBlocks(cat *registry.Category, v *registry.Value) int {
	if v == nil {
		return 0
	}
	switch cat.Pick {
	case registry.PickSingle:
		if v.Single == nil || *v.Single == "" {
			return 0
		}
		return 1
	case registry.PickMulti:
		if v.Multi == nil {
			return 0
		}
		return len(*v.Multi)
	}
	return 0
}

// isInteractive reports whether the current process has a TTY on
// both stdin and stdout. Both must be a TTY to launch a huh form:
// stdin must be readable for key events, and stdout must be a TTY
// for the rendered form to make sense. Pipes / redirects / CI on
// either side fall through to the non-interactive path.
//
// Defensive: term.IsTerminal works across darwin/linux/windows.
// Windows pipe semantics differ subtly from POSIX, but term's
// abstraction handles it.
var isInteractive = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// writeOutput implements the existing-file rules:
//   - error if target exists and !force
//   - rename existing to CLAUDE.md.bak.<unix-ts> when force, then write
//   - print backup path to stderr
//   - never delete, always rename
//
// The non-force path uses os.OpenFile with O_CREATE|O_EXCL so the
// existence check and the write happen atomically at the kernel
// boundary — a separate Stat → WriteFile sequence races against any
// concurrent process that drops a CLAUDE.md between the two calls,
// clobbering work without a backup. The force path keeps the
// rename-then-write shape because backing up requires we observe the
// existing file before overwriting it; a race there is still
// possible (another process could swap the file between Stat and
// Rename) but the worst case is "we backed up the wrong content"
// rather than "we lost data without a backup", which is acceptable
// for a developer CLI.
func writeOutput(stderr io.Writer, path string, content []byte, force bool) error {
	if force {
		// Force path: observe the existing file, rename to backup,
		// then write. The Stat-then-Rename window is microseconds and
		// the failure mode is "wrong backup", not "lost data".
		if _, err := os.Stat(path); err == nil {
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

	// Non-force path: atomic exclusive create. O_EXCL makes the
	// create fail with EEXIST if the file appeared between any
	// earlier check and this call, closing the TOCTOU window.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s exists. Pass --force to overwrite (current file will be backed up) or --out to write elsewhere", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
