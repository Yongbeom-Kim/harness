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

## Round 2

Questions asked:

6. How should skills and docs that currently end in an `implement-with-reviewer` command behave after this change?
A: Remove that execution path; they should stop at the produced spec/artifact path or describe manual next steps, without inventing a replacement orchestrator command. `(Recommended)`
B: Replace it with a single-agent `tmux_codex --attach` command as the default implementation path.
C: Replace it with a documented manual two-launcher flow using `tmux_codex` and `tmux_claude`.
D: Leave those skills/docs temporarily unchanged and fix them later.

7. What should happen to the older design docs and implementation specs centered on `implement-with-reviewer`?
A: Keep them as historical records, but make the new launcher-only design the active superseding direction. `(Recommended)`
B: Rewrite older docs in place so the history reads as if the review loop was never the main direction.
C: Delete the older review-loop design docs/specs entirely.
D: Leave them all active side-by-side with no explicit superseding relationship.

8. After removing `implement-with-reviewer`, what is the product stance on `log/runs/`?
A: It is no longer part of the core product contract; treat existing `log/runs/` references as historical to the removed workflow and do not redefine it for launcher-only V1. `(Recommended)`
B: Keep `log/runs/` as a generic reserved artifact root for future workflows, even though no current binary writes it.
C: Repurpose `log/runs/` immediately for single-agent launcher session logs.
D: Keep the directory mentioned in docs but leave its meaning intentionally vague.

9. For `tmux_codex` and `tmux_claude`, is this design meant to preserve their current behavior and flags?
A: Yes. Preserve their current launcher contracts; this design is about removing other binaries and narrowing the product surface, not redesigning launcher behavior. `(Recommended)`
B: No. Use this design to add prompt/task flags to the launchers.
C: No. Use this design to change default session names or attach behavior.
D: No. Replace the two launchers with one generic launcher internally, even if two binary names remain.

10. The repo currently tracks extra binary entrypoints like `tmux_agent`, `t_codex`, `t_claude`, `t_agent`, and `implement-with-reviewer`. What should the supported product surface say about those?
A: Only `tmux_codex` and `tmux_claude` remain supported operator-facing binaries; remove the others from the supported surface and from tracked binary outputs. `(Recommended)`
B: Keep the short `t_*` wrappers as supported convenience entrypoints, but remove `implement-with-reviewer` and `tmux_agent`.
C: Keep the extra entrypoints in-repo but mark them unsupported/internal.
D: Do not make a product-level statement about helper entrypoints yet.

User response:

- Q6 = A
- Q7 = A
- Q8 = A
- Q9 = A
- Q10 = A

## Consolidated Decisions

- The harness product surface becomes launcher-only. There is no replacement workflow binary in this design.
- Remove `implement-with-reviewer` completely as an operator-facing binary, runtime surface, contract, and active design target.
- Delete review-loop-specific runtime code and product contracts rather than keeping them dormant or deprecated.
- The supported operator-facing binaries are exactly `tmux_codex` and `tmux_claude`.
- Update active docs so the core product is persistent tmux-backed single-agent launchers; multi-agent orchestration is future work or external to the core product.
- Skills and docs that currently end in an `implement-with-reviewer` command should stop at artifact output or manual next steps; they should not substitute an unreviewed replacement orchestrator command.
- Older `implement-with-reviewer` design docs and implementation specs remain as historical records, but this new design supersedes them as the active direction.
- `log/runs/` is no longer part of the active product contract after the review-loop surface is removed.
- Preserve the current `tmux_codex` and `tmux_claude` launcher behavior and flags in this design.
- Remove other tracked binary outputs and entrypoints such as `tmux_agent`, `t_codex`, `t_claude`, `t_agent`, and `implement-with-reviewer` from the supported product surface.
