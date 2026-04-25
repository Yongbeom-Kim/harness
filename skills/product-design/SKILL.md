---
name: product-design
description: "Turn ideas into reviewed design specs through collaborative dialogue. Produces artifacts on disk."
---

# Product Design

Collaborate with the user to turn an idea into a reviewed design doc. This skill produces two artifacts on disk:

1. **Raw Decision Log**: A log file of all decisions made during the dialogue
2. **Design Document**: Design document that comprehensively outlines all product-related decisions and edge cases as a structured document.

<HARD-GATE>
Do NOT write any code, scaffold any project, or invoke any implementation skill. This skill ends after the design document is reviewed and approved.
</HARD-GATE>

## Steps

Create a task for each step and complete them in order:
1. **Explore project context**: Check files, docs, recent commits.
2. **Clarifying Questions**: Ask **many rounds** of questions to the user, regarding product and design decisions, as well as any edge cases.
    - Ask all Q&A in plain text using this exact structure:
      `1. <QUESTION>`
      `A: ...`
      `B: ...`
      `C: ...`
      `D: ...`
    - Number questions consecutively within each round. Keep the option labels exactly `A`, `B`, `C`, `D`.
    - Put the recommended option first when possible and mark it inline with `(Recommended)`.
    - Derive the options from the current context and other files in the repository.
    - Before asking a question, navigate the current codebase and check if this question can already be answered from current context.
    - Expect the user to answer in compact form such as `1A2B3A4D`. Parse that mapping by question number. If the response is ambiguous, ask a minimal follow-up.
    - When you ask and receive an answer to the question, save it to `${PWD}/docs/development/YYYY-MM-DD-<topic>/decisions-raw.md`.
3. **Write design doc**: Save to `${PWD}/docs/development/YYYY-MM-DD-<topic>/design-document.md`.
4. **Design document review**: Read `./design-document-reviewer-prompt.md`, spawn a reviewer subagent with the design doc path, wait for it to finish, and read its output before proceeding.
5. If the reviewer requests changes, update the design doc and repeat step 4 until the reviewer approves the design.

### Output

After all steps, print a single copy-pasteable command for a fresh window and stop:

```
/implementation-design <absolute-output-directory>
```
