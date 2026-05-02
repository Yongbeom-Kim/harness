# Implementation Decisions - Launcher-Only Surface

- Date: 2026-05-02
- Topic: Implementation architecture for removing `implement-with-reviewer` and narrowing the active harness surface to `tmux_codex` and `tmux_claude`.
- Design input: `docs/development/2026-05-02-launcher-only-surface/design-document.md`
- Product decisions input: `docs/development/2026-05-02-launcher-only-surface/decisions-raw.md`

## Project Context Snapshot

- `Makefile` still builds `bin/implement-with-reviewer` alongside `bin/tmux_codex` and `bin/tmux_claude`.
- The active review-loop product surface currently lives under `orchestrator/cmd/implement-with-reviewer/` and still owns runtime code, artifact writers, prompt shaping, turn execution, tests, and `CONTRACT.md`.
- The launcher binaries already route through `orchestrator/internal/agent`:
  - `orchestrator/cmd/tmux_codex/main.go`
  - `orchestrator/cmd/tmux_claude/main.go`
  - `orchestrator/internal/agent/standalone.go`
- The only tracked launcher contract doc today is `orchestrator/cmd/tmux_codex/CONTRACT.md`; there is no tracked `orchestrator/cmd/tmux_claude/CONTRACT.md`.
- The only active skill file that still points at the removed workflow command is `skills/implementation-design/SKILL.md`, whose final output still says to pipe the spec into `implement-with-reviewer`.
- `orchestrator/internal/filechannel/` is only referenced by `orchestrator/cmd/implement-with-reviewer/main.go`, so removing the workflow binary makes the package unreachable unless we intentionally keep it as dormant infrastructure.
- `orchestrator/cmd/tmux_agent/` exists as an empty on-disk directory, but it is not tracked by git.
- The repo currently has no tracked top-level product README or other active product doc outside command contracts and skills; the active documentation surface is effectively command contracts plus skill docs.

## Round 1

Questions asked:

1. Where should the launcher-only active product contract live once `implement-with-reviewer` is deleted?
A: Update `orchestrator/cmd/tmux_codex/CONTRACT.md` so it becomes the checked-in launcher-only contract anchor, explicitly naming both `tmux_codex` and `tmux_claude` while preserving Codex-specific invocation details where needed. `(Recommended)`
B: Create a new top-level product doc (for example `README.md`) and treat command-local contracts as secondary details.
C: Add a new `orchestrator/cmd/tmux_claude/CONTRACT.md` so each launcher has its own parallel checked-in contract.
D: Remove checked-in command contracts entirely and leave active product direction only in skills plus the new design doc.

2. What should `skills/implementation-design/SKILL.md` output at the end once the old pipeline command is gone?
A: Replace the obsolete pipeline with a neutral single-command artifact handoff such as `cat <implementation_spec_path>`, so the skill still ends with one copy-pasteable command without inventing a new orchestrator. `(Recommended)`
B: Replace it with a path-printing command such as `printf '%s\n' <implementation_spec_path>`.
C: Remove the final command requirement entirely and end with prose/manual next steps only.
D: Replace it with a launcher command such as `tmux_codex --attach`.

3. Once `implement-with-reviewer` is removed, what should happen to `orchestrator/internal/filechannel/`?
A: Delete the package and its tests because it becomes review-loop-only dead infrastructure with no remaining launcher owner. `(Recommended)`
B: Keep it in-repo as dormant reusable infrastructure even though no remaining binary imports it.
C: Keep only `manager.go` / interface types and delete the FIFO implementation/tests.
D: Move it under `orchestrator/internal/agent/tmux/` as future launcher infrastructure.

4. How should the empty on-disk `orchestrator/cmd/tmux_agent/` directory be treated in the implementation plan?
A: Include explicit directory cleanup so the local source tree no longer carries an orphan non-launcher entrypoint path after the change. `(Recommended)`
B: Ignore it because it is untracked and the tracked product surface is already defined by files and build rules.
C: Keep the directory as a placeholder for future launcher expansion, but leave it empty.
D: Replace it with a doc note explaining it is inactive.

5. What verification bar should the plan require for this cleanup?
A: Verify the narrowed surface with `go test ./...`, `make build`, and a focused grep/diff check that active tracked docs/skills/build outputs no longer promise `implement-with-reviewer` or `log/runs/` as current product surface. `(Recommended)`
B: Require only `go test ./...` because documentation and build changes are straightforward.
C: Require only `make build` because the change is mostly deletion and doc cleanup.
D: Require only targeted package tests around the remaining launchers and skip repo-wide checks.

User response:

- Q1 = A
- Q2 = custom: replace the obsolete final command with `/implement <path_to_impl_plan>` because the skill no longer references any binary.
- Q3 = A
- Q4 = A
- Q5 = A

## Consolidated Decisions So Far

- The checked-in launcher-only active product contract should be anchored in `orchestrator/cmd/tmux_codex/CONTRACT.md`; do not add new symmetry docs unless another requirement forces it.
- `skills/implementation-design/SKILL.md` should stop ending in an `implement-with-reviewer` pipeline and instead end with `/implement <path_to_impl_plan>`.
- `orchestrator/internal/filechannel/` should be deleted with the removed workflow surface rather than preserved as dormant infrastructure.
- The empty orphan `orchestrator/cmd/tmux_agent/` directory should be explicitly cleaned up so the local source tree no longer suggests another supported entrypoint.
- Verification should include `go test ./...`, `make build`, and a focused grep/diff check that active tracked docs/skills/build outputs no longer promise `implement-with-reviewer` or `log/runs/` as active product surface.

## Round 2

Questions asked:

6. The current launcher runtime code in `orchestrator/internal/agent/` and the two `tmux_*` mains already matches the desired two-binary surface. How should the implementation plan treat that code?
A: Keep launcher runtime Go code out of the planned edit set unless deleting the removed surface reveals a concrete build/test break; rely on regression tests and build verification rather than opportunistic runtime edits. `(Recommended)`
B: Touch `orchestrator/cmd/tmux_codex/main.go`, `orchestrator/cmd/tmux_claude/main.go`, and `orchestrator/internal/agent/*` to restate the launcher-only direction in code comments or small refactors.
C: Add new launcher-specific tests in the command packages even if the runtime code itself does not need changes.
D: Refactor launcher/runtime code for symmetry while removing the old workflow surface.

7. Based on the current tracked repo, the active non-historical cleanup targets are effectively `Makefile`, `orchestrator/cmd/implement-with-reviewer/**`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, and `skills/implementation-design/SKILL.md`. How broad should the plan make the active-doc/source cleanup?
A: Treat that set as the intentional cleanup scope, plus directory removal for the empty `orchestrator/cmd/tmux_agent/`; do not widen into a repo-wide wording sweep. `(Recommended)`
B: Do a broader cleanup across all non-historical files mentioning workflows or launchers, even if they are not currently part of the active surface.
C: Limit cleanup to build files and source deletions only; do not touch active docs beyond the deleted command.
D: Add new active product docs in addition to cleaning the current files.

8. If removing the workflow command leaves stale module dependencies behind, how should the plan handle `orchestrator/go.mod` and `orchestrator/go.sum`?
A: Allow them as incidental-only files for narrow dependency cleanup if the deleted workflow imports leave them stale or `go test ./...` rewrites them. `(Recommended)`
B: Require a full `go mod tidy` pass as a primary task.
C: Freeze module files even if they retain now-unused dependencies.
D: Allow broader dependency cleanup while already touching the module.

9. How should the plan prove that `tmux_codex` and `tmux_claude` still preserve their current behavior and flag contracts after the surface cleanup?
A: Use existing automated coverage plus repo-wide verification (`go test ./...`, `make build`) and explicit no-code-change intent for the launcher runtime unless verification finds a break. `(Recommended)`
B: Add new command-package tests under `orchestrator/cmd/tmux_codex` and `orchestrator/cmd/tmux_claude`.
C: Add manual test steps that launch the binaries in tmux and attach to sessions.
D: Treat documentation review alone as sufficient for launcher preservation.

User response:

- Q6 = A
- Q7 = A
- Q8 = B
- Q9 = A

## Final Consolidated Decisions

- Anchor the checked-in launcher-only active product contract in `orchestrator/cmd/tmux_codex/CONTRACT.md`; do not create extra launcher symmetry docs unless implementation proves they are needed.
- Change the final output of `skills/implementation-design/SKILL.md` to `/implement <path_to_impl_plan>`.
- Delete `orchestrator/internal/filechannel/` and its tests with the removed workflow surface.
- Explicitly clean up the orphan `orchestrator/cmd/tmux_agent/` directory so the local source tree no longer suggests another supported entrypoint.
- Keep launcher runtime Go code out of scope unless deleting the removed surface reveals a concrete build/test break.
- Keep active cleanup scope tight: `Makefile`, `orchestrator/cmd/implement-with-reviewer/**`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, `skills/implementation-design/SKILL.md`, plus cleanup of the empty `orchestrator/cmd/tmux_agent/` directory.
- Treat `orchestrator/go.mod` and `orchestrator/go.sum` as primary implementation concern via an explicit `go mod tidy` step after the workflow surface is deleted.
- Preserve `tmux_codex` and `tmux_claude` behavior through existing automated coverage and repo-wide verification rather than new launcher code or new launcher-specific tests.
- Verification should include `go test ./...`, `make build`, and a focused grep/diff check that active tracked docs/skills/build outputs no longer promise `implement-with-reviewer` or `log/runs/` as active product surface.

## Round 3

Questions asked:

10. How should `make build` handle stale non-launcher binaries that may already exist under `bin/` from older builds, such as `bin/implement-with-reviewer` or `bin/tmux_agent`?
A: Add explicit cleanup in the `build` target so it removes known stale non-launcher binaries before rebuilding the two supported launchers. `(Recommended)`
B: Leave `build` as additive and rely on operators to run `make clean` first when they want stale binaries removed.
C: Ignore stale generated binaries entirely because they are untracked.
D: Expand `clean` only, but leave `build` unable to enforce the two-binary postcondition by itself.
