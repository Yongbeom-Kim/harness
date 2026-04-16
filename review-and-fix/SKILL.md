---
name: review-and-fix
description: "Review code changes with a single self-looping reviewer subagent that fixes P1/P2 issues until only stylistic issues remain."
---

# Review and Fix

Dispatch a single reviewer subagent that reviews code changes across correctness, simplification, and style dimensions, fixes P1/P2 issues directly, and loops until only P3 (stylistic) issues remain.

## When to Use

- **Standalone:** User invokes `/review-and-fix` directly after making changes
- **Embedded:** Called from `implement-from-plan` as the final review step (runs in a subagent)

## Inputs

| Input | Required | Source |
|---|---|---|
| `BASE_SHA` | No | User argument or interactive ask. Diff is always base_sha vs working tree. |
| `SPEC_FILE_PATH` | No | Passed by `implement-from-plan` |
| `PLAN_FILE_PATH` | No | Passed by `implement-from-plan` |

If `BASE_SHA` is not provided as an argument, ask the user interactively:

```
AskUserQuestion(
  questions=[{
    question: "What base commit should I diff against?",
    header: "Base SHA",
    options: [
      { label: "HEAD~1", description: "Compare against the previous commit" },
      { label: "main/master", description: "Compare against the main branch" },
      { label: "Merge base", description: "Auto-detect merge base with main branch" }
    ],
    multiSelect: false
  }]
)
```

## Control Flow

```python
def review_and_fix(base_sha=None, spec_path=None, plan_path=None):
    if not base_sha:
        base_sha = ask_user_for_base_sha()

    diff = git_diff(base_sha, working_tree=True)
    changed_files = extract_changed_files(diff)

    # Dispatch single reviewer — it self-loops internally
    result = dispatch_reviewer(
        changed_files=changed_files,
        diff=diff,
        spec_path=spec_path,
        plan_path=plan_path,
    )

    print_summary(result)
```

## Dispatching the Reviewer

Read `./reviewer-prompt.md`, fill in placeholders, and dispatch as a **foreground** Agent tool call (`subagent_type: "general-purpose"`).

Placeholders:
- `[CHANGED_FILES_LIST]`: List of files from `git diff --name-only BASE_SHA`
- `[DIFF_CONTENT]`: Output of `git diff BASE_SHA`
- `[SPEC_FILE_PATH]`: The spec path, or "N/A" if standalone
- `[PLAN_FILE_PATH]`: The plan path, or "N/A" if standalone

The reviewer handles its own fix→test→re-review loop internally (up to 5 iterations). You do not manage iterations yourself.

## Red Flags

**Never:**
- Manage the reviewer's fix→re-review loop yourself (the reviewer self-loops)
- Dispatch multiple reviewer subagents in parallel (use the single consolidated reviewer)
- Proceed without waiting for the reviewer's final status
