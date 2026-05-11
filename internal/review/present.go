package review

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// Present renders a ReviewResult to w with lipgloss styling per spec
// §7.4. `termWidth` is the detected terminal width; when 0 (or
// unknown) the function falls back to 100 cols per §7.4.
//
// Why termWidth is an int argument rather than detected here: the
// presentation layer needs to be deterministically testable, and
// `golang.org/x/term` reads the actual TTY. Hoisting the detection to
// the caller (the cli layer in Task 5.5) keeps Present pure-ish — the
// only side effect is writing to w.
//
// The unstructured-verdict branch (§7.7 sentinel) renders the raw
// model text under a `review (unstructured)` header instead of the
// issue-list shape, so the user can still read what claude said.
func Present(w io.Writer, res ReviewResult, termWidth int) {
	width := termWidth
	if width <= 0 {
		width = fallbackWidth
	}

	if res.Verdict == VerdictUnstructured {
		presentUnstructured(w, res)
		return
	}
	presentStructured(w, res, width)
}

// presentStructured renders the ok / warnings / problems shape.
func presentStructured(w io.Writer, res ReviewResult, width int) {
	fmt.Fprintln(w, sectionHeaderStyle.Render(headerDivider))

	marker, vstyle := verdictMarker(res.Verdict)
	header := fmt.Sprintf("%s %s    %s", marker, string(res.Verdict), res.Summary)
	fmt.Fprintln(w, vstyle.Render(header))

	for _, iss := range res.Issues {
		fmt.Fprintln(w) // blank line between header / each issue
		renderIssue(w, iss, width)
	}
}

// presentUnstructured renders the §7.7 sentinel shape. The raw text is
// printed verbatim (no wrap) — it's already prose and lipgloss-wrapping
// it would mangle Claude's intended line breaks.
//
// Edge case: a hand-constructed result with both Summary and Raw empty
// would print a header and a blank line. Parse() never produces that
// shape (tier 3 always populates Summary), but the defensive
// "review failed without producing text" sentinel keeps the output
// readable for any future caller that synthesises an unstructured
// result by hand.
func presentUnstructured(w io.Writer, res ReviewResult) {
	fmt.Fprintln(w, sectionHeaderStyle.Render("── review (unstructured) ──"))
	if res.Summary != "" {
		fmt.Fprintln(w, severityWarningStyle.Render(res.Summary))
	}
	fmt.Fprintln(w)
	if res.Summary == "" && res.Raw == "" {
		fmt.Fprintln(w, severityWarningStyle.Render("review failed without producing text"))
		return
	}
	fmt.Fprint(w, res.Raw)
	if res.Raw == "" || !strings.HasSuffix(res.Raw, "\n") {
		fmt.Fprintln(w)
	}
}

// renderIssue prints one Issue per spec §7.4:
//
//	Section "<location>" — <issue body>
//	    → <suggestion>
//
// `location` is truncated to 57 + "..." beyond 60 cols. `issue` and
// `suggestion` are wrapped (not truncated) at the configured width.
// Severity colour is applied to the whole line via the severity style.
func renderIssue(w io.Writer, iss Issue, width int) {
	sev := severityStyle(iss.Severity)

	loc := truncateLocation(iss.Location, locationMaxCols)
	prefix := fmt.Sprintf(`Section "%s" — `, loc)
	// Indent width is measured in display columns, not bytes. The em-dash
	// `—` is one column but three UTF-8 bytes; using len(prefix) would
	// push continuation lines two columns too far right. lipgloss.Width
	// is already imported and handles the rune-counting (plus ANSI, which
	// `prefix` currently doesn't carry — but the choice keeps us safe if
	// a future change adds inline styling here).
	prefixCols := lipgloss.Width(prefix)
	bodyWrap := wrap(iss.Issue, max(width-prefixCols, 20))
	bodyLines := strings.Split(bodyWrap, "\n")

	// First line: prefix + first body chunk. Subsequent body lines
	// align under the start of the issue text (continuation indent).
	contIndent := strings.Repeat(" ", prefixCols)
	fmt.Fprintln(w, sev.Render(prefix+bodyLines[0]))
	for _, line := range bodyLines[1:] {
		fmt.Fprintln(w, sev.Render(contIndent+line))
	}

	// Suggestion: `    → <suggestion>` with `    ` indent per §7.4's
	// rendering example. Wraps at width - 6 display columns (4 indent
	// + 2 for "→ "). Use lipgloss.Width, not len: the arrow is a
	// 3-byte rune occupying 1 column; `len("→ ") == 4` would budget
	// the wrap two columns short and indent continuation lines two
	// columns too far. Same bug class as the issue-body prefix above.
	suggIndent := "    "
	suggArrow := "→ "
	suggArrowCols := lipgloss.Width(suggArrow)
	suggWrap := wrap(iss.Suggestion, max(width-lipgloss.Width(suggIndent)-suggArrowCols, 20))
	suggLines := strings.Split(suggWrap, "\n")
	fmt.Fprintln(w, suggestionStyle.Render(suggIndent+suggArrow+suggLines[0]))
	suggCont := suggIndent + strings.Repeat(" ", suggArrowCols)
	for _, line := range suggLines[1:] {
		fmt.Fprintln(w, suggestionStyle.Render(suggCont+line))
	}
}

// truncateLocation returns loc truncated to (max-3) runes + "..." when
// loc is longer than max runes. Rune-aware to avoid emitting half a
// codepoint when a Section header contains non-ASCII characters
// (`"Section: 빌드"` etc.). Location strings are typically short ASCII
// identifiers so the fast path hits first.
func truncateLocation(loc string, max int) string {
	if utf8.RuneCountInString(loc) <= max {
		return loc
	}
	// Walk runes until we've kept max-3, then append the ellipsis.
	keep := max - 3
	i := 0
	for j := range loc {
		if i == keep {
			return loc[:j] + "..."
		}
		i++
	}
	// Unreachable: loc had > max runes, so we hit keep before end.
	return loc
}

// wrap soft-wraps s at width columns by inserting newlines at word
// boundaries. A word longer than width overflows the line — review
// content is sentences of normal words, so the overflow case is
// vanishingly rare and the alternative (mid-word hard-break) would
// mangle URLs and identifiers when it did fire. width <= 0 means
// "no wrap" (return s verbatim).
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	col := 0
	for i, word := range strings.Fields(s) {
		wlen := len(word)
		switch {
		case i == 0:
			// First word: emit verbatim. If it's longer than width
			// the next word will trigger a wrap.
			out.WriteString(word)
			col = wlen
		case col+1+wlen <= width:
			out.WriteByte(' ')
			out.WriteString(word)
			col += 1 + wlen
		default:
			out.WriteByte('\n')
			out.WriteString(word)
			col = wlen
		}
	}
	return out.String()
}

// verdictMarker returns the glyph and style for a verdict per §7.4.
func verdictMarker(v Verdict) (marker string, style lipgloss.Style) {
	switch v {
	case VerdictOK:
		return "✓", verdictOKStyle
	case VerdictWarnings:
		return "⚠", verdictWarnStyle
	case VerdictProblems:
		return "✗", verdictProblemStyle
	default:
		return "?", verdictWarnStyle
	}
}

// severityStyle returns the lipgloss style for a severity per §7.4
// (info=neutral, warning=yellow, error=red).
func severityStyle(s Severity) lipgloss.Style {
	switch s {
	case SeverityWarning:
		return severityWarningStyle
	case SeverityError:
		return severityErrorStyle
	default:
		return severityInfoStyle
	}
}

// Layout / wrap constants.
const (
	fallbackWidth   = 100
	locationMaxCols = 60
	headerDivider   = "── claude review ──"
)

// Styles. Local to this package (not imported from internal/tui) per
// the layering tradeoff in plan Task 5.4: review is a leaf package,
// pulling style declarations from tui would invert the dependency.
// The visual contract is small enough that duplicating these few
// lipgloss style values is cheaper than the cross-package coupling.
//
// Lipgloss v2 picks the terminal colour profile via runtime detection,
// so test assertions strip ANSI rather than match against specific
// codepoints — that keeps the assertions stable across local dev
// (truecolor) and CI (ansi256 / monochrome).
var (
	sectionHeaderStyle = lipgloss.NewStyle().Bold(true).Faint(true)

	verdictOKStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	verdictWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	verdictProblemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	severityInfoStyle    = lipgloss.NewStyle()
	severityWarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	severityErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	suggestionStyle = lipgloss.NewStyle().Faint(true)
)
