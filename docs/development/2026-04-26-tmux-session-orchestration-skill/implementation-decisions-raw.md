# Raw Implementation Decisions

## 2026-04-26 Project Context

- Design inputs reviewed:
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/design-document.md`
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/decisions-raw.md`
- Current implementation surface reviewed:
  - `orchestrator/cmd/implement-with-reviewer/main.go`
  - `orchestrator/cmd/implement-with-reviewer/main_test.go`
  - `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
  - `orchestrator/internal/cli/interface.go`
  - `orchestrator/internal/cli/command.go`
  - `orchestrator/internal/cli/codex.go`
  - `orchestrator/internal/cli/claude.go`
  - `orchestrator/internal/cli/codex_test.go`
  - `orchestrator/internal/cli/claude_test.go`
- Current repository shape is intentionally small:
  - one command entrypoint package under `orchestrator/cmd/implement-with-reviewer`
  - one backend adapter package under `orchestrator/internal/cli`
  - no existing runtime/artifact/tmux helper packages yet
- Current command flow is concentrated in `main.go`, while backend execution details live in `internal/cli`.
- The design already fixes product-level scope for v1; the remaining implementation questions are about component ownership, file/package boundaries, prompt composition boundaries, and test strategy.

## 2026-04-26 Question Round 1

1. Which package should own the expanded run loop, tmux session lifecycle, prompt composition, and artifact persistence while keeping `main.go` focused on CLI parsing and wiring?
A: Create `orchestrator/internal/implementwithreviewer/` as the feature package, move runtime orchestration into it, and leave `cmd/implement-with-reviewer/main.go` as a thin entrypoint. (Recommended)
B: Keep the full runtime orchestration in `orchestrator/cmd/implement-with-reviewer/main.go` and expand that file with more helpers.
C: Create a more generic `orchestrator/internal/run/` package for lifecycle/state handling and keep feature-specific prompt logic in `cmd/implement-with-reviewer/main.go`.
D: Push orchestration into `orchestrator/internal/cli/` so adapters and run control share one package.

2. How should shared pane-local tmux mechanics be implemented across `codex` and `claude` while still keeping backend ownership in the adapters?
A: Add a narrow shared helper in `orchestrator/internal/cli/` for paste/capture/reset/wait primitives, while `codex` and `claude` still own launch commands and startup prompts. (Recommended)
B: Implement pane-local tmux logic separately in `orchestrator/internal/cli/codex.go` and `orchestrator/internal/cli/claude.go`, sharing only `command.go`.
C: Create a dedicated `orchestrator/internal/tmux/` subsystem that both adapters and the run controller call into.
D: Move wait/capture/reset logic into the run controller and keep adapters responsible only for backend launch strings.

3. Where should the per-turn `<promise>done</promise>` prompt suffix be appended?
A: In the run controller when it builds each implementer/reviewer turn prompt, so adapters receive a complete prompt payload and only own startup prompts plus completion detection. (Recommended)
B: Inside each adapter right before the prompt is pasted into tmux, so completion instructions live next to wait logic.
C: Only in the adapter startup prompt, relying on the persistent conversation to keep honoring it.
D: Per backend, with `codex` and `claude` allowed to use different suffix text.

4. How explicit should runtime artifact persistence be as its own component?
A: Give artifact persistence a dedicated owner component inside the new feature package, with focused files for run paths, metadata/state/result writing, and capture naming. (Recommended)
B: Keep artifact writing inline inside the main orchestration flow with a few local helper functions.
C: Let adapters write their own capture files and have the run controller write only metadata and final result files.
D: Reduce v1 scope by writing only `result.json` plus raw captures, leaving `metadata.json` and `state-transitions.jsonl` out.

5. How should tmux-backed integration coverage be made deterministic enough for `go test`?
A: Use real `tmux`, but inject fake `codex`/`claude` executables through `PATH` so integration tests cover the actual pane/session flow without depending on external services; keep live-backend tests optional and separately gated. (Recommended)
B: Extend only the existing env-gated live integration test so it exercises tmux with real backends.
C: Prefer unit tests with mocked tmux command execution and skip real tmux integration coverage in CI-style runs.
D: Treat tmux behavior as manual-test-only and keep automated tests at the current subprocess level.

6. How should UUIDv7 run IDs be implemented in this minimal Go module?
A: Keep the module dependency-light and implement a small local UUIDv7 generator using the standard library. (Recommended)
B: Add a focused third-party UUID package to generate UUIDv7 values.
C: Reuse the current Claude session UUIDv4 generator for run IDs even though the design says UUIDv7.
D: Use a timestamp plus random suffix instead of UUIDv7.

## 2026-04-26 Question Round 1 Answers

1. Which package should own the expanded run loop, tmux session lifecycle, prompt composition, and artifact persistence while keeping `main.go` focused on CLI parsing and wiring?
- Answer: custom
- Raw response: "I thinkg we should refactor the CliTool to own the tmux session lifecycle + everything to do with CLI parsing. So next time we don't want <promise> marker only for claude, we can do it. Then the rest can go in A."
- Interpretation: create a new feature package for the run loop and artifact logic, but keep tmux session behavior and CLI-tool-specific interaction strategy behind a refactored session-oriented interface in `orchestrator/internal/cli`.
- Follow-up needed: confirm who creates the single tmux session and its two panes: the run controller or the `internal/cli` session factory.

2. How should shared pane-local tmux mechanics be implemented across `codex` and `claude` while still keeping backend ownership in the adapters?
- Answer: custom
- Raw response: "A, the CliTool (or whatever we rename it to) owns all tmux semantics as an interface, and then all the implementation inthe respective structs."
- Interpretation: the interface should expose tmux-backed session behavior, and each backend struct should implement that behavior; any shared helper code is acceptable only as an implementation detail behind that interface.

3. Where should the per-turn `<promise>done</promise>` prompt suffix be appended?
- Answer: `B`
- Interpretation: prompt-completion instructions live inside each adapter so backends can diverge later if needed.

4. How explicit should runtime artifact persistence be as its own component?
- Answer: pending clarification
- Raw response: "explain this more"
- Follow-up needed: choose whether artifact writing has a dedicated owner component, stays inline in the run controller, or is split across adapters and the run controller.

5. How should tmux-backed integration coverage be made deterministic enough for `go test`?
- Answer: `D` (tentative)
- Raw response: "hmm maybe let's skip the tests, manual test only"
- Interpretation: prefer manual verification over automated tmux integration coverage for v1.
- Follow-up needed: confirm whether this should be locked in as an intentional deviation from the design spec's integration-led test requirement.

6. How should UUIDv7 run IDs be implemented in this minimal Go module?
- Answer: `B`
- Interpretation: add a focused third-party dependency to generate UUIDv7 run IDs.

## 2026-04-26 Question Round 2

7. If tmux/session behavior should live behind the refactored `internal/cli` interface, who should create the single tmux session and the two role panes for a run?
A: Add a session host/factory in `orchestrator/internal/cli` that creates the tmux session plus both panes, then returns pane-bound implementer/reviewer sessions to the run controller. (Recommended)
B: Let the new `implementwithreviewer` run controller create the tmux session and pane targets, then pass targets into backend sessions.
C: Have the implementer adapter create the tmux session and reviewer attach to what it created.
D: Let each backend session create and manage its own tmux session independently.

8. Here is what question 4 was getting at: artifact persistence can either be a dedicated component or just helper code inside the runner. Which ownership do you want?
A: Dedicated artifact writer component in `orchestrator/internal/implementwithreviewer`, with focused files for run paths, metadata/result JSON, state transition appends, and capture naming/writes. The runner calls it, but does not own file formats. (Recommended)
B: Keep artifact persistence inline in the main runner file with a few private helper functions; no separate component boundary.
C: Let backend sessions write capture files, and keep only metadata/result/state writing in the runner.
D: Reduce v1 artifact scope below the design doc and only write a minimal subset.

9. Where should the role-specific prompt templates live once tmux/session behavior moves behind the `internal/cli` interface?
A: Keep implementer/reviewer role prompt templates in `orchestrator/internal/implementwithreviewer`, and let each backend session append its own transport-level done-marker/startup instructions. (Recommended)
B: Move the full role prompt templates into each backend adapter so `codex` and `claude` each own complete prompt construction.
C: Keep the current prompt constants in `cmd/implement-with-reviewer/main.go` and pass the fully built strings through the new runner.
D: Put prompt templates in shared files under `orchestrator/internal/cli` so the runner only supplies raw task/review content.

10. How coarse should the new session interface be?
A: High-level session API: `Start(...)`, `RunTurn(...)`, `Close(...)`, where each backend session internally handles paste/wait/capture/reset and returns a structured turn result. (Recommended)
B: Low-level lifecycle API: `Start`, `Send`, `WaitForDone`, `Capture`, `Reset`, `Close`, with the runner coordinating each step.
C: Preserve a single-call `SendMessage`-style API and hide persistence by rebuilding a session internally on each call.
D: Use different interfaces for `codex` and `claude` and let the runner branch on backend type.

11. For v1 verification, should the implementation plan intentionally drop new automated coverage and rely on a manual test script?
A: Yes. Treat manual verification as the planned test strategy for this feature and record the design-doc deviation explicitly. (Recommended based on your note)
B: Add only lightweight unit tests around prompt composition, artifacts, and validation, but no tmux integration tests.
C: Add tmux integration tests with fake `codex`/`claude` binaries.
D: Keep the design-doc integration-led test requirement unchanged.

12. Should this feature update `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` as part of the main implementation?
A: Yes. The contract doc is part of the feature and should be updated alongside code to reflect runtime artifacts and tmux-backed behavior. (Recommended)
B: No. Ship code first and leave contract docs for a follow-up change.
C: Update only the design docs under `docs/development/`, not the command contract doc.
D: Add a new runtime-operations doc instead of touching `CONTRACT.md`.

## 2026-04-26 Question Round 2 Answers

7. If tmux/session behavior should live behind the refactored `internal/cli` interface, who should create the single tmux session and the two role panes for a run?
- Answer: `C`
- Raw response: "C, I think some code duplication is OK here, we may also want different behavior for different CLI wrapper"
- Interpretation: bias toward backend-wrapper ownership of session bootstrapping instead of a shared run-controller-owned tmux host.
- Follow-up needed: confirm whether `C` literally means the implementer backend becomes the special creator of shared tmux topology, or more generally that backend wrappers should own tmux topology creation behind `internal/cli` without the runner knowing how.

8. Here is what question 4 was getting at: artifact persistence can either be a dedicated component or just helper code inside the runner. Which ownership do you want?
- Answer: `A`
- Interpretation: create a dedicated artifact writer component in the new feature package; the runner coordinates lifecycle, but artifact formats and file writes have their own owner.

9. Where should the role-specific prompt templates live once tmux/session behavior moves behind the `internal/cli` interface?
- Answer: `A`
- Interpretation: keep role prompts in the new feature package, while backend sessions append backend-specific startup and done-marker instructions.

10. How coarse should the new session interface be?
- Answer: `A`
- Interpretation: use a high-level session API such as `Start`, `RunTurn`, and `Close`, with each backend session owning paste/wait/capture/reset internally.

11. For v1 verification, should the implementation plan intentionally drop new automated coverage and rely on a manual test script?
- Answer: `A`
- Interpretation: plan manual verification as the primary test strategy for this feature and record the deviation from the design document's integration-led testing requirement.

12. Should this feature update `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` as part of the main implementation?
- Answer: `A`
- Interpretation: update the command contract doc as part of the main feature.

## 2026-04-26 Question Round 3

13. Your `7C` answer is the only remaining ambiguous ownership boundary. Which concrete interpretation do you want?
A: Keep the runner unaware of tmux details, but add a backend-agnostic session host/factory in `orchestrator/internal/cli` that creates shared tmux topology and returns pane-bound backend sessions; wrappers still own pane behavior. This avoids making the implementer backend special. (Recommended)
B: Make the implementer backend session literally create the tmux session and both panes, then hand the reviewer pane target to the reviewer backend session.
C: Let each backend wrapper create whatever tmux topology it needs for its own role, even if that means moving away from the design's one-session-two-pane topology.
D: Keep the design's one-session-two-pane topology, but let the runner create it after all and only move pane-local behavior into the wrappers.

14. Where should the manual verification procedure live in the implementation plan?
A: As explicit verification steps inside the task list plus a dedicated manual verification task that exercises success, timeout, artifact writing, and cleanup. (Recommended)
B: Only as a short note in the header; the implementer can improvise the checks.
C: Add a new checked-in runbook file to the allowlist solely for manual test instructions.
D: Do not prescribe manual verification beyond “run it and inspect output”.

15. Since you want backend sessions to own transport-specific completion strategy, how should the done-marker contract be represented in the code?
A: Put a shared runner-level semantic contract in `implementwithreviewer` like “turns must come back complete”, but let each backend session inject and detect its own completion marker/instructions internally. (Recommended)
B: Hard-code `<promise>done</promise>` in both the runner and adapters for clarity, even if duplicated.
C: Remove all shared knowledge of completion from the runner and let it trust session return values blindly.
D: Put the done-marker constant only in `internal/cli`, and have the runner know nothing about completion semantics.

16. How should the third-party UUID dependency be introduced?
A: Add one focused UUID library to `orchestrator/go.mod`, use it only for run ID generation in the new feature package, and do not replace the existing Claude session ID helper unless needed later. (Recommended)
B: Add the UUID library and also migrate Claude session IDs to use it in the same feature for consistency.
C: Vendor or copy UUIDv7 code locally instead of using the module system.
D: Defer the dependency choice until implementation time and leave the plan non-committal.

## 2026-04-26 Question Round 3 Answers

13. Your `7C` answer is the only remaining ambiguous ownership boundary. Which concrete interpretation do you want?
- Answer: pending clarification
- Raw response: "Sorry what's a backend wrapper here? I want codex and claude cli to own the tmux lifecycle entirely, the runner has no idea about anything tmux."
- Interpretation: by "backend wrapper," this plan means the concrete adapter/session implementations in `orchestrator/internal/cli/codex.go` and `orchestrator/internal/cli/claude.go`.
- Follow-up needed: choose whether shared tmux topology is created by one `internal/cli` factory that the runner calls once, or by one specific backend implementation that becomes responsible for bootstrapping the shared session.

14. Where should the manual verification procedure live in the implementation plan?
- Answer: `A`
- Interpretation: include explicit manual verification steps in the task list and a dedicated verification task that covers success, timeout, artifact writing, and cleanup.

15. Since you want backend sessions to own transport-specific completion strategy, how should the done-marker contract be represented in the code?
- Answer: `B`
- Interpretation: hard-code `<promise>done</promise>` in both the runner-facing plan and the backend session implementations for v1 clarity, even though that duplicates knowledge.

16. How should the third-party UUID dependency be introduced?
- Answer: `A`
- Interpretation: add one focused UUID dependency for run IDs only; do not use this feature to refactor Claude session ID generation.

## 2026-04-26 Question Round 4

17. To make your tmux-ownership preference concrete while preserving the design's one-run/one-session/two-pane topology, which implementation shape do you want?
A: The runner calls one `internal/cli` factory such as `NewSessionPair(...)`; that factory creates the shared tmux session plus both panes, then returns codex/claude-backed sessions. The runner still knows nothing about tmux. (Recommended)
B: One concrete backend implementation, likely the implementer session, literally creates the shared tmux session and reviewer pane, then the second backend attaches to what the first created.

## 2026-04-26 Question Round 4 Answers

17. To make your tmux-ownership preference concrete while preserving the design's one-run/one-session/two-pane topology, which implementation shape do you want?
- Answer: custom
- Raw response: "no, just calls someting like codex.NewSession, which creates the tmux session + launches codex inside it. and then we just do session.Write or whatever"
- Interpretation: the runner should call backend-specific constructors such as `codex.NewSession(...)` / `claude.NewSession(...)`, and those concrete session implementations should own tmux lifecycle and backend launch details entirely; the runner should only interact with a session API afterward.
- Follow-up needed: confirm whether those constructors should hide a shared run-level tmux session keyed by run ID and role, or whether each backend session should get its own tmux session.

## 2026-04-26 Question Round 5

18. One last topology choice so I can lock file ownership: when the runner calls backend-specific constructors like `codex.NewSession(...)`, should they create or attach to what?
A: A shared run-level tmux session with one pane per role, hidden behind the constructors using run ID + role. This keeps the reviewed design's topology while the runner stays tmux-agnostic. (Recommended)
B: A separate tmux session per backend role, so implementer and reviewer do not share one tmux session in v1.

## 2026-04-26 Question Round 5 Answers

18. One last topology choice so I can lock file ownership: when the runner calls backend-specific constructors like `codex.NewSession(...)`, should they create or attach to what?
- Answer: `B`
- Raw response: "new tmux session per backend-specific constructor invocation"
- Interpretation: each backend-specific constructor creates and owns its own tmux session; the implementation plan should record this as an intentional deviation from the reviewed design's original one-run/one-session/two-pane topology.
