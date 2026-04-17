---
name: implementation-design
description: "Turn design documents into reviewed implementation specifications through collaborative dialogue. Produces artifacts on disk."
---

# Implementation Design

Turn a design document plus its decision log into a comprehensive implementation specification that another engineer can execute without prior project context.

Write comprehensive implementation plans assuming the engineer has zero context for our codebase and questionable taste. Document everything they need to know: which files to touch for each task, code, testing, docs they might need to check, how to test it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. TDD. Frequent commits.

Assume they are a skilled developer, but know almost nothing about our toolset or problem domain. Assume they don't know good test design very well.

## Inputs

This skill accepts a directory of documents from the `product-design` skill:

```
/implementation-design <directory path>
```

In the directory, there will be two documents: a raw log of design decisions made through dialogue and the resulting design document.

You are tasked with creating a comprehensive implementation plan from the design document given.

This skill produces two artifacts in `${PWD}/docs/development/YYYY-MM-DD-<topic>/`:

1. **Implementation Decisions Log**: `implementation-decisions-raw.md` capturing the implementation-architecture Q&A decisions.
2. **Implementation Spec**: `implementation-spec.md` containing the approved execution plan.

## Steps

Create a task for each step and complete them in order:

1. **Explore project context**: Check the design document, decision log, relevant code, existing file structure, and recent commits.
2. **Implementation Q&A**: Ask **many rounds** of implementation-structure questions before writing the plan.
   - The purpose of this Q&A is to determine the components that will be created or edited, the file and folder structure, and the responsibility of each component.
   - Questions should ALWAYS be multiple choice, with the options derived from the current codebase, design document, and repository patterns.
   - Before asking a question, inspect the current codebase and design docs to see whether the answer is already implied by existing structure.
   - Always provide a recommended option for each question.
   - When you ask and receive an answer, save it to `${PWD}/docs/development/YYYY-MM-DD-<topic>/implementation-decisions-raw.md`.
3. **Component Breakdown**: After the Q&A, write down the implementation component model before task planning.
   - Ask: "Which component should take responsibility for this behavior?"
   - Prefer extending an existing component when responsibility is already clear there.
   - Create a new component only when no existing component cleanly owns the behavior.
4. **File Structure Mapping**: Lock the file and folder structure before decomposing into tasks.
   - Map which files will be created or modified.
   - State what each file or folder is responsible for.
   - Design focused units with clear boundaries and interfaces.
   - In existing codebases, follow established patterns unless the design explicitly requires a new structure.
5. **Write implementation spec**: Save to `${PWD}/docs/development/YYYY-MM-DD-<topic>/implementation-spec.md`.
6. **Implementation spec review**: Read `./implementation-spec-reviewer-prompt.md`, spawn a reviewer subagent with that prompt, pass the implementation spec path plus the design spec path, wait for it to finish, apply any requested fixes, and repeat until the reviewer approves.

## Plan Requirements

The implementation spec must assume the implementer has no prior context. It must be executable without further design decisions.

### Required plan sections

Every implementation spec MUST include, in this order:

1. Plan header
2. Component breakdown
3. File and folder structure
4. Implementation file allowlist
5. Task list

## Plan Document Header

**Every plan MUST start with this header:**

```markdown
# [Feature Name] Implementation Plan

**Goal:** [One sentence describing what this builds]

**Architecture:** [2-3 sentences about approach]

**Tech Stack:** [Key technologies/libraries]

---
```

## Component Breakdown Section

Immediately after the header, include a section that names each component and its responsibility.

Example shape:

```markdown
## Component Breakdown

- `FeatureFlagEvaluator`: determines whether the feature is enabled for the current request.
- `CheckoutFormState`: owns client-side state, validation triggers, and submit readiness.
- `OrderSubmissionHandler`: translates validated form data into the existing backend request contract.
```

## File and Folder Structure Section

After component breakdown, include a section that locks the file and folder structure.

Example shape:

```markdown
## File and Folder Structure

- Create `src/checkout/feature_flags.ts` - feature gating logic for checkout experiments.
- Modify `src/checkout/form_state.ts` - extend existing checkout state machine with new fields.
- Create `src/checkout/components/address_panel.tsx` - isolated address editing UI.
- Modify `src/checkout/__tests__/form_state.test.ts` - regression coverage for new validation paths.
```

## Implementation File Allowlist Section

After file and folder structure, include a section that enumerates the exact files the implementer is allowed to touch for the main work.

Rules:
- This section must be explicit and exhaustive for intentional feature work.
- Prefer exact file paths over directories.
- Separate primary implementation files from incidental files.
- Incidental files are limited to small supporting edits such as imports, wiring, generated snapshots, or narrowly scoped config updates required to make the planned files build or test correctly.
- If a file is not listed here, the implementer should assume it is out of bounds unless the plan explicitly marks it as an incidental exception.
- Do not expand the allowlist just to fix unrelated preexisting test failures or lint issues.

Example shape:

```markdown
## Implementation File Allowlist

**Primary files:**
- `src/checkout/feature_flags.ts`
- `src/checkout/form_state.ts`
- `src/checkout/components/address_panel.tsx`
- `src/checkout/__tests__/form_state.test.ts`

**Incidental-only files:**
- `src/checkout/index.ts` - export wiring only.
- `package.json` - dependency or script update only if required by the planned files.
```

## Bite-Sized Task Granularity

**Each step is one action (2-5 minutes):**
- "Write the failing test" - step
- "Run it to make sure it fails" - step
- "Implement the minimal code to make the test pass" - step
- "Run the tests and make sure they pass" - step
- "Commit" - step

## Task Structure

````markdown
### Task N: [Component Name]

**Files:**
- Create: `exact/path/to/file.py`
- Modify: `exact/path/to/existing.py:123-145`
- Test: `tests/exact/path/to/test.py`

**Why:** [What responsibility this task establishes or extends]

- [ ] **Step 1: Write the failing test**

```python
def test_specific_behavior():
    result = function(input)
    assert result == expected
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/path/test.py::test_name -v`
Expected: FAIL with "function not defined"

- [ ] **Step 3: Write minimal implementation**

```python
def function(input):
    return expected
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/path/test.py::test_name -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add tests/path/test.py src/path/file.py
git commit -m "feat: add specific feature"
```
````

## Remember
- Exact file paths always.
- Complete code in plan, not vague descriptions like "add validation".
- Exact commands with expected output.
- Reference relevant skills with `@` syntax when applicable.
- DRY, YAGNI, TDD, frequent commits.
- Do not start task planning until the component boundaries and file structure are explicit.
- The plan must declare the exact implementation file allowlist before task planning starts.
- Task boundaries should follow the component responsibilities decided during the Q&A.
- Keep the allowlist tight. Do not include speculative files "just in case".
- If tests or lint already fail outside the feature scope, record that as preexisting context rather than planning unrelated fixes.

### Output

After all steps, print a single copy-pasteable command for a fresh window and stop:

```
cat <implementation_spec_path> | implement-with-reviewer --implementer codex --reviewer codex
```
