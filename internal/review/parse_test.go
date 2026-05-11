package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a testdata fixture, fataling on read failure.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}

// TestParse_CleanJSON asserts tier 1 — a syntactically and schematically
// valid response — unmarshals into the typed ReviewResult cleanly.
func TestParse_CleanJSON(t *testing.T) {
	t.Parallel()
	got, err := Parse(readFixture(t, "clean.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictOK {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictOK)
	}
	if got.Summary == "" {
		t.Errorf("summary should be non-empty")
	}
	if len(got.Issues) != 0 {
		t.Errorf("issues: got %d want 0", len(got.Issues))
	}
}

// TestParse_WarningsWithIssues asserts the issues array is parsed into
// typed Issue values, including the severity enum.
func TestParse_WarningsWithIssues(t *testing.T) {
	t.Parallel()
	got, err := Parse(readFixture(t, "warnings.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictWarnings {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictWarnings)
	}
	if len(got.Issues) != 2 {
		t.Fatalf("issues: got %d want 2", len(got.Issues))
	}
	if got.Issues[0].Severity != SeverityWarning {
		t.Errorf("issues[0].severity: got %q want %q", got.Issues[0].Severity, SeverityWarning)
	}
	if got.Issues[1].Severity != SeverityInfo {
		t.Errorf("issues[1].severity: got %q want %q", got.Issues[1].Severity, SeverityInfo)
	}
	if !strings.Contains(got.Issues[0].Issue, "lockfile") {
		t.Errorf("issues[0].issue: %q", got.Issues[0].Issue)
	}
}

// TestParse_Problems asserts the third verdict-enum value parses.
func TestParse_Problems(t *testing.T) {
	t.Parallel()
	got, err := Parse(readFixture(t, "problems.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictProblems {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictProblems)
	}
	if got.Issues[0].Severity != SeverityError {
		t.Errorf("issues[0].severity: got %q want %q", got.Issues[0].Severity, SeverityError)
	}
}

// TestParse_FencedFallsToTier2 asserts a ```json … ``` fenced response
// gets caught by the lenient extraction (first { to last }).
func TestParse_FencedFallsToTier2(t *testing.T) {
	t.Parallel()
	got, err := Parse(readFixture(t, "fenced.txt"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictOK {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictOK)
	}
}

// TestParse_PreambleFallsToTier2 asserts a response with prose before
// the JSON also gets caught by tier 2.
func TestParse_PreambleFallsToTier2(t *testing.T) {
	t.Parallel()
	got, err := Parse(readFixture(t, "preamble.txt"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictWarnings {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictWarnings)
	}
	if len(got.Issues) != 1 {
		t.Fatalf("issues: got %d want 1", len(got.Issues))
	}
}

// TestParse_MalformedJSONFallsToTier3 asserts that totally-broken JSON
// surfaces as the "unstructured" sentinel verdict with the raw input
// preserved, and Parse itself does not return an error — the spec §7.3
// contract is "always best-effort, never fail the file write".
func TestParse_MalformedJSONFallsToTier3(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "malformed.txt")
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictUnstructured {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictUnstructured)
	}
	if got.Raw != string(raw) {
		t.Errorf("raw not preserved\ngot:  %q\nwant: %q", got.Raw, string(raw))
	}
}

// TestParse_UnknownVerdictFallsToTier3 locks in the design call (plan
// Task 5.3): tier 1 validates the schema, including enum membership.
// A response that's syntactically valid JSON but carries an unknown
// verdict falls through to tier 3 rather than getting a
// "structured-but-invalid" treatment.
func TestParse_UnknownVerdictFallsToTier3(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "unknown_verdict.json")
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictUnstructured {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictUnstructured)
	}
	if !strings.Contains(got.Raw, "totally-broken") {
		t.Errorf("raw should contain the original verdict value; got %q", got.Raw)
	}
}

// TestParse_EmptyInputFallsToTier3 asserts the degenerate empty-input
// case — an empty runner stdout — yields the unstructured sentinel.
func TestParse_EmptyInputFallsToTier3(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictUnstructured {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictUnstructured)
	}
}

// TestParse_InvalidSeverityFallsToTier3 asserts the severity enum is
// also validated in tier 1, mirroring the verdict-enum check.
func TestParse_InvalidSeverityFallsToTier3(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "verdict": "warnings",
  "summary": "ok",
  "issues": [{"severity":"catastrophic","location":"X","issue":"i","suggestion":"s"}]
}`)
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Verdict != VerdictUnstructured {
		t.Errorf("verdict: got %q want %q", got.Verdict, VerdictUnstructured)
	}
}

// TestParse_NullIssuesNormalisedToEmpty asserts that a `null` issues
// field (which JSON unmarshal would leave as a nil slice) is treated
// the same as an empty array. Spec §7.2 says issues "may be empty";
// nil and [] should look identical to consumers.
func TestParse_NullIssuesNormalisedToEmpty(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte(`{"verdict":"ok","summary":"ok","issues":null}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Issues == nil {
		t.Errorf("issues should be non-nil empty slice after normalisation")
	}
	if len(got.Issues) != 0 {
		t.Errorf("issues should be empty; got %d", len(got.Issues))
	}
}
