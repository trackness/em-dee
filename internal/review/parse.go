package review

import (
	"bytes"
	"encoding/json"
)

// Verdict is the overall assessment value from spec §7.2's enum, plus
// the §7.7 sentinel `"unstructured"` that the parser emits when none of
// tier 1 / tier 2 succeed.
//
// Spec §7.2: claude only ever sends one of the three real verdicts.
// §7.7: the sentinel is introduced by em-dee's tier 3 fallback so the
// presentation and `--review-out` layers can branch on it without
// special-casing nil values.
type Verdict string

const (
	VerdictOK           Verdict = "ok"
	VerdictWarnings     Verdict = "warnings"
	VerdictProblems     Verdict = "problems"
	VerdictUnstructured Verdict = "unstructured"
)

// Severity is the per-issue severity enum from spec §7.2.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Issue mirrors one entry in the §7.2 `issues` array.
type Issue struct {
	Severity   Severity `json:"severity"`
	Location   string   `json:"location"`
	Issue      string   `json:"issue"`
	Suggestion string   `json:"suggestion"`
}

// ReviewResult is what Parse returns. On tier 1 or 2 it carries the
// fully populated fields per spec §7.2; on tier 3 it carries the
// `VerdictUnstructured` sentinel plus the raw input in `Raw` so the
// presentation and `--review-out` layers can branch on it.
type ReviewResult struct {
	Verdict Verdict `json:"verdict"`
	Summary string  `json:"summary"`
	Issues  []Issue `json:"issues"`
	// Raw carries the original input when Verdict == VerdictUnstructured.
	// Empty for tier 1/2 results.
	Raw string `json:"raw,omitempty"`
}

// Parse turns the runner's stdout bytes (after envelope unwrapping)
// into a typed ReviewResult. Three tiers per spec §7.3:
//
//  1. Strict — json.Unmarshal against the full schema, then validate
//     enum membership. Success → return the result.
//  2. Lenient — find the first `{` and last `}` in the input, re-parse.
//     Catches markdown-fenced or preamble-wrapped responses.
//  3. Fallback — return a ReviewResult with VerdictUnstructured and
//     Raw == input. Never errors; the CLI maps tier 3 to exit 0 per
//     spec §7.5 (review is best-effort).
//
// The function never returns a non-nil error. Plan Task 5.3's design
// call (locked in by TestParse_UnknownVerdictFallsToTier3) is that tier
// 1's "validate" includes schema correctness — an unknown verdict in
// otherwise-valid JSON falls to tier 3 rather than being treated as a
// "structured-but-invalid" intermediate tier. Fewer code paths, fewer
// states for the CLI to branch on.
func Parse(input []byte) (ReviewResult, error) {
	if res, ok := tryStrict(input); ok {
		return res, nil
	}
	if extracted, ok := lenientExtract(input); ok {
		if res, ok := tryStrict(extracted); ok {
			return res, nil
		}
	}
	return ReviewResult{
		Verdict: VerdictUnstructured,
		Summary: "claude review could not be parsed as structured JSON",
		Issues:  []Issue{},
		Raw:     string(input),
	}, nil
}

// tryStrict attempts a strict-schema unmarshal + validation. On success
// it returns (result, true); on any parse or validation failure it
// returns (zero, false).
//
// Validation rules (spec §7.2):
//   - verdict must be one of "ok" / "warnings" / "problems".
//   - each issue's severity must be one of "info" / "warning" / "error".
//
// We do not validate that `summary` is non-empty — Claude might
// legitimately return an empty summary on `verdict: "ok"`. We do
// normalise nil `issues` to an empty slice so consumers can rely on
// non-nil semantics.
//
// `DisallowUnknownFields` is deliberately NOT enabled: spec §7.2 does
// not forbid extra fields, and accepting them gives Claude room to add
// optional metadata (e.g. a `model` or `confidence` field in a future
// prompt revision) without forcing all responses through the tier 3
// fallback. The tier 3 path catches genuinely malformed responses
// anyway, so the rejection-on-unknown choice would buy strictness at
// the cost of brittleness.
func tryStrict(input []byte) (ReviewResult, bool) {
	var res ReviewResult
	dec := json.NewDecoder(bytes.NewReader(input))
	if err := dec.Decode(&res); err != nil {
		return ReviewResult{}, false
	}
	switch res.Verdict {
	case VerdictOK, VerdictWarnings, VerdictProblems:
		// fine
	default:
		return ReviewResult{}, false
	}
	for _, iss := range res.Issues {
		switch iss.Severity {
		case SeverityInfo, SeverityWarning, SeverityError:
			// fine
		default:
			return ReviewResult{}, false
		}
	}
	if res.Issues == nil {
		res.Issues = []Issue{}
	}
	return res, true
}

// lenientExtract returns the slice of input from the first `{` to the
// last `}` inclusive. Returns (nil, false) if either anchor is missing
// or they're in the wrong order.
//
// This is documented as best-effort per spec §7.3: a JSON object with
// a literal `{` or `}` inside a quoted string could mis-anchor the
// extraction. Tier 3 catches anything that survives.
func lenientExtract(input []byte) ([]byte, bool) {
	first := bytes.IndexByte(input, '{')
	last := bytes.LastIndexByte(input, '}')
	if first < 0 || last < 0 || last <= first {
		return nil, false
	}
	return input[first : last+1], true
}
