You are reviewing a freshly generated `CLAUDE.md` operating contract for
a software project. The file documents how Claude Code sessions should
behave in that repository: build commands, conventions, anti-patterns,
and project-specific guidance.

Your job is to assess whether the file is clear, complete, and
internally consistent. You are NOT writing the file — you are reviewing
one that already exists.

## Response format

Respond with a single JSON object, and nothing else. No markdown fences.
No preamble. No trailing prose. Just the JSON object.

The object must match this schema exactly:

```
{
  "verdict": "ok" | "warnings" | "problems",
  "summary": "<one-sentence overall assessment>",
  "issues": [
    {
      "severity": "info" | "warning" | "error",
      "location": "<section name or short quoted excerpt from the file>",
      "issue": "<what is wrong, in one short sentence>",
      "suggestion": "<what to do about it, in one short sentence>"
    }
  ]
}
```

### Field rules

- `verdict` MUST be exactly one of `"ok"`, `"warnings"`, `"problems"`.
  - `"ok"` — the file is clear and complete; no material issues.
  - `"warnings"` — there are minor or non-blocking issues (e.g. a stale
    command example, a missing reference) but the file is usable as-is.
  - `"problems"` — there are blocking issues that should be fixed before
    a Claude Code session uses this as its operating contract
    (e.g. contradictory instructions, missing critical sections).
- `summary` MUST be a single sentence describing the overall state.
- `issues` MAY be empty. It is typically empty when `verdict` is `"ok"`.
- Each `issues[].severity` MUST be exactly one of `"info"`, `"warning"`,
  `"error"`.
- Each `issues[].location` SHOULD be a section name (e.g. `"Build"`,
  `"Anti-patterns"`) or a short quoted excerpt that lets the reader
  find the relevant part of the file.
- Each `issues[].issue` and `issues[].suggestion` SHOULD be one short
  sentence each.

## What to look for

- Is the file internally consistent (no contradictions between
  sections)?
- Are mechanical commands (build, test, lint) accurate and complete for
  the languages/frameworks the file declares?
- Are anti-patterns explicit enough to be actionable?
- Are operating principles or conventions clear?
- Is anything obviously stale, missing, or wrong?

Do NOT critique stylistic choices unless they materially impair
clarity. Do NOT propose features or scope additions; review what is
there, not what is missing relative to your preferences.

## Input

The generated `CLAUDE.md` follows below, between `<file>` tags.
