# Launcher-Only Harness Surface Implementation Plan

**Goal:** Remove `implement-with-reviewer` and all other non-launcher operator surface so the active harness product consists only of `tmux_codex` and `tmux_claude`.

**Architecture:** This is a deletion-first cleanup. Remove the workflow command package and its now-unreachable FIFO side-channel support package, then tighten the build target and the one remaining checked-in launcher contract so they describe and enforce only the two supported tmux launchers. Keep launcher runtime Go code out of scope unless the deletions reveal a concrete regression; preserve launcher behavior through existing automated coverage, `make build`, and focused active-surface grep checks.

**Tech Stack:** Go 1.26.2, Make, tmux-backed launcher binaries, Markdown contract/skill docs, `go mod tidy`.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | The supported operator-facing binary surface and `make build` output must be exactly `tmux_codex` and `tmux_claude`. | `SupportedBinaryBuild` | `LauncherRegressionGuard` | `Makefile`, `orchestrator/cmd/tmux_agent/` | `build:` target, pre-build cleanup of `bin/*`, local orphan cleanup for `orchestrator/cmd/tmux_agent`, `make build` | `mkdir -p bin && touch bin/implement-with-reviewer bin/tmux_agent bin/extra-wrapper && make build && test ! -e bin/implement-with-reviewer && test ! -e bin/tmux_agent && test ! -e bin/extra-wrapper && test -e bin/tmux_codex && test -e bin/tmux_claude && test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2" && [ ! -d orchestrator/cmd/tmux_agent ]` |
| R2 | Remove `implement-with-reviewer` as an active product command by deleting its command package, tests, and command-local contract instead of leaving dormant source behind. | `WorkflowSurfaceRemoval` | `SupportedBinaryBuild`, `LauncherContractAnchor` | `orchestrator/cmd/implement-with-reviewer/main.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/cmd/implement-with-reviewer/run.go`, `orchestrator/cmd/implement-with-reviewer/run_test.go`, `orchestrator/cmd/implement-with-reviewer/types.go`, `orchestrator/cmd/implement-with-reviewer/prompts.go`, `orchestrator/cmd/implement-with-reviewer/prompts_test.go`, `orchestrator/cmd/implement-with-reviewer/turns.go`, `orchestrator/cmd/implement-with-reviewer/turns_test.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`, `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`, `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`, `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | Deleted package boundary `./cmd/implement-with-reviewer`, removed CLI flag set name `implement-with-reviewer`, removed checked-in workflow contract | `test ! -d orchestrator/cmd/implement-with-reviewer`, `cd orchestrator && go test ./...` |
| R3 | Remove review-loop-specific runtime code that no remaining launcher imports. | `WorkflowInfrastructureRemoval` | `WorkflowSurfaceRemoval`, `ModuleDependencyCleanup` | `orchestrator/internal/filechannel/fifo_manager.go`, `orchestrator/internal/filechannel/manager.go`, `orchestrator/internal/filechannel/fifo_test.go` | Deleted package boundary `internal/filechannel`, removed `NewFIFOManager`, removed `ChannelManager` | `test ! -d orchestrator/internal/filechannel`, `cd orchestrator && go test ./...` |
| R4 | The active checked-in product contract must describe the harness as launcher-only, name only `tmux_codex` and `tmux_claude`, and stop presenting orchestration or `log/runs/` as current product contract. | `LauncherContractAnchor` | `SupportedBinaryBuild`, `HistoricalBoundaryGuard` | `orchestrator/cmd/tmux_codex/CONTRACT.md` | Product-surface statement inside `CONTRACT.md`, codex-specific invocation/validation/runtime sections, explicit active-surface wording | `rg -n "tmux_claude|supported operator-facing binaries|launcher-only" orchestrator/cmd/tmux_codex/CONTRACT.md`, `! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/tmux_codex/CONTRACT.md` |
| R5 | Active skill instructions that previously ended in `implement-with-reviewer` must stop doing that and hand off to `/implement <path_to_impl_plan>` without inventing a new binary. | `ImplementationDesignSkillOutput` | `HistoricalBoundaryGuard` | `skills/implementation-design/SKILL.md` | Final `### Output` block, `/implement <path_to_impl_plan>` handoff to `@implement` | `! rg -n "implement-with-reviewer" skills/implementation-design/SKILL.md`, `rg -n "/implement <path_to_impl_plan>" skills/implementation-design/SKILL.md` |
| R6 | Preserve the existing behavior and flag contracts of `tmux_codex` and `tmux_claude`; this cleanup must not redesign launcher runtime behavior. | `LauncherRegressionGuard` | `SupportedBinaryBuild`, `LauncherContractAnchor` | `orchestrator/internal/agent/standalone.go`, `orchestrator/internal/agent/standalone_test.go`, `orchestrator/internal/agent/agent_test.go`, `orchestrator/cmd/tmux_codex/main.go`, `orchestrator/cmd/tmux_claude/main.go`, `orchestrator/cmd/tmux_codex/CONTRACT.md` | `RunStandalone(args, StandaloneConfig)`, `NewCodexAgent(sessionName)`, `NewClaudeAgent(sessionName)`, `--session`, `--attach` | Existing coverage in `orchestrator/internal/agent/standalone_test.go` and `orchestrator/internal/agent/agent_test.go`, plus `cd orchestrator && go test ./...` and `make build` |
| R7 | After workflow-source deletion, module metadata must be reconciled with a real `go mod tidy` run rather than leaving stale workflow-only requirements behind. | `ModuleDependencyCleanup` | `WorkflowSurfaceRemoval`, `WorkflowInfrastructureRemoval` | `orchestrator/go.mod`, `orchestrator/go.sum` | `go mod tidy`, removal of `require github.com/google/uuid v1.6.0` | `! rg -n "google/uuid" orchestrator/go.mod orchestrator/go.sum 2>/dev/null`, `cd orchestrator && go test ./...` |
| E1 | Stale non-launcher binaries or wrapper remnants may already exist under `bin/` from older builds or local experiments; `make build` alone must clear them so the post-build state still reflects exactly two supported binaries. | `SupportedBinaryBuild` | `LauncherRegressionGuard` | `Makefile` | `rm -f "$(ROOT)/bin"/*` before rebuild | `mkdir -p bin && touch bin/implement-with-reviewer bin/tmux_agent bin/extra-wrapper && make build && test ! -e bin/implement-with-reviewer && test ! -e bin/tmux_agent && test ! -e bin/extra-wrapper && test -e bin/tmux_codex && test -e bin/tmux_claude && test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2"` |
| E2 | Historical docs under `docs/development/` may continue to mention the removed workflow, but feature implementation must not rewrite those historical records. | `HistoricalBoundaryGuard` | `ImplementationDesignSkillOutput`, `LauncherContractAnchor` | `docs/development/` | Implementation file allowlist, `@implement` boundary check, focused grep restricted to active files only | `@implement` boundary check must report no out-of-bounds edits under `docs/development/` |
| E3 | Existing `log/runs/` directories on disk may remain untouched; the feature removes the active contract, not the historical artifacts themselves. | `HistoricalBoundaryGuard` | `LauncherContractAnchor`, `WorkflowSurfaceRemoval` | `log/runs/`, `orchestrator/cmd/tmux_codex/CONTRACT.md` | Allowlist exclusion for `log/runs/`, active-contract wording in `CONTRACT.md` | `@implement` boundary check must report no out-of-bounds edits under `log/runs/`; `! rg -n "log/runs" Makefile orchestrator/cmd/tmux_codex/CONTRACT.md skills/implementation-design/SKILL.md` |
| E4 | `orchestrator/cmd/tmux_agent/` may exist locally only as an untracked empty directory; cleanup must remove it if present even though no tracked diff may result. | `SupportedBinaryBuild` | `HistoricalBoundaryGuard` | `orchestrator/cmd/tmux_agent/` | `rmdir orchestrator/cmd/tmux_agent` | `[ ! -d orchestrator/cmd/tmux_agent ]` |

## Component Responsibility Map

- `SupportedBinaryBuild`: primary owner for the supported binary set and the build recipe that enforces it. It updates `Makefile` so `make build` creates only the two supported launchers, clears stale `bin/` outputs first, and cleans the empty `orchestrator/cmd/tmux_agent/` directory if it still exists locally. It does not own launcher runtime behavior or active documentation wording beyond the build target.
- `WorkflowSurfaceRemoval`: primary owner for removing the active `implement-with-reviewer` command surface. It deletes the whole command package, its tests, its artifact writers, its side-channel code, and its checked-in contract, rather than leaving a dormant command tree in the repo. It does not own module tidy or the surviving launcher contract.
- `WorkflowInfrastructureRemoval`: primary owner for deleting `orchestrator/internal/filechannel`, which becomes unreachable once the workflow command is removed. It does not own build outputs or documentation.
- `ModuleDependencyCleanup`: primary owner for reconciling module metadata after the source deletions. It runs `go mod tidy` and owns the resulting `go.mod` / `go.sum` state. It does not choose which source packages are removed.
- `LauncherContractAnchor`: primary owner for the only remaining checked-in active product contract. It rewrites `orchestrator/cmd/tmux_codex/CONTRACT.md` so the file anchors the launcher-only product statement while preserving Codex-specific invocation, validation, and runtime details. It does not own launcher runtime code and must not add a new `tmux_claude` contract just for symmetry.
- `ImplementationDesignSkillOutput`: primary owner for removing the obsolete workflow command from `skills/implementation-design/SKILL.md` and replacing it with the new `/implement <path_to_impl_plan>` handoff. It does not own runtime behavior and must not invent a replacement binary.
- `LauncherRegressionGuard`: primary owner for the preservation boundary around `tmux_codex` / `tmux_claude`. It keeps launcher runtime Go files out of scope unless the deletions expose a concrete failure, and it relies on existing tests plus repo-wide verification to prove behavior was preserved. It does not own active doc wording.
- `HistoricalBoundaryGuard`: primary owner for keeping historical records historical. It excludes `docs/development/` and `log/runs/` from the implementation allowlist, ensures active-surface grep checks target current build/docs/skills only, and prevents the feature from rewriting archival material. It does not own active product files.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `SupportedBinaryBuild` | `WorkflowSurfaceRemoval` | `build:` target no longer invokes `./cmd/implement-with-reviewer` and clears stale `bin/` outputs before rebuilding the surviving launchers. | Build cleanup must happen before or with command deletion so `make build` keeps enforcing the new two-binary postcondition. |
| `WorkflowSurfaceRemoval` | `WorkflowInfrastructureRemoval` | Deleting `./cmd/implement-with-reviewer` leaves `internal/filechannel` with no remaining importers. | The two deletions belong in the same feature so no dormant workflow-only package survives. |
| `WorkflowSurfaceRemoval` | `ModuleDependencyCleanup` | After the workflow command disappears, `go mod tidy` removes the last `github.com/google/uuid` dependency. | `go mod tidy` is required explicitly, not as an incidental side effect. |
| `LauncherContractAnchor` | `SupportedBinaryBuild` | The contract anchor says the supported binaries are exactly `tmux_codex` and `tmux_claude`; `Makefile` must enforce the same set. | Contract wording and build output must agree. |
| `ImplementationDesignSkillOutput` | `LauncherContractAnchor` | Skill output hands off to `/implement <path_to_impl_plan>` and must not imply that any launcher binary is a replacement for the removed workflow command. | This keeps higher-level execution in skills rather than core product binaries. |
| `LauncherRegressionGuard` | `SupportedBinaryBuild` | Existing launcher/runtime tests plus `make build` validate that build/doc cleanup did not change launcher semantics. | No intentional edits are planned in launcher runtime files. |
| `HistoricalBoundaryGuard` | All changing components | Implementation file allowlist excludes `docs/development/` and `log/runs/`; active-surface grep checks target only `Makefile`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, and `skills/implementation-design/SKILL.md`. | Historical mentions of `implement-with-reviewer` remain valid history and must not block the feature. |

## File Ownership Map

- Modify `Makefile` - owned by `SupportedBinaryBuild`; stop building `bin/implement-with-reviewer`, ensure `bin/` exists for fresh builds, and clear stale `bin/` outputs before rebuilding the two supported launchers.
- Delete `orchestrator/cmd/implement-with-reviewer/main.go` and `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `WorkflowSurfaceRemoval`; remove the deleted CLI entrypoint plus its CLI-validation and lock-behavior tests.
- Delete `orchestrator/cmd/implement-with-reviewer/run.go`, `orchestrator/cmd/implement-with-reviewer/run_test.go`, and `orchestrator/cmd/implement-with-reviewer/types.go` - owned by `WorkflowSurfaceRemoval`; remove the workflow run loop, run-local types, and orchestration tests.
- Delete `orchestrator/cmd/implement-with-reviewer/prompts.go`, `orchestrator/cmd/implement-with-reviewer/prompts_test.go`, `orchestrator/cmd/implement-with-reviewer/turns.go`, `orchestrator/cmd/implement-with-reviewer/turns_test.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel.go`, and `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go` - owned by `WorkflowSurfaceRemoval`; remove workflow prompt shaping, completion-marker logic, and side-channel delivery behavior.
- Delete `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`, `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`, and `orchestrator/cmd/implement-with-reviewer/artifacts_test.go` - owned by `WorkflowSurfaceRemoval`; remove the active `log/runs` artifact writer implementation and its schema/layout tests.
- Delete `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `WorkflowSurfaceRemoval`; remove the active checked-in contract for the deleted workflow binary.
- Delete `orchestrator/internal/filechannel/fifo_manager.go`, `orchestrator/internal/filechannel/manager.go`, and `orchestrator/internal/filechannel/fifo_test.go` - owned by `WorkflowInfrastructureRemoval`; remove the now-unreachable FIFO transport package.
- Modify `orchestrator/go.mod` - owned by `ModuleDependencyCleanup`; drop the workflow-only `github.com/google/uuid` requirement after the command deletion.
- Modify or delete `orchestrator/go.sum` - owned by `ModuleDependencyCleanup`; reflect the exact post-`go mod tidy` module state.
- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `LauncherContractAnchor`; add the launcher-only product-surface statement, keep Codex-specific runtime details, and remove any active contract dependence on orchestration or `log/runs/`.
- Modify `skills/implementation-design/SKILL.md` - owned by `ImplementationDesignSkillOutput`; replace the removed workflow-command handoff with `/implement <path_to_impl_plan>`.
- Delete empty directory `orchestrator/cmd/tmux_agent/` if it exists at implementation time - owned by `SupportedBinaryBuild`; local orphan-entrypoint cleanup only, with no tracked diff expected if the directory is already absent or untracked.
- Preserve `orchestrator/internal/agent/standalone.go`, `orchestrator/internal/agent/agent_test.go`, `orchestrator/internal/agent/standalone_test.go`, `orchestrator/cmd/tmux_codex/main.go`, and `orchestrator/cmd/tmux_claude/main.go` unchanged - guarded by `LauncherRegressionGuard`; launcher runtime stays out of bounds unless the deletions reveal a concrete build/test break.
- Preserve `docs/development/` and `log/runs/` unchanged - guarded by `HistoricalBoundaryGuard`; both stay outside the implementation allowlist as historical records.

## Implementation File Allowlist

**Primary files:**
- `Makefile`
- `orchestrator/cmd/implement-with-reviewer/main.go`
- `orchestrator/cmd/implement-with-reviewer/main_test.go`
- `orchestrator/cmd/implement-with-reviewer/run.go`
- `orchestrator/cmd/implement-with-reviewer/run_test.go`
- `orchestrator/cmd/implement-with-reviewer/types.go`
- `orchestrator/cmd/implement-with-reviewer/prompts.go`
- `orchestrator/cmd/implement-with-reviewer/prompts_test.go`
- `orchestrator/cmd/implement-with-reviewer/turns.go`
- `orchestrator/cmd/implement-with-reviewer/turns_test.go`
- `orchestrator/cmd/implement-with-reviewer/sidechannel.go`
- `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`
- `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`
- `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`
- `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`
- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- `orchestrator/internal/filechannel/fifo_manager.go`
- `orchestrator/internal/filechannel/manager.go`
- `orchestrator/internal/filechannel/fifo_test.go`
- `orchestrator/go.mod`
- `orchestrator/go.sum`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `skills/implementation-design/SKILL.md`
- `orchestrator/cmd/tmux_agent/` - empty directory cleanup only if present locally

**Incidental-only files:**
- None expected. If implementation appears to require touching `orchestrator/internal/agent/**`, `orchestrator/cmd/tmux_codex/main.go`, `orchestrator/cmd/tmux_claude/main.go`, `docs/development/**`, or `log/runs/**`, stop and update the plan instead of widening scope implicitly.

## Task List

### Task 1: SupportedBinaryBuild

**Files:**
- Modify: `Makefile`
- Delete: `orchestrator/cmd/tmux_agent/` (directory cleanup only if present locally)
- Test: repo-root shell checks for `make build` output and orphan-directory removal

**Covers:** `R1`, `E1`, `E4`
**Owner:** `SupportedBinaryBuild`
**Why:** The build target itself must enforce the two-binary surface. If `make build` still creates or leaves behind non-launcher outputs, the product surface stays ambiguous even after source deletion.

- [ ] **Step 1: Write the failing build-surface check**

```bash
mkdir -p bin
touch bin/implement-with-reviewer bin/tmux_agent bin/extra-wrapper
make build
test ! -e bin/implement-with-reviewer
test ! -e bin/tmux_agent
test ! -e bin/extra-wrapper
test -e bin/tmux_codex
test -e bin/tmux_claude
test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2"
test ! -d orchestrator/cmd/tmux_agent
```

- [ ] **Step 2: Run the build-surface check to verify it fails**

Run: `mkdir -p bin && touch bin/implement-with-reviewer bin/tmux_agent bin/extra-wrapper && make build && test ! -e bin/implement-with-reviewer && test ! -e bin/tmux_agent && test ! -e bin/extra-wrapper && test -e bin/tmux_codex && test -e bin/tmux_claude && test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2" && test ! -d orchestrator/cmd/tmux_agent`
Expected: FAIL because the current `build:` target still leaves non-launcher files in `bin/`, so the exact-two-binaries assertion does not hold before the Makefile cleanup. The final directory check also fails if the orphan `orchestrator/cmd/tmux_agent/` path is still present locally.

- [ ] **Step 3: Write the minimal implementation**

```make
build:
	@mkdir -p "$(ROOT)/bin"
	rm -f "$(ROOT)/bin"/*
	cd orchestrator && go build -o ../bin/tmux_codex ./cmd/tmux_codex
	cd orchestrator && go build -o ../bin/tmux_claude ./cmd/tmux_claude
```

Also remove the orphan directory if it exists:

```bash
rmdir orchestrator/cmd/tmux_agent 2>/dev/null || true
```

- [ ] **Step 4: Re-run the build-surface check**

Run: `mkdir -p bin && touch bin/implement-with-reviewer bin/tmux_agent bin/extra-wrapper && make build && test ! -e bin/implement-with-reviewer && test ! -e bin/tmux_agent && test ! -e bin/extra-wrapper && test -e bin/tmux_codex && test -e bin/tmux_claude && test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2" && test ! -d orchestrator/cmd/tmux_agent`
Expected: PASS, proving `make build` leaves exactly the two supported launchers and no orphan launcher directory.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "build: narrow harness binaries to launchers"
```

Do not stage `orchestrator/cmd/tmux_agent/`; the directory cleanup is local-only and produces no tracked diff when the path is already empty or untracked.

### Task 2: WorkflowSurfaceRemoval

**Files:**
- Delete: `orchestrator/cmd/implement-with-reviewer/main.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/main_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/run.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/run_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/types.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/prompts.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/prompts_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/turns.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/turns_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/sidechannel.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`
- Delete: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- Test: `cd orchestrator && go test ./...`

**Covers:** `R2`
**Owner:** `WorkflowSurfaceRemoval`
**Why:** The launcher-only product cannot keep a dormant workflow command in the active source tree. The deletion must remove the code, tests, and checked-in contract files together.

- [ ] **Step 1: Write the failing workflow-surface check**

```bash
test ! -d orchestrator/cmd/implement-with-reviewer
! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/implement-with-reviewer 2>/dev/null
```

- [ ] **Step 2: Run the workflow-surface check to verify it fails**

Run: `test ! -d orchestrator/cmd/implement-with-reviewer && ! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/implement-with-reviewer 2>/dev/null`
Expected: FAIL because the command directory still exists and the deleted workflow surface still produces grep hits before deletion.

- [ ] **Step 3: Write the minimal implementation**

```bash
rm -rf orchestrator/cmd/implement-with-reviewer
```

- [ ] **Step 4: Run repository tests to verify the remaining launcher code still builds**

Run: `test ! -d orchestrator/cmd/implement-with-reviewer && cd orchestrator && go test ./...`
Expected: PASS, proving the workflow command surface is gone without breaking the remaining packages.

- [ ] **Step 5: Commit**

```bash
git add -u orchestrator/cmd/implement-with-reviewer
git commit -m "refactor: remove workflow command surface"
```

### Task 3: WorkflowInfrastructureRemoval

**Files:**
- Delete: `orchestrator/internal/filechannel/fifo_manager.go`
- Delete: `orchestrator/internal/filechannel/manager.go`
- Delete: `orchestrator/internal/filechannel/fifo_test.go`
- Test: `cd orchestrator && go test ./...`

**Covers:** `R3`
**Owner:** `WorkflowInfrastructureRemoval`
**Why:** Once the workflow command is removed, the FIFO side-channel package has no remaining importers. Keeping it would leave dormant workflow-only infrastructure in the launcher-only repo surface.

- [ ] **Step 1: Write the failing infrastructure check**

```bash
test ! -d orchestrator/internal/filechannel
! rg -n "NewFIFOManager|ChannelManager" orchestrator/internal/filechannel 2>/dev/null
```

- [ ] **Step 2: Run the infrastructure check to verify it fails**

Run: `test ! -d orchestrator/internal/filechannel && ! rg -n "NewFIFOManager|ChannelManager" orchestrator/internal/filechannel 2>/dev/null`
Expected: FAIL because the FIFO side-channel package still exists and still exports workflow-only symbols before deletion.

- [ ] **Step 3: Write the minimal implementation**

```bash
rm -rf orchestrator/internal/filechannel
```

- [ ] **Step 4: Run repository tests to verify the remaining launcher code still builds**

Run: `test ! -d orchestrator/internal/filechannel && cd orchestrator && go test ./...`
Expected: PASS, proving the now-unreachable workflow transport package is gone without breaking the remaining launcher code.

- [ ] **Step 5: Commit**

```bash
git add -u orchestrator/internal/filechannel
git commit -m "refactor: remove workflow channel package"
```

### Task 4: ModuleDependencyCleanup

**Files:**
- Modify: `orchestrator/go.mod`
- Modify or delete: `orchestrator/go.sum`
- Test: `cd orchestrator && go mod tidy`, `cd orchestrator && go test ./...`

**Covers:** `R7`
**Owner:** `ModuleDependencyCleanup`
**Why:** Once the workflow command is gone, the module file must stop advertising workflow-only dependencies. This task makes the post-deletion module state explicit instead of leaving stale requirements behind.

- [ ] **Step 1: Write the failing dependency check**

```bash
! rg -n "google/uuid" orchestrator/go.mod orchestrator/go.sum 2>/dev/null
```

- [ ] **Step 2: Run the dependency check to verify it fails**

Run: `! rg -n "google/uuid" orchestrator/go.mod orchestrator/go.sum 2>/dev/null`
Expected: FAIL because the deleted workflow command was the last consumer of `github.com/google/uuid`, but the dependency is still present before `go mod tidy`.

- [ ] **Step 3: Write the minimal implementation**

```bash
cd orchestrator && go mod tidy
```

- [ ] **Step 4: Re-run dependency and repository verification**

Run: `! rg -n "google/uuid" orchestrator/go.mod orchestrator/go.sum 2>/dev/null && cd orchestrator && go test ./...`
Expected: PASS. It is acceptable if `orchestrator/go.sum` disappears entirely.

- [ ] **Step 5: Commit**

```bash
git add -u orchestrator/go.mod orchestrator/go.sum
git commit -m "build: tidy modules after workflow removal"
```

### Task 5: LauncherContractAnchor

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Test: contract-text grep checks plus repo-wide verification

**Covers:** `R4`, `R6`, `E3`
**Owner:** `LauncherContractAnchor`
**Why:** After deleting the workflow command, one active checked-in contract still needs to define what the product is now. That contract must anchor the launcher-only story without changing launcher runtime behavior.

- [ ] **Step 1: Write the failing contract check**

```bash
rg -n "tmux_claude|supported operator-facing binaries|launcher-only" orchestrator/cmd/tmux_codex/CONTRACT.md
! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/tmux_codex/CONTRACT.md
```

- [ ] **Step 2: Run the contract check to verify it fails**

Run: `rg -n "tmux_claude|supported operator-facing binaries|launcher-only" orchestrator/cmd/tmux_codex/CONTRACT.md && ! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/tmux_codex/CONTRACT.md`
Expected: FAIL because the current file still reads as a single-command contract and does not yet anchor the launcher-only product surface.

- [ ] **Step 3: Write the minimal implementation**

Add a launcher-only product-surface section near the top of the contract, for example:

```md
## Product Surface

The supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`

This file documents the `tmux_codex` member of that launcher-only surface. No workflow binary or `log/runs/` artifact contract remains part of the active product surface.
```

Keep the existing Codex-specific invocation, validation, runtime model, attach behavior, and exit-code details intact.

- [ ] **Step 4: Re-run the contract check**

Run: `rg -n "tmux_claude|supported operator-facing binaries|launcher-only" orchestrator/cmd/tmux_codex/CONTRACT.md && ! rg -n "implement-with-reviewer|log/runs" orchestrator/cmd/tmux_codex/CONTRACT.md`
Expected: PASS. The contract now anchors the launcher-only surface and no longer mentions the removed workflow or `log/runs` as an active contract.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/tmux_codex/CONTRACT.md
git commit -m "docs: anchor launcher-only harness contract"
```

### Task 6: ImplementationDesignSkillOutput

**Files:**
- Modify: `skills/implementation-design/SKILL.md`
- Test: active-surface grep checks, `cd orchestrator && go test ./...`, `make build`

**Covers:** `R5`, `E2`
**Owner:** `ImplementationDesignSkillOutput`
**Why:** Active instructions cannot end in a removed binary. This task finishes the operator-facing transition and proves the remaining active files no longer advertise the removed workflow surface.

- [ ] **Step 1: Write the failing skill-output check**

```bash
! rg -n "implement-with-reviewer" skills/implementation-design/SKILL.md
rg -n "/implement <path_to_impl_plan>" skills/implementation-design/SKILL.md
```

- [ ] **Step 2: Run the skill-output check to verify it fails**

Run: `! rg -n "implement-with-reviewer" skills/implementation-design/SKILL.md && rg -n "/implement <path_to_impl_plan>" skills/implementation-design/SKILL.md`
Expected: FAIL because the current final `### Output` block still names `implement-with-reviewer`.

- [ ] **Step 3: Write the minimal implementation**

Replace the final output block so it hands off to `@implement` without naming a binary:

```md
### Output

After all steps, print a single copy-pasteable command for a fresh window and stop:

```text
/implement <path_to_impl_plan>
```
```

- [ ] **Step 4: Run final active-surface verification**

Run: `! rg -n "implement-with-reviewer|log/runs" Makefile orchestrator/cmd/tmux_codex/CONTRACT.md skills/implementation-design/SKILL.md && test ! -d orchestrator/cmd/implement-with-reviewer && test ! -d orchestrator/internal/filechannel && (cd orchestrator && go test ./...) && make build && test ! -e bin/implement-with-reviewer && test ! -e bin/tmux_agent && test -e bin/tmux_codex && test -e bin/tmux_claude && test "$(find bin -maxdepth 1 -type f | wc -l | tr -d '[:space:]')" = "2"`
Expected:
- The grep returns no matches in active tracked files.
- The deleted workflow directories remain absent.
- `cd orchestrator && go test ./...` passes.
- The final binary assertions prove only `bin/tmux_claude` and `bin/tmux_codex` remain in scope.

- [ ] **Step 5: Commit**

```bash
git add skills/implementation-design/SKILL.md
git commit -m "docs: update implementation-design handoff"
```
