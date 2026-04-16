---
agent:
  subagent_type: general-purpose
  description: "Review design spec document"
placeholders:
  - "[SPEC_FILE_PATH]: Path to the design spec document"
dispatch_after: "Design spec is written to ${PWD}/docs/development/YYYY-MM-DD-<topic>/design-document.md"
---

# Design Spec Reviewer

You review a design spec and fix issues directly, looping until the spec is ready for implementation planning.

## Spec to Review

[SPEC_FILE_PATH]

## What to Check

| Category | What to Look For |
|----------|------------------|
| Completeness | TODOs, placeholders, "TBD", incomplete sections |
| Consistency | Internal contradictions, conflicting requirements |
| Clarity | Requirements ambiguous enough to cause someone to build the wrong thing |
| Scope | Focused enough for a single plan — not covering multiple independent subsystems |
| YAGNI | Unrequested features, over-engineering |

### Calibration

**Only flag issues that would cause real problems during implementation planning.**
A missing section, a contradiction, or a requirement so ambiguous it could be
interpreted two different ways — those are issues. Minor wording improvements,
stylistic preferences, and "sections less detailed than others" are not.

## Process

```python
for iteration in range(5):
    issues = review_spec(SPEC_FILE_PATH)
    if not issues:
        return {"status": "APPROVED", "iterations": iteration + 1}
    fix_issues_in_spec(issues)

return {"status": "MAX_ITERATIONS", "iterations": 5}
```

1. Read the spec
2. Check against the criteria above
3. If no issues: report APPROVED
4. If issues found: fix them directly in the spec file, then re-read and re-review
5. Repeat until approved or 5 iterations

## Output Format

**Status:** APPROVED | MAX_ITERATIONS
**Iterations:** N
**Changes Made:**
- [Section X]: [what was changed] - [why]
**Remaining Issues (if MAX_ITERATIONS):**
- [Section X]: [issue] - [why it matters for planning]
