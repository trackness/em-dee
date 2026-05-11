// Package tui's styles file is the single home for lipgloss styles
// shared across the interactive flow's success line and (in Phase 5)
// the review-presentation block. Keeping all styles in one file makes
// the visual contract auditable in one read and avoids style drift
// across packages.
//
// Review presentation uses verdict markers and severity colors that
// are declared here even though the consuming code lands in Phase 5 —
// having the styles in place now lets the success line of Phase 4
// share the same lipgloss bootstrapping.
//
// **Lipgloss version tradeoff**: huh v2 (charm.land/huh/v2)
// transitively requires `charm.land/lipgloss/v2` rather than the
// charmbracelet/lipgloss namespace. To avoid carrying two lipgloss
// copies in the module graph we use the v2 namespace here.
// Functionally equivalent for our usage; surfaced in the commit body
// for Task 4.1.

package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// SuccessLine renders the post-write summary line:
// `wrote CLAUDE.md (N blocks, N.NN KB)`. The whole line is rendered
// in a muted green so it stands out from any preceding stderr noise
// but doesn't shout. The byte count is reported as KB to two decimals.
func SuccessLine(path string, blocks int, byteCount int) string {
	kb := float64(byteCount) / 1024.0
	body := fmt.Sprintf("wrote %s (%d blocks, %.2f KB)", path, blocks, kb)
	return successLineStyle.Render(body)
}

// Verdict / severity styles — exported for the Phase 5 review-
// presentation code. The colors are ANSI 256-color codes so they
// degrade gracefully on terminals without truecolor.
var (
	// successLineStyle is the muted green used by SuccessLine.
	successLineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)

	// VerdictOK is the green check used when claude returns
	// verdict:"ok".
	VerdictOK = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	// VerdictWarn is the yellow warning glyph used when claude
	// returns verdict:"warnings".
	VerdictWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	// VerdictProblem is the red cross used when claude returns
	// verdict:"problems".
	VerdictProblem = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	// SeverityInfo / Warning / Error: info=neutral, warning=yellow,
	// error=red. Placeholders for Phase 5; exposed now so the styles
	// live in one file.
	//
	// SeverityInfo intent (PR #5 review question): the zero-foreground
	// style is **deliberate**, not an oversight — info is "neutral",
	// which the lipgloss render path passes through as the terminal's
	// default foreground. The Phase 5 consumer (ultraviolet renderer
	// or otherwise) should treat `SeverityInfo.Render(s) == s` as the
	// contract: any visible adornment (e.g. a glyph) belongs to the
	// consumer, not the style. If a future change needs a non-empty
	// info color, revisit this declaration.
	SeverityInfo    = lipgloss.NewStyle()
	SeverityWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	SeverityError   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// SectionHeader styles the `── claude review ──` divider line.
	SectionHeader = lipgloss.NewStyle().Bold(true).Faint(true)
	// IssueLocation styles the `Section "Build"` prefix on each issue.
	IssueLocation = lipgloss.NewStyle().Bold(true)
	// IssueSuggestion styles the `→ Add a one-line note` indent.
	IssueSuggestion = lipgloss.NewStyle().Faint(true)
)
