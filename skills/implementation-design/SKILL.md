---
name: implementation-design
description: "Turn a reviewed design document plus decision log into a reviewed implementation model and executable implementation plan. Produces requirement-to-component ownership mapping, interface contracts, file ownership maps, and task lists. Use when a design spec needs concrete implementation architecture before coding."
---

# Implementation Design

Turn a design document plus its decision log into a comprehensive implementation specification that another engineer can execute without prior project context.

Write comprehensive implementation plans assuming the engineer has zero context for our codebase and questionable taste. Document everything they need to know: which files to touch for each task, code, testing, docs they might need to check, how to test it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. TDD. Frequent commits.

Assume they are a skilled developer, but know almost nothing about our toolset or problem domain. Assume they don't know good test design very well.

Bias toward implementation-model discussion over task decomposition. The core output is a complete, reviewed mapping from requirements to component ownership; the task list is downstream of that model, not a substitute for it.

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
   - The purpose of this Q&A is to resolve the implementation model: which components own which requirements, which files they live in, how they interact, and how each requirement will be tested.
   - Keep asking until every requirement and material edge case from the design spec has: exactly one primary owner, any named collaborators, concrete file homes, explicit interface points, and planned tests.
   - Ask all Q&A in plain text using this exact structure:
     `1. <QUESTION>`
     `A: ...`
     `B: ...`
     `C: ...`
     `D: ...`
   - Number questions consecutively within each round. Keep the option labels exactly `A`, `B`, `C`, `D`.
   - Put the recommended option first when possible and mark it inline with `(Recommended)`.
   - Derive the options from the current codebase, design document, and repository patterns.
   - Before asking a question, inspect the current codebase and design docs to see whether the answer is already implied by existing structure.
   - Expect the user to answer in compact form such as `1A2B3A4D`. Parse that mapping by question number. If the response is ambiguous, ask a minimal follow-up.
   - When you ask and receive an answer, save it to `${PWD}/docs/development/YYYY-MM-DD-<topic>/implementation-decisions-raw.md`.
3. **Implementation model**: After the Q&A, write down the implementation model before task planning.
   - Start with a requirement coverage matrix that maps every requirement and edge case to an implementation owner.
   - Ask: "Which component should take responsibility for this behavior?"
   - Each requirement or edge case must have exactly one primary owner component. Collaborators are allowed, but they do not share ownership.
   - Prefer extending an existing component when responsibility is already clear there.
   - Create a new component only when no existing component cleanly owns the behavior.
   - Do not proceed to task planning until the requirement coverage matrix, component responsibility map, interaction contracts, and file ownership map are complete.
4. **File ownership mapping**: Lock the file and folder structure before decomposing into tasks.
   - Map which files will be created or modified and which component primarily owns each one.
   - State what each file or folder is responsible for.
   - Design focused units with clear boundaries and interfaces.
   - Each file should have one clear primary owner component, even if multiple components interact with it.
   - In existing codebases, follow established patterns unless the design explicitly requires a new structure.
5. **Write implementation spec**: Save to `${PWD}/docs/development/YYYY-MM-DD-<topic>/implementation-spec.md`.
6. **Implementation spec review**: Read `./implementation-spec-reviewer-prompt.md`, spawn a reviewer subagent with that prompt, pass the implementation spec path plus the design spec path, wait for it to finish, apply any requested fixes, and repeat until the reviewer approves.

## Plan Requirements

The implementation spec must assume the implementer has no prior context. It must be executable without further design decisions, and its implementation model must be complete enough that an implementer can trace every requirement and material edge case to a single owning component, concrete files, interface points, and planned tests.

### Required plan sections

Every implementation spec MUST include, in this order:

1. Plan header
2. Requirement coverage matrix
3. Component responsibility map
4. Component interactions and contracts
5. File ownership map
6. Implementation file allowlist
7. Task list

## Plan Document Header

**Every plan MUST start with this header:**

```markdown
# [Feature Name] Implementation Plan

**Goal:** [One sentence describing what this builds]

**Architecture:** [2-3 sentences about approach]

**Tech Stack:** [Key technologies/libraries]

---
```

## Requirement Coverage Matrix Section

Immediately after the header, include a section that maps every requirement and material edge case from the design doc to its implementation ownership.

Rules:
- Include one row for every user-visible requirement and every non-trivial edge case from the design doc.
- Use stable IDs such as `R1`, `R2`, `E1`, `E2` so later tasks can reference them.
- Each row must name exactly one primary owner component.
- Collaborators are optional, but if present they are helpers, not co-owners.
- Files must use exact repository paths.
- Interface points must name the concrete boundary: function, prop, event, API route, selector, schema, DB write, config flag, or equivalent.
- Planned tests must say what level of coverage is expected and where that test will live when known.
- Do not proceed to task planning until every row is fully assigned.

Example shape:

```markdown
## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Enable the feature only for allowlisted requests. | `FeatureFlagEvaluator` | `CheckoutEntryPoint` | `src/checkout/feature_flags.ts`, `src/checkout/index.ts` | `isCheckoutFeatureEnabled(request)` | `src/checkout/__tests__/feature_flags.test.ts` |
| E1 | Fall back cleanly when persisted draft data is invalid. | `DraftRecoveryService` | `CheckoutFormState`, `DraftRecoveryBanner` | `src/checkout/draft_recovery.ts`, `src/checkout/form_state.ts`, `src/checkout/components/draft_recovery_banner.tsx` | `loadDraft(orderId)`, `onDraftRejected(reason)` | `src/checkout/__tests__/draft_recovery.test.ts`, `src/checkout/components/__tests__/draft_recovery_banner.test.tsx` |
```

## Component Responsibility Map Section

After the requirement coverage matrix, include a section that names each component, its primary responsibility, and its boundary.

Rules:
- Explain why this component is the primary owner for its rows in the requirement coverage matrix.
- Note the key collaborators and the interfaces they use.
- State what the component explicitly does not own when that boundary might otherwise be ambiguous.

Example shape:

```markdown
## Component Responsibility Map

- `FeatureFlagEvaluator`: primary owner for request-time feature enablement decisions. Collaborates with `CheckoutEntryPoint` via `isCheckoutFeatureEnabled(request)`. Does not own UI fallback rendering.
- `CheckoutFormState`: primary owner for client-side field state, validation triggers, and submit readiness. Collaborates with `DraftRecoveryService` for persisted draft hydration. Does not own persistence.
- `OrderSubmissionHandler`: primary owner for translating validated form state into the backend request contract. Collaborates with `CheckoutFormState` through `buildOrderPayload(state)`.
```

## Component Interactions and Contracts Section

After the component responsibility map, include a section that makes the cross-component interfaces explicit.

Rules:
- Cover every non-trivial interaction between components that an implementer would otherwise need to rediscover.
- For each interaction, state the source component, destination component, contract name or shape, and any important invariants, failure handling, or sequencing assumptions.
- Include API routes, events, callbacks, props, selectors, schemas, queues, DB boundaries, and feature flags when they are part of the design.

Example shape:

```markdown
## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CheckoutEntryPoint` | `FeatureFlagEvaluator` | `isCheckoutFeatureEnabled(request): boolean` | Must run before any checkout UI mounts. |
| `DraftRecoveryService` | `CheckoutFormState` | `hydrateFromDraft(draft: DraftPayload)` | Reject drafts that fail schema validation and emit `onDraftRejected(reason)`. |
| `CheckoutFormState` | `OrderSubmissionHandler` | `buildOrderPayload(state): OrderRequest` | Called only after validation passes; preserves existing backend payload schema. |
```

## File Ownership Map Section

After component interactions, include a section that locks the file and folder structure and names which component primarily owns each file.

Rules:
- Each file must have exactly one primary owner component.
- If multiple components touch the same file, state which component owns it and why the other touches are incidental or extension points.
- Prefer exact file paths over directories. Group by folder only when it improves readability without hiding ownership.

Example shape:

```markdown
## File Ownership Map

- Create `src/checkout/feature_flags.ts` - owned by `FeatureFlagEvaluator`; request-time gating logic for checkout experiments.
- Modify `src/checkout/index.ts` - owned by `CheckoutEntryPoint`; wires feature gating into the existing checkout entry flow.
- Modify `src/checkout/form_state.ts` - owned by `CheckoutFormState`; extends state machine with draft hydration and validation transitions.
- Create `src/checkout/draft_recovery.ts` - owned by `DraftRecoveryService`; loads, validates, and rejects persisted drafts.
- Create `src/checkout/components/draft_recovery_banner.tsx` - owned by `DraftRecoveryBanner`; displays recovery/rejection feedback from draft hydration.
- Modify `src/checkout/__tests__/draft_recovery.test.ts` - owned by `DraftRecoveryService`; regression coverage for draft validation and rejection paths.
```

## Implementation File Allowlist Section

After the file ownership map, include a section that enumerates the exact files the implementer is allowed to touch for the main work.

Rules:
- This section must be explicit and exhaustive for intentional feature work.
- It must match the file ownership map. If a file is allowlisted but not explained in the ownership map, the plan is incomplete.
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

**Covers:** `R1`, `E1`
**Owner:** `ComponentName`
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
- The implementation model comes before the task list.
- Every requirement and material edge case must map to exactly one primary owner, concrete files, interface points, and planned tests.
- Do not start task planning until the requirement coverage matrix, component boundaries, interaction contracts, and file ownership map are explicit.
- The plan must declare the exact implementation file allowlist before task planning starts.
- Task boundaries should follow the component responsibilities decided during the Q&A.
- Each task should reference the requirement IDs it covers from the requirement coverage matrix.
- Keep the allowlist tight. Do not include speculative files "just in case".
- Do not use the task list to invent architecture that was not resolved in the implementation model.
- Ask all Q&A in the numbered `1. / A: / B: / C: / D:` text format and expect compact replies like `1A2B3A4D`.
- If tests or lint already fail outside the feature scope, record that as preexisting context rather than planning unrelated fixes.

### Output

After all steps, print a single copy-pasteable command for a fresh window and stop:

```text
/implement <path_to_impl_plan>
```
