---
name: implement
description: "Execute a design spec and implementation plan produced by implementation-design. Starts with fresh context — reads all requirements from disk."
---

# Implement from Plan

Execute an implementation plan that was produced by the `implementation-design` skill. This skill starts with **zero prior context** — everything it needs is in the artifacts on disk.

## Inputs

Expected invocation format:

```
/implementation-design <directory_path>
```

Retrieve the design doc and implementation specification from the arguments. If the documents could not be found, notify the user and end the skill.

## Required plan contract

Before implementation begins, read the implementation plan and extract the `Implementation File Allowlist` section.

Treat that allowlist as a hard execution boundary:
- **Primary files** are the only files allowed for intentional feature work.
- **Incidental-only files** may receive small supporting edits only when required to make the primary-file changes build, wire up, or test correctly.
- Any file not listed in the allowlist is out of bounds.

Examples of acceptable incidental edits:
- import or export wiring
- narrowly scoped config updates
- generated snapshots or lockfile changes caused by an approved dependency update
- minimal test harness registration needed by the planned files

Non-acceptable out-of-bounds behavior:
- opportunistic refactors
- unrelated cleanup
- broad renames or file moves not declared in the plan
- touching neighboring modules because they "seem related"
- fixing preexisting test failures or lint issues outside the planned feature scope

If implementation appears to require touching an out-of-bounds file, stop and report that the plan is incomplete instead of expanding scope implicitly.

## Verification scope

Verification is limited to the planned feature scope.

- Do not fix preexisting test failures that are outside the allowlisted files or outside the planned feature behavior.
- Do not fix preexisting lint errors that are outside the allowlisted files or outside the planned feature behavior.
- If a test or lint failure is discovered and appears unrelated to the current feature, report it as preexisting or out of scope rather than broadening the implementation.
- Only address test or lint failures when they are introduced by the planned change, or when the plan explicitly included that cleanup inside the allowlist.

## Steps

1. **Register tools**: Execute the `register-development-tools` skill.
2. **Read artifacts**: Read both the design spec and implementation plan from disk. These are your sole source of truth — do not assume any prior context.
3. **Extract constraints**: Parse the implementation plan for the component breakdown, file structure, and implementation file allowlist. Keep the allowlist visible throughout execution.
4. **Record base SHA**: Run `git rev-parse HEAD` and save it as `BASE_SHA` (the commit before implementation begins).
5. **Implement**: Invoke the `subagent-driven-execution` skill with the implementation plan and explicitly instruct it that files outside the allowlist are forbidden except for listed incidental-only files within their stated limits.
6. **Boundary check**: Before review, compare the changed files against the allowlist. If out-of-bounds files were touched, treat that as a failure and do not continue as if implementation succeeded.
7. **Review and fix**: Invoke the `review-and-fix` skill with `BASE_SHA`, `SPEC_FILE_PATH`, and `PLAN_FILE_PATH`, while preserving the same file-boundary and verification-scope rules.

## Red Flags

**Never:**
- Treat the plan as a suggestion instead of a contract.
- Expand the file set without the plan explicitly allowing it.
- Hide out-of-bounds edits inside "miscellaneous" changes.
- Fix unrelated preexisting test or lint failures just because they showed up during verification.
- Continue to review-and-fix if the implementation already violated the allowlist without first calling that out.
