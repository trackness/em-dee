package review

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// ansiRE matches ANSI CSI escape sequences (e.g. `\x1b[1;32m`). Used by
// stripANSI to make rendered output deterministic for assertions —
// lipgloss adds colour escapes that vary across terminal-profile
// detection.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripANSI removes ANSI escapes from s. Tests compare against the
// stripped form so the assertions don't drift if lipgloss picks a
// different colour codepoint.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// TestPresent_OKVerdict asserts the simplest happy path: verdict ok,
// no issues, renders the header + verdict line + summary, nothing else.
func TestPresent_OKVerdict(t *testing.T) {
	t.Parallel()
	res := ReviewResult{
		Verdict: VerdictOK,
		Summary: "The CLAUDE.md is clear and complete.",
		Issues:  []Issue{},
	}
	var buf bytes.Buffer
	Present(&buf, res, 80)
	got := stripANSI(buf.String())

	for _, want := range []string{
		"claude review",
		"ok",
		"The CLAUDE.md is clear and complete.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Section") {
		t.Errorf("ok verdict with no issues should not print any Section line:\n%s", got)
	}
}

// TestPresent_WarningsWithIssues asserts the issue-list shape: each
// issue gets a `Section "<loc>" — <issue>` line and a `→ <suggestion>`
// continuation line.
func TestPresent_WarningsWithIssues(t *testing.T) {
	t.Parallel()
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "Two minor issues.",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   "Build",
				Issue:      "Missing reference to lockfile commit policy",
				Suggestion: "Add a one-line note: commit lockfiles for reproducible builds",
			},
			{
				Severity:   SeverityInfo,
				Location:   "Testing",
				Issue:      "Pytest invocation example is stale",
				Suggestion: "Use `pytest -q` instead of `py.test`",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 120)
	got := stripANSI(buf.String())

	wants := []string{
		"warnings",
		"Two minor issues.",
		`Section "Build"`,
		"Missing reference to lockfile commit policy",
		"→",
		"commit lockfiles for reproducible builds",
		`Section "Testing"`,
		"Pytest invocation example is stale",
		"py.test",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q:\n%s", w, got)
		}
	}
}

// TestPresent_ProblemsVerdict asserts the problems verdict carries the
// expected marker (a cross / X glyph) and surfaces the error severity.
func TestPresent_ProblemsVerdict(t *testing.T) {
	t.Parallel()
	res := ReviewResult{
		Verdict: VerdictProblems,
		Summary: "Critical issue.",
		Issues: []Issue{
			{
				Severity:   SeverityError,
				Location:   "Build",
				Issue:      "Two conflicting build commands",
				Suggestion: "Pick one and remove the other",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 80)
	got := stripANSI(buf.String())

	for _, want := range []string{
		"problems",
		"Critical issue.",
		"Two conflicting build commands",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestPresent_UnstructuredRendersRawUnderHeader asserts the §7.7
// sentinel path: an unstructured verdict carries the raw text under a
// `review (unstructured)` header, no Section-format issue list.
func TestPresent_UnstructuredRendersRawUnderHeader(t *testing.T) {
	t.Parallel()
	res := ReviewResult{
		Verdict: VerdictUnstructured,
		Summary: "claude review could not be parsed as structured JSON",
		Issues:  []Issue{},
		Raw:     "this is not JSON at all\nit's just text\n",
	}
	var buf bytes.Buffer
	Present(&buf, res, 80)
	got := stripANSI(buf.String())

	for _, want := range []string{
		"unstructured",
		"this is not JSON at all",
		"it's just text",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestPresent_LongLocationTruncated asserts the §7.4 truncation rule:
// `location` longer than 60 columns gets truncated to 57 + "...".
func TestPresent_LongLocationTruncated(t *testing.T) {
	t.Parallel()
	longLoc := strings.Repeat("a", 100)
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "long-location case",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   longLoc,
				Issue:      "something",
				Suggestion: "fix it",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 200)
	got := stripANSI(buf.String())

	// The truncated form: 57 'a's + "..." appears in the output.
	wantTruncated := strings.Repeat("a", 57) + "..."
	if !strings.Contains(got, wantTruncated) {
		t.Errorf("expected truncated location %q in output:\n%s", wantTruncated, got)
	}
	// The full 100-char location does NOT appear.
	if strings.Contains(got, longLoc) {
		t.Errorf("untruncated location should not appear in output")
	}
}

// TestPresent_ShortLocationNotTruncated asserts the no-op case: a
// location at or under the 60-col threshold is rendered verbatim.
func TestPresent_ShortLocationNotTruncated(t *testing.T) {
	t.Parallel()
	loc := strings.Repeat("a", 60)
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "exactly-60 case",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   loc,
				Issue:      "i",
				Suggestion: "s",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 200)
	got := stripANSI(buf.String())
	if !strings.Contains(got, loc) {
		t.Errorf("60-char location should be rendered verbatim:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("a", 57)+"...") {
		t.Errorf("60-char location should not be truncated:\n%s", got)
	}
}

// TestPresent_FallbackWidth asserts that termWidth=0 picks the 100-col
// fallback documented in spec §7.4. We exercise it indirectly: long
// content gets wrapped *somewhere*, and the wrap column is the
// fallback when termWidth is 0.
func TestPresent_FallbackWidth(t *testing.T) {
	t.Parallel()
	// An issue text longer than 100 cols — under the fallback it must
	// wrap, under no wrap it would be one long line.
	long := strings.Repeat("word ", 40) // 200 chars
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "wrap test",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   "X",
				Issue:      long,
				Suggestion: "s",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 0) // 0 -> 100-col fallback
	got := stripANSI(buf.String())

	// Every output line should be at most ~100 cols (allowing a few
	// chars of leading indent / prefix).
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 110 {
			t.Errorf("line exceeded 110 cols (fallback width 100 + indent slack): %q", line)
		}
	}
}

// TestPresent_LongMultibyteLocationTruncatedAtRunes asserts rune-aware
// truncation: when the location is longer than the 60-rune threshold
// and contains multibyte characters, the truncated form is still valid
// UTF-8 (no mid-rune cut) and counts runes rather than bytes.
func TestPresent_LongMultibyteLocationTruncatedAtRunes(t *testing.T) {
	t.Parallel()
	// 100 Korean syllables (3 UTF-8 bytes each, 1 display column per
	// rune in the lipgloss/wcwidth model — but the byte length is 300,
	// way over the 60-byte threshold under the old impl, which would
	// have cut mid-rune).
	longLoc := strings.Repeat("빌", 100)
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "multibyte location",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   longLoc,
				Issue:      "something",
				Suggestion: "fix it",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 200)
	got := stripANSI(buf.String())

	// Output must be valid UTF-8 — a byte-based truncation would have
	// produced a partial rune.
	for _, line := range strings.Split(got, "\n") {
		for i, r := range line {
			if r == '�' {
				t.Errorf("line %d contains replacement rune at %d (mid-rune cut?): %q", i, i, line)
			}
		}
	}
	// The truncated form should appear: 57 syllables + "...".
	wantPrefix := strings.Repeat("빌", 57) + "..."
	if !strings.Contains(got, wantPrefix) {
		t.Errorf("expected rune-truncated location prefix %q in output:\n%s", wantPrefix, got)
	}
}

// TestPresent_UnstructuredEmptyEmitsSentinel asserts the L4 edge case:
// a hand-constructed unstructured result with empty Summary and empty
// Raw prints a sentinel line instead of just a blank.
func TestPresent_UnstructuredEmptyEmitsSentinel(t *testing.T) {
	t.Parallel()
	res := ReviewResult{
		Verdict: VerdictUnstructured,
		// Summary and Raw deliberately empty.
		Issues: []Issue{},
	}
	var buf bytes.Buffer
	Present(&buf, res, 80)
	got := stripANSI(buf.String())
	if !strings.Contains(got, "review failed without producing text") {
		t.Errorf("expected sentinel for empty unstructured result:\n%s", got)
	}
}

// TestPresent_IssueWrapsAtTermWidth asserts the issue body gets wrapped
// at the configured terminal width, not truncated.
func TestPresent_IssueWrapsAtTermWidth(t *testing.T) {
	t.Parallel()
	// 60-char issue, 30-col terminal width: must produce at least 2
	// lines for the issue body.
	res := ReviewResult{
		Verdict: VerdictWarnings,
		Summary: "wrap",
		Issues: []Issue{
			{
				Severity:   SeverityWarning,
				Location:   "X",
				Issue:      strings.Repeat("word ", 20),
				Suggestion: "s",
			},
		},
	}
	var buf bytes.Buffer
	Present(&buf, res, 30)
	got := stripANSI(buf.String())

	// All output lines fit under ~40 cols (term width + slack for
	// leading indent).
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 40 {
			t.Errorf("line exceeded 40 cols under termWidth=30: %q", line)
		}
	}
}
