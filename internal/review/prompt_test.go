package review

import (
	"strings"
	"testing"
)

// TestPromptTemplate_Embedded asserts the prompt is compiled into the
// binary and is non-empty. A missing or empty file would be a silent
// regression: every review would prompt Claude with just the rendered
// CLAUDE.md and no instructions, and the JSON contract would be
// whatever Claude felt like producing.
func TestPromptTemplate_Embedded(t *testing.T) {
	t.Parallel()
	if got := PromptTemplate(); got == "" {
		t.Fatal("PromptTemplate() is empty; the //go:embed directive failed")
	}
}

// TestPromptTemplate_SchemaMarkers asserts the embedded prompt names
// the JSON keys the parser expects, plus each enum value. The check is
// deliberately substring-only (not a regex over wording) so the prompt
// can be re-written for clarity without test churn — but a renamed
// schema key or a dropped enum value will be caught.
func TestPromptTemplate_SchemaMarkers(t *testing.T) {
	t.Parallel()
	body := PromptTemplate()

	// Schema field names. The parser looks for these exact keys, so
	// the prompt must name them too.
	for _, marker := range []string{
		"verdict",
		"summary",
		"issues",
		"severity",
		"location",
		"issue",
		"suggestion",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("prompt missing schema key %q", marker)
		}
	}

	// Verdict enum (spec §7.2).
	for _, marker := range []string{`"ok"`, `"warnings"`, `"problems"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("prompt missing verdict enum value %s", marker)
		}
	}
	// Severity enum (spec §7.2).
	for _, marker := range []string{`"info"`, `"warning"`, `"error"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("prompt missing severity enum value %s", marker)
		}
	}
}

// TestPromptTemplate_NoMarkdownFenceInstruction asserts the prompt
// tells Claude explicitly not to wrap the JSON in markdown fences. The
// three-tier parser's tier 2 catches fence-wrapped responses as a
// fallback, but tier 1 should be the common case — so the prompt has
// to be explicit.
func TestPromptTemplate_NoMarkdownFenceInstruction(t *testing.T) {
	t.Parallel()
	body := strings.ToLower(PromptTemplate())
	// Either of these phrasings is fine; we just need *some* explicit
	// no-fence instruction.
	wants := []string{"no markdown fences", "no markdown fence"}
	var found bool
	for _, w := range wants {
		if strings.Contains(body, w) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("prompt should explicitly forbid markdown fences (looked for one of %v)", wants)
	}
}
