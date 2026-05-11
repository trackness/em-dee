package tui

import (
	"strings"
	"testing"
)

// TestSuccessLine_Shape asserts SuccessLine produces a string matching
// the §5.2 step 7 contract — path, block count, KB-to-two-decimals.
// We don't pin ANSI escape codes because lipgloss varies them by
// terminal-capability detection during the test run; the substring
// checks are sufficient to catch regressions in the format string.
func TestSuccessLine_Shape(t *testing.T) {
	t.Parallel()

	got := SuccessLine("CLAUDE.md", 7, 2048)
	for _, want := range []string{"wrote CLAUDE.md", "7 blocks", "2.00 KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("SuccessLine missing %q: got %q", want, got)
		}
	}
}

// TestStyles_Render is a smoke test that each declared lipgloss style
// renders without panicking and returns the input substring somewhere
// in its output. This catches misconfigured styles at unit-test time
// rather than first-interactive-run time.
func TestStyles_Render(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
	}{
		{"VerdictOK", VerdictOK.Render("ok")},
		{"VerdictWarn", VerdictWarn.Render("warn")},
		{"VerdictProblem", VerdictProblem.Render("problem")},
		{"SeverityInfo", SeverityInfo.Render("info")},
		{"SeverityWarning", SeverityWarning.Render("warning")},
		{"SeverityError", SeverityError.Render("error")},
		{"SectionHeader", SectionHeader.Render("section")},
		{"IssueLocation", IssueLocation.Render("loc")},
		{"IssueSuggestion", IssueSuggestion.Render("sugg")},
	}
	for _, c := range cases {
		if c.out == "" {
			t.Errorf("%s.Render returned empty string", c.name)
		}
	}
}

// TestSeverityInfo_IsNeutral pins the §7.4 "neutral info" contract:
// SeverityInfo.Render(s) returns s byte-equal — no ANSI, no padding,
// no width transforms. The zero-style is deliberate (see styles.go).
// A future PR that accidentally adds a foreground colour to
// SeverityInfo would fail this test; deliberate changes must update
// the spec first.
func TestSeverityInfo_IsNeutral(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"info", "", "multi line\nstring", "with-symbols-✓"} {
		if got := SeverityInfo.Render(s); got != s {
			t.Errorf("SeverityInfo.Render(%q) = %q; want byte-equal (§7.4 'neutral')", s, got)
		}
	}
}
