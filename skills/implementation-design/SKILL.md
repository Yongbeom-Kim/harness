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

Write the implementation plan to `${PWD}/docs/development/YYYY-MM-DD-<topic>/implementation-spec.md`.

## Component Breakdown

Before defining tasks, map out all changes that need to be made, and come up with a logical model of which "component" is responsible for which changes. This is possibly the MOST IMPORTANT step of the planning process.

Ask yourself: "Which component should take responsibility for this operation?" and check whether such a component already exists.

If such a component already exists, consider extending the pre-existing component to support the requirement. If such a component does not exist, we need to create a new component.

## File Structure

After looking at the component and responsibility structure, map out which files will be created or modified and what each one is responsible for. This is where decomposition decisions get locked in.

- Design units with clear boundaries and well-defined interfaces. Each file should have one clear responsibility.
- You reason best about code you can hold in context at once, and your edits are more reliable when files are focused. Prefer smaller, focused files over large ones that do too much.
- Files that change together should live together. Split by responsibility, not by technical layer.
- In existing codebases, follow established patterns. If the codebase uses large files, don't unilaterally restructure - but if a file you're modifying has grown unwieldy, including a split in the plan is reasonable.

This structure informs the task decomposition. Each task should produce self-contained changes that make sense independently.

## Bite-Sized Task Granularity

**Each step is one action (2-5 minutes):**
- "Write the failing test" - step
- "Run it to make sure it fails" - step
- "Implement the minimal code to make the test pass" - step
- "Run the tests and make sure they pass" - step
- "Commit" - step

## Plan Document Header

**Every plan MUST start with this header:**

```markdown
# [Feature Name] Implementation Plan

**Goal:** [One sentence describing what this builds]

**Architecture:** [2-3 sentences about approach]

**Tech Stack:** [Key technologies/libraries]

---
```

## Task Structure

````markdown
### Task N: [Component Name]

**Files:**
- Create: `exact/path/to/file.py`
- Modify: `exact/path/to/existing.py:123-145`
- Test: `tests/exact/path/to/test.py`

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
- Exact file paths always
- Complete code in plan (not "add validation")
- Exact commands with expected output
- Reference relevant skills with @ syntax
- DRY, YAGNI, TDD, frequent commits

## Review

After writing the implementation spec, read `./implementation-spec-reviewer-prompt.md`, spawn a reviewer subagent with that prompt, pass the implementation spec path plus the design spec path, wait for it to finish, apply any requested fixes, and repeat until the reviewer approves.


### Output

After all steps, print a single copy-pasteable command for a fresh window and stop:

```
cat <implementation_spec_path> | implement-with-reviewer --implementer codex --reviewer codex
```
