# Raw Decisions - Launcher-Only Surface

- Date: 2026-05-02
- Topic: Remove `implement-with-reviewer` and keep only `tmux_codex` and `tmux_claude` as operator-facing binaries.
- Seed direction from user: remove `implement-with-reviewer`; final operator-facing binaries should be `tmux_codex` and `tmux_claude`.

## Round 1

Questions asked:

1. Once `implement-with-reviewer` is removed, what should the product promise as the replacement for multi-agent workflows?
A: No replacement CLI; the harness product surface becomes launcher-only, and higher-level workflows are expressed in skills/docs on top of `tmux_codex` and `tmux_claude`. `(Recommended)`
B: Keep the review loop as an internal Go package/API, but not as an operator-facing binary.
C: Replace `implement-with-reviewer` with a new generic workflow binary later, but not in this design.
D: Keep `implement-with-reviewer` available as an unsupported/hidden binary outside the main build.

2. What should happen to the current implementer/reviewer runtime code and artifacts if we adopt the launcher-only surface?
A: Delete the `implement-with-reviewer` command and its review-loop-specific runtime/docs/artifact contract entirely; only preserve old run artifacts as historical files already on disk. `(Recommended)`
B: Delete the binary, but keep the command package/runtime code in-repo as dormant internal code for later reuse.
C: Keep the command package but stop building/installing the binary.
D: Keep the code and artifact contract fully documented, just mark it deprecated.

3. How strict do you want the final operator-facing binary set to be?
A: Exactly two canonical binaries: `tmux_codex` and `tmux_claude`; remove extra operator-facing binaries tied to other agents or workflows from the supported surface. `(Recommended)`
B: Keep `tmux_codex` and `tmux_claude` as the official binaries, but allow extra helper aliases/scripts to remain.
C: Keep `tmux_agent`-style extensibility hooks, but do not ship the agent binary right now.
D: Keep the current helper/alias landscape unchanged except for removing `implement-with-reviewer`.

4. What should happen to docs and contracts that currently describe the review loop as the “first concrete workflow”?
A: Rewrite them so the core product is persistent tmux-backed single-agent launchers, and treat multi-agent orchestration as future or external-to-core. `(Recommended)`
B: Keep the current narrative, but mark `implement-with-reviewer` as removed for now.
C: Split docs into “core launcher product” and “archived review-loop experiment.”
D: Leave most docs alone and only update build/readme/contract files directly affected by the removed binary.

User response:

- Raw compact answer: `1AA3A4A`
- Parsed with confidence:
  - Q1 = A
  - Q3 = A
  - Q4 = A
- Q2 is ambiguous because the `2` marker is missing from the compact response.
- Working inference pending confirmation: user likely intended `1A2A3A4A`.

## Follow-up

5. Please confirm your answer for question 2 from the previous round.
A: Yes, question 2 is `A`: delete `implement-with-reviewer` and its review-loop-specific runtime/docs/artifact contract entirely; only preserve historical artifacts already on disk. `(Recommended)`
B: No, question 2 is `B`: delete the binary, but keep the command package/runtime code dormant in-repo.
C: No, question 2 is `C`: keep the command package but stop building/installing the binary.
D: No, question 2 is `D`: keep the code and artifact contract, but mark them deprecated.

User response:

- Q5 = A
- Confirmed interpretation: Q2 = A
