---
name: product-design
description: "Turn ideas into reviewed design specs through collaborative dialogue. Produces artifacts on disk."
---

# Product Design

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
    - Questions should ALWAYS be multiple choice, with the options derived from the current context and other files in the repository.
    - Before asking a question, navigate the current codebase, and check if this question can be answered with your current context. Always try to provide a recommended option for each question.
    - When you ask and receive an answer to the question, save it to `${PWD}/docs/YYYY-MM-DD-<topic>/decisions-raw.md`.
3. **Write design doc**: Save to `${PWD}/docs/development/YYYY-MM-DD-<topic>/design-document.md`.
4. **Design document review**: Read `./design-document-reviewer-prompt.md` and spawn a new subagent to review it. Do NOT proceed until the agent completes and you've read its output.
5. If the subagent requests any changes, make said change, and go back to step 4 (review again with a new subagent). Repeat until the agent approves the design.

### Output

After all steps, print a copy-pasteable command for a fresh window and stop:

```
## Ready for Implementation Planning

Copy this into a new conversation to start implementation planning session:

Invoke the implementation-design skill on the following documents in the directory ${PWD}/docs/YYYY-MM-DD-<topic>/
```