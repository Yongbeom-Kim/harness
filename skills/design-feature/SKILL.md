---
name: design-feature
description: "Turn ideas into reviewed design specs through collaborative dialogue. Produces artifacts on disk."
---

# Design and Plan

Collaborate with the user to turn an idea a reviewed design doc. This skill produces two artifacts on disk:

1. **Raw Decision Log**: A log file of all decisions made during hte dialogue
2. **Design Document**: Design document that comprehensively outlines all product-related decisions and edge cases as a structured document.

<HARD-GATE>
Do NOT write any code, scaffold any project, or invoke any implementation skill. This skill ends after the implementation plan is reviewed.
</HARD-GATE>

## Steps

Create a task for each step and complete them in order:
1. **Explore project context**: Check files, docs, recent commits.
2. **Clarifying Questions**: Ask **many rounds** of questions to the user, regarding product and design decisions, as well as any edge cases.
    - When you ask and receive an answer to the question, save it to `${PWD}/docs/YYYY-MM-DD-<topic>/design/decisions-raw.md`.
3. **Write design doc**: Save to `${PWD}/docs/development/design/YYYY-MM-DD-<topic>-design.md`.
4. **Design spec review**: Read `./design-spec-document-reviewer-prompt.md` and spawn a new subagent to review it. Do NOT proceed until the agent completes and you've read its output.