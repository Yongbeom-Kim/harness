# Thin Concrete Agent Interface Refactor Implementation Plan

**Goal:** Replace the shared `internal/agent/session` runtime stack with concrete tmux-backed `CodexAgent` and `ClaudeAgent` types, and move `implement-with-reviewer` orchestration back into its command package.

**Architecture:** Keep `internal/agent` concrete and small: Codex and Claude stay as separate types over a reusable `internal/agent/tmux` package plus narrow shared helpers for launch command assembly and typed runtime errors. Move `implement-with-reviewer` loop ownership, prompt decoration, done-marker polling, capture slicing, side-channel routing, session naming, and artifact writing into `orchestrator/cmd/implement-with-reviewer`, leaving only reusable infrastructure in `internal/filechannel`, `internal/dirlock`, and `internal/agent/tmux`. Stage the migration so `internal/agent/session` and `internal/reviewloop` remain temporarily buildable until the final cleanup task; each intermediate task must end in a compiling tree.

**Tech Stack:** Go 1.26, `tmux` CLI, Unix FIFOs via `syscall.Mkfifo`, `github.com/google/uuid`, existing harness command binaries and contract Markdown docs.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Remove the shared session/runtime abstraction as the production control surface and center runtime behavior in `internal/agent`. | `ConcreteAgentRuntime` | `CodexAgent`, `ClaudeAgent`, `AgentTmuxRuntime` | `orchestrator/internal/agent/agent.go`, create `orchestrator/internal/agent/errors.go`, `orchestrator/internal/agent/shell.go`, `orchestrator/internal/agent/standalone.go`, delete `orchestrator/internal/agent/session/dependencies.go`, `orchestrator/internal/agent/session/errors.go`, `orchestrator/internal/agent/session/launch.go`, `orchestrator/internal/agent/session/protocol.go`, `orchestrator/internal/agent/session/runtime.go`, `orchestrator/internal/agent/session/standalone.go`, `orchestrator/internal/agent/session/types.go`, `orchestrator/internal/agent/session/driver/protocol/protocol.go`, `orchestrator/internal/agent/session/launcher/launcher.go` | `KnownBackendNames()`, `ValidateBackend(name string)`, `AgentError{Kind,SessionName,Capture}`, `buildLaunchCommand(command, args...)` | `orchestrator/internal/agent/agent_test.go`, create `orchestrator/internal/agent/standalone_test.go`, `cd orchestrator && go test ./...` |
| R2 | Implement `CodexAgent` as a thin concrete type exposing `Start`, `WaitUntilReady`, `SendPrompt`, `Capture`, `SessionName`, and `Close`. | `CodexAgent` | `AgentTmuxRuntime` | `orchestrator/internal/agent/codex.go`, `orchestrator/internal/agent/agent_test.go` | `NewCodexAgent(sessionName string)`, `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture()`, `Close()` | `orchestrator/internal/agent/agent_test.go` |
| R3 | Implement `ClaudeAgent` as a separate concrete type with the same action-level surface and its own ready heuristics. | `ClaudeAgent` | `AgentTmuxRuntime` | `orchestrator/internal/agent/claude.go`, `orchestrator/internal/agent/agent_test.go` | `NewClaudeAgent(sessionName string)`, `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture()`, `Close()` | `orchestrator/internal/agent/agent_test.go` |
| R4 | Keep tmux interaction reusable in `internal/agent/tmux`, including creating new sessions, opening existing sessions for attach, pane send/capture, and typed tmux command errors. | `AgentTmuxRuntime` | `CodexAgent`, `ClaudeAgent`, `LauncherCommands` | create `orchestrator/internal/agent/tmux/errors.go`, `orchestrator/internal/agent/tmux/interfaces.go`, `orchestrator/internal/agent/tmux/tmux.go`, `orchestrator/internal/agent/tmux/tmux_test.go`, delete `orchestrator/internal/agent/session/tmux/errors.go`, `orchestrator/internal/agent/session/tmux/interfaces.go`, `orchestrator/internal/agent/session/tmux/tmux.go`, `orchestrator/internal/agent/session/tmux/tmux_test.go` | `NewTmuxSession(name string)`, `OpenTmuxSession(name string)`, `TmuxSession.Attach(...)`, `TmuxSession.NewPane()`, `TmuxPane.SendText`, `TmuxPane.Capture` | `orchestrator/internal/agent/tmux/tmux_test.go` |
| R5 | Move the full implementer/reviewer loop, backend branching, and runtime sequencing into `cmd/implement-with-reviewer`. | `ImplementWithReviewerCommand` | `CodexAgent`, `ClaudeAgent`, `ImplementWithReviewerArtifacts`, `ImplementWithReviewerTurnEngine`, `ImplementWithReviewerSideChannel` | `orchestrator/cmd/implement-with-reviewer/main.go`, create `orchestrator/cmd/implement-with-reviewer/run.go`, `orchestrator/cmd/implement-with-reviewer/run_test.go`, `orchestrator/cmd/implement-with-reviewer/types.go`, delete `orchestrator/internal/reviewloop/runner.go`, `orchestrator/internal/reviewloop/types.go`, `orchestrator/internal/reviewloop/runner_test.go` | `runnerConfig`, `runImplementWithReviewer(cfg, parsed)`, `newAgentForBackend(name, sessionName)` | `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/cmd/implement-with-reviewer/run_test.go` |
| R6 | Remove the startup prompt phase and prepend the role contract plus fixed FIFO side-channel instructions to every workflow turn prompt instead. | `ImplementWithReviewerPrompts` | `ImplementWithReviewerCommand`, `ImplementWithReviewerSideChannel` | create `orchestrator/cmd/implement-with-reviewer/prompts.go`, `orchestrator/cmd/implement-with-reviewer/prompts_test.go`, delete `orchestrator/internal/reviewloop/prompts.go` | `buildRoleContract(role string)`, `BuildImplementerPrompt(task string)`, `BuildReviewerPrompt(task, implementation string)`, `BuildRewritePrompt(task, implementation, review string)` | `orchestrator/cmd/implement-with-reviewer/prompts_test.go` |
| R7 | Move `<promise>done</promise>` decoration, marker generation, full-pane capture slicing, idle polling, and approval detection into the command package. | `ImplementWithReviewerTurnEngine` | `ImplementWithReviewerCommand`, `ImplementWithReviewerPrompts` | create `orchestrator/cmd/implement-with-reviewer/turns.go`, `orchestrator/cmd/implement-with-reviewer/turns_test.go` | `decorateTurnPrompt(roleContract, body, marker string)`, `waitForDone(...)`, `extractTurnCapture(...)`, `isApproved(review string)` | `orchestrator/cmd/implement-with-reviewer/turns_test.go` |
| R8 | Keep FIFO side-channel delivery, but make it command-owned and reuse `SendPrompt(...)` without turn markers or done decoration. Terminal file-channel failures must still fail the run. | `ImplementWithReviewerSideChannel` | `ImplementWithReviewerCommand`, `filechannel.ChannelManager` | create `orchestrator/cmd/implement-with-reviewer/sidechannel.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`, delete `orchestrator/internal/reviewloop/sidechannel.go`, `orchestrator/internal/reviewloop/sidechannel_test.go` | `newSideChannelCoordinator(...)`, `handleChannelMessage(...)`, `wrapSideChannelMessage(body string)`, `SendPrompt(text string)` | `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`, `orchestrator/internal/filechannel/fifo_test.go` |
| R9 | Make run metadata, transitions, captures, results, and session naming command-owned instead of `internal/reviewloop`-owned, while preserving the existing run artifact layout and JSON field names. | `ImplementWithReviewerArtifacts` | `ImplementWithReviewerCommand` | create `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`, `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`, `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`, modify `orchestrator/cmd/implement-with-reviewer/types.go`, delete `orchestrator/internal/reviewloop/artifact_paths.go`, `orchestrator/internal/reviewloop/artifact_writer.go`, `orchestrator/internal/reviewloop/session_namer.go` | `runsRootDir = "log/runs"`, `newArtifactWriter(runID string)`, `buildSessionName(runID, role string)`, `WriteMetadata(...)`, `AppendTransition(...)`, `AppendChannelEvent(...)`, `WriteCapture(...)`, `WriteResult(...)` | `orchestrator/cmd/implement-with-reviewer/artifacts_test.go` |
| R10 | Keep `tmux_codex` and `tmux_claude` as thin wrappers over the concrete backend types, preserve existing `--session` / `--attach` UX, remove `tmux_agent`, and update build/docs to match. | `LauncherCommands` | `BuildAndContracts`, `CodexAgent`, `ClaudeAgent`, `AgentTmuxRuntime` | `orchestrator/cmd/tmux_codex/main.go`, `orchestrator/cmd/tmux_claude/main.go`, delete `orchestrator/cmd/tmux_agent/main.go`, modify `Makefile`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, `orchestrator/internal/agent/standalone.go` | `parseStandaloneArgs(args []string, cfg standaloneConfig)`, `runStandalone(args []string, cfg standaloneConfig)`, `Start()`, `WaitUntilReady()`, `OpenTmuxSession(name).Attach(...)` | `orchestrator/internal/agent/standalone_test.go`, `cd orchestrator && go test ./cmd/... ./internal/agent/...` |
| R11 | Preserve `implement-with-reviewer` CLI surface and exit semantics (`--implementer`, `--reviewer`, `--max-iterations`, stdin task reading, backend validation, and current `0` / `1` / `2` exits) while moving orchestration into the command package. | `ImplementWithReviewerCommand` | `ConcreteAgentRuntime` | `orchestrator/cmd/implement-with-reviewer/main.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `parseArgs(args, stderr, getenv)`, `run(args, cfg)`, `readTask(stdin)`, exit code mapping in `runImplementWithReviewer(...)` | `orchestrator/cmd/implement-with-reviewer/main_test.go` |
| E1 | `Start()` must fail immediately when the requested tmux session already exists. | `CodexAgent` | `ClaudeAgent`, `AgentTmuxRuntime` | `orchestrator/internal/agent/codex.go`, `orchestrator/internal/agent/claude.go`, `orchestrator/internal/agent/tmux/tmux.go`, `orchestrator/internal/agent/agent_test.go`, `orchestrator/internal/agent/tmux/tmux_test.go` | `Start()`, `NewTmuxSession(name string)` | `orchestrator/internal/agent/agent_test.go`, `orchestrator/internal/agent/tmux/tmux_test.go` |
| E2 | Full pane capture contains prior turns and may include side-channel traffic; the command must isolate the active turn by marker so completion detection stays correct. | `ImplementWithReviewerTurnEngine` | `ImplementWithReviewerSideChannel` | `orchestrator/cmd/implement-with-reviewer/turns.go`, `orchestrator/cmd/implement-with-reviewer/turns_test.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go` | `extractTurnCapture(capture, marker string)`, marker-only main-turn slicing, undecorated side-channel `SendPrompt(text)` | `orchestrator/cmd/implement-with-reviewer/turns_test.go`, `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go` |
| E3 | Moving artifact ownership must not change the existing artifact filenames or JSON schema, so downstream readers and contract docs remain compatible. | `ImplementWithReviewerArtifacts` | `ImplementWithReviewerCommand` | `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`, `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`, `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`, `orchestrator/cmd/implement-with-reviewer/types.go` | `metadata.json`, `state-transitions.jsonl`, `channel-events.jsonl`, `result.json`, `successCaptureName(iteration, role)`, `failureCaptureName(iteration, role, suffix)`, existing JSON tags on run artifact structs | `orchestrator/cmd/implement-with-reviewer/artifacts_test.go` |

## Component Responsibility Map

- `ConcreteAgentRuntime`: primary owner for the `internal/agent` package surface after the refactor. It keeps backend validation, shared agent errors, shared launch-command assembly, and standalone launcher helpers. It does not own prompt decoration, done-marker parsing, or implementer/reviewer iteration control.
- `CodexAgent`: primary owner for Codex-specific tmux-backed runtime behavior, including launch, readiness detection, raw prompt send, full-pane capture, and session close. It does not own workflow turn semantics.
- `ClaudeAgent`: primary owner for Claude-specific tmux-backed runtime behavior with the same boundary as `CodexAgent`. It does not own command-level marker logic.
- `AgentTmuxRuntime`: primary owner for reusable tmux session/pane mechanics and tmux command error typing. It does not own backend launch commands or workflow rules.
- `ImplementWithReviewerCommand`: primary owner for the command entrypoint, backend branching, run-scoped session names, orchestration lifecycle, and preserving the current `implement-with-reviewer` CLI/exit behavior. It does not own raw tmux command execution.
- `ImplementWithReviewerPrompts`: primary owner for role contracts, fixed FIFO side-channel instructions, and workflow prompt body construction. It does not own transport or completion detection.
- `ImplementWithReviewerTurnEngine`: primary owner for marker generation, prompt decoration, full-pane capture slicing, done-marker polling, and approval detection. It does not own backend process launch.
- `ImplementWithReviewerSideChannel`: primary owner for FIFO routing, readiness gating, side-channel message wrapping, and terminal propagation of file-channel failures. It does not own FIFO implementation below `internal/filechannel`.
- `ImplementWithReviewerArtifacts`: primary owner for run metadata, transition logs, captures, results, session-name formatting, and preservation of the current on-disk run artifact layout/schema. It does not own tmux or backend selection.
- `LauncherCommands`: primary owner for `tmux_codex` and `tmux_claude` behavior, including optional attach flow and removal of `tmux_agent`. It does not own reusable tmux internals.
- `BuildAndContracts`: primary owner for build target updates and checked-in contract docs. It does not own runtime implementation.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `cmd/implement-with-reviewer/main.go` | `CodexAgent` / `ClaudeAgent` | `newCodexAgent(sessionName string)`, `newClaudeAgent(sessionName string)` | Backend branching stays in the command package; no shared production runtime interface comes back. |
| `ImplementWithReviewerCommand` | `CodexAgent` / `ClaudeAgent` | `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture() (string, error)`, `SessionName() string`, `Close() error` | `Start()` must fail when the target tmux session already exists. `SendPrompt()` is raw transport only. |
| `ImplementWithReviewerCommand` | `ImplementWithReviewerPrompts` | `buildRoleContract(role)`, `BuildImplementerPrompt(...)`, `BuildReviewerPrompt(...)`, `BuildRewritePrompt(...)` | Every main turn prompt includes the role contract plus the fixed FIFO paths (`./to_reviewer.pipe`, `./to_implementer.pipe`) and the current role's writable channel. There is no startup prompt stage. |
| `ImplementWithReviewerCommand` | `ImplementWithReviewerTurnEngine` | `executeTurn(...)`, `decorateTurnPrompt(...)`, `waitForDone(...)`, `extractTurnCapture(...)`, `isApproved(review string)` | Main-turn prompts get unique markers and done instructions. Side-channel prompts do not. |
| `ImplementWithReviewerCommand` | `ImplementWithReviewerArtifacts` | `newArtifactWriter(runID string)`, `buildSessionName(runID, role string)` | Command owns run metadata layout and per-role session names, but the artifact files and JSON fields must remain compatible with the current `log/runs/<run-id>` structure. |
| `ImplementWithReviewerSideChannel` | `filechannel.ChannelManager` | `Messages()`, `Errors()`, `Stop()`, `Remove()` | File-channel reader failures remain terminal and must reach the command loop. |
| `ImplementWithReviewerSideChannel` | `CodexAgent` / `ClaudeAgent` | `SendPrompt(wrapSideChannelMessage(body))` | Side-channel sends bypass marker and done decoration. |
| `CodexAgent` / `ClaudeAgent` | `AgentTmuxRuntime` | `tmux.NewTmuxSession(name)`, `TmuxSession.NewPane()`, `TmuxPane.SendText(text)`, `TmuxPane.Capture()`, `TmuxSession.Close()` | Agents own backend-specific launch/wait behavior but not raw tmux execution. |
| `LauncherCommands` | `CodexAgent` / `ClaudeAgent` | `Start()`, `WaitUntilReady()`, `SessionName()` | Standalone launch success should mean the backend is ready for input. |
| `LauncherCommands` | `AgentTmuxRuntime` | `tmux.OpenTmuxSession(name).Attach(stdin, stdout, stderr)` | Attach remains in the tmux layer so the concrete backend method set stays narrow. |

## File Ownership Map

- Modify `orchestrator/internal/agent/agent.go` - owned by `ConcreteAgentRuntime`; remove the shared `Agent` interface/session factory shape and keep only backend validation plus narrow shared helpers.
- Create `orchestrator/internal/agent/errors.go` - owned by `ConcreteAgentRuntime`; move the shared typed runtime error model out of `internal/agent/session/errors.go`.
- Create `orchestrator/internal/agent/shell.go` - owned by `ConcreteAgentRuntime`; replace `session/launcher` with a small sourced-shell launch helper.
- Create `orchestrator/internal/agent/standalone.go` - owned by `ConcreteAgentRuntime`; hold shared standalone launcher parsing/wiring used by `tmux_codex` and `tmux_claude` while preserving `--session` / `--attach` behavior.
- Modify `orchestrator/internal/agent/codex.go` - owned by `CodexAgent`; implement the new concrete lifecycle methods and Codex-ready heuristic.
- Modify `orchestrator/internal/agent/claude.go` - owned by `ClaudeAgent`; implement the new concrete lifecycle methods and Claude-ready heuristic.
- Modify `orchestrator/internal/agent/agent_test.go` - owned by `ConcreteAgentRuntime`; update tests around validation, agent errors, and concrete backend behavior.
- Create `orchestrator/internal/agent/standalone_test.go` - owned by `ConcreteAgentRuntime`; move launcher-flow tests out of the deleted `session/standalone_test.go`.
- Create `orchestrator/internal/agent/tmux/errors.go` - owned by `AgentTmuxRuntime`; move tmux command error types to the new package root.
- Create `orchestrator/internal/agent/tmux/interfaces.go` - owned by `AgentTmuxRuntime`; keep tmux session/pane contracts in the new package path.
- Create `orchestrator/internal/agent/tmux/tmux.go` - owned by `AgentTmuxRuntime`; keep tmux session creation, open-existing-session, pane send/capture, attach, and close behavior.
- Create `orchestrator/internal/agent/tmux/tmux_test.go` - owned by `AgentTmuxRuntime`; keep tmux helper tests at the new package path.
- Modify `orchestrator/cmd/implement-with-reviewer/main.go` - owned by `ImplementWithReviewerCommand`; keep flag parsing, locking, command-local test seams, and existing exit semantics while wiring the command-owned loop.
- Create `orchestrator/cmd/implement-with-reviewer/run.go` - owned by `ImplementWithReviewerCommand`; own the full implementer/reviewer lifecycle, backend construction, and terminal error handling.
- Create `orchestrator/cmd/implement-with-reviewer/run_test.go` - owned by `ImplementWithReviewerCommand`; cover command-owned loop behavior through injected concrete-agent constructor seams.
- Create `orchestrator/cmd/implement-with-reviewer/types.go` - owned by `ImplementWithReviewerArtifacts`; hold command-owned run metadata, transition, result, and channel-event structs.
- Create `orchestrator/cmd/implement-with-reviewer/prompts.go` - owned by `ImplementWithReviewerPrompts`; keep role contracts, fixed FIFO instructions, and prompt builders out of `main.go`.
- Create `orchestrator/cmd/implement-with-reviewer/prompts_test.go` - owned by `ImplementWithReviewerPrompts`; verify prompt shapes, fixed FIFO instructions, and removal of startup-prompt behavior.
- Create `orchestrator/cmd/implement-with-reviewer/turns.go` - owned by `ImplementWithReviewerTurnEngine`; implement marker generation, prompt decoration, capture slicing, and done polling.
- Create `orchestrator/cmd/implement-with-reviewer/turns_test.go` - owned by `ImplementWithReviewerTurnEngine`; cover marker slicing, approval detection, timeout behavior, and side-channel-safe turn isolation.
- Create `orchestrator/cmd/implement-with-reviewer/sidechannel.go` - owned by `ImplementWithReviewerSideChannel`; own routing, readiness gating, wrapper generation, and terminal propagation of file-channel failures.
- Create `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go` - owned by `ImplementWithReviewerSideChannel`; cover dropped-before-ready, delivered, and reader-error behavior.
- Create `orchestrator/cmd/implement-with-reviewer/artifact_paths.go` - owned by `ImplementWithReviewerArtifacts`; define run directory layout while preserving existing artifact filenames.
- Create `orchestrator/cmd/implement-with-reviewer/artifact_writer.go` - owned by `ImplementWithReviewerArtifacts`; implement metadata/transitions/channel-events/captures/result writing without changing JSON schema.
- Create `orchestrator/cmd/implement-with-reviewer/artifacts_test.go` - owned by `ImplementWithReviewerArtifacts`; verify legacy-compatible paths, capture filenames, and JSON structure.
- Modify `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `ImplementWithReviewerCommand`; keep CLI parsing, exit code, and command-local seam tests.
- Modify `orchestrator/cmd/tmux_codex/main.go` - owned by `LauncherCommands`; rewrite the launcher to use the concrete `CodexAgent` and optional tmux attach via `internal/agent/tmux`.
- Modify `orchestrator/cmd/tmux_claude/main.go` - owned by `LauncherCommands`; rewrite the launcher to use the concrete `ClaudeAgent` and optional tmux attach.
- Delete `orchestrator/cmd/tmux_agent/main.go` - owned by `LauncherCommands`; remove the generic launcher binary.
- Modify `Makefile` - owned by `BuildAndContracts`; stop building `tmux_agent` and keep remaining binaries aligned with the new command list.
- Modify `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `BuildAndContracts`; document the command-owned loop, no startup prompt, command-owned artifacts, and marker-based completion logic.
- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `BuildAndContracts`; document that standalone launcher success now means Codex started and became ready for input.
- Delete `orchestrator/internal/reviewloop/artifact_paths.go`, `artifact_writer.go`, `prompts.go`, `runner.go`, `runner_test.go`, `session_namer.go`, `sidechannel.go`, `sidechannel_test.go`, and `types.go` - owned by `ImplementWithReviewerCommand`; these responsibilities move into `cmd/implement-with-reviewer`.
- Delete `orchestrator/internal/agent/session/dependencies.go`, `errors.go`, `launch.go`, `protocol.go`, `runtime.go`, `runtime_test.go`, `standalone.go`, `standalone_test.go`, `types.go`, `driver/protocol/protocol.go`, `driver/protocol/protocol_test.go`, `launcher/launcher.go`, and `launcher/launcher_test.go` - owned by `ConcreteAgentRuntime`; these abstractions are no longer first-class after the refactor.
- Delete `orchestrator/internal/agent/session/tmux/errors.go`, `interfaces.go`, `tmux.go`, and `tmux_test.go` - owned by `AgentTmuxRuntime`; replaced by the new `internal/agent/tmux` package path.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/agent/agent.go`
- `orchestrator/internal/agent/errors.go`
- `orchestrator/internal/agent/shell.go`
- `orchestrator/internal/agent/standalone.go`
- `orchestrator/internal/agent/standalone_test.go`
- `orchestrator/internal/agent/codex.go`
- `orchestrator/internal/agent/claude.go`
- `orchestrator/internal/agent/agent_test.go`
- `orchestrator/internal/agent/tmux/errors.go`
- `orchestrator/internal/agent/tmux/interfaces.go`
- `orchestrator/internal/agent/tmux/tmux.go`
- `orchestrator/internal/agent/tmux/tmux_test.go`
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
- `orchestrator/cmd/tmux_codex/main.go`
- `orchestrator/cmd/tmux_claude/main.go`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `Makefile`
- delete `orchestrator/cmd/tmux_agent/main.go`
- delete `orchestrator/internal/reviewloop/artifact_paths.go`
- delete `orchestrator/internal/reviewloop/artifact_writer.go`
- delete `orchestrator/internal/reviewloop/prompts.go`
- delete `orchestrator/internal/reviewloop/runner.go`
- delete `orchestrator/internal/reviewloop/runner_test.go`
- delete `orchestrator/internal/reviewloop/session_namer.go`
- delete `orchestrator/internal/reviewloop/sidechannel.go`
- delete `orchestrator/internal/reviewloop/sidechannel_test.go`
- delete `orchestrator/internal/reviewloop/types.go`
- delete `orchestrator/internal/agent/session/dependencies.go`
- delete `orchestrator/internal/agent/session/errors.go`
- delete `orchestrator/internal/agent/session/launch.go`
- delete `orchestrator/internal/agent/session/protocol.go`
- delete `orchestrator/internal/agent/session/runtime.go`
- delete `orchestrator/internal/agent/session/runtime_test.go`
- delete `orchestrator/internal/agent/session/standalone.go`
- delete `orchestrator/internal/agent/session/standalone_test.go`
- delete `orchestrator/internal/agent/session/types.go`
- delete `orchestrator/internal/agent/session/driver/protocol/protocol.go`
- delete `orchestrator/internal/agent/session/driver/protocol/protocol_test.go`
- delete `orchestrator/internal/agent/session/launcher/launcher.go`
- delete `orchestrator/internal/agent/session/launcher/launcher_test.go`
- delete `orchestrator/internal/agent/session/tmux/errors.go`
- delete `orchestrator/internal/agent/session/tmux/interfaces.go`
- delete `orchestrator/internal/agent/session/tmux/tmux.go`
- delete `orchestrator/internal/agent/session/tmux/tmux_test.go`

**Incidental-only files:**
- `orchestrator/go.mod` - import cleanup only if package moves require it.
- `orchestrator/go.sum` - dependency churn only if `go test` rewrites it.

## Task List
Implementation sequencing rule: Tasks 1-4 keep `internal/agent/session` and `internal/reviewloop` compiling as temporary compatibility shims. Do not delete those packages until Task 5, after all callers and tests have been moved. Every task must end in a buildable tree.

### Task 1: Add the New `internal/agent` Scaffolding Without Deleting Legacy Paths Yet

**Files:**
- Create: `orchestrator/internal/agent/errors.go`
- Create: `orchestrator/internal/agent/shell.go`
- Create: `orchestrator/internal/agent/standalone.go`
- Create: `orchestrator/internal/agent/standalone_test.go`
- Modify: `orchestrator/internal/agent/agent.go`
- Modify: `orchestrator/internal/agent/agent_test.go`
- Test: `orchestrator/internal/agent/agent_test.go`, `orchestrator/internal/agent/standalone_test.go`

**Covers:** `R1`, `R10`
**Owner:** `ConcreteAgentRuntime`
**Why:** Establish the new agent-owned error and standalone-launch helpers first, while leaving `internal/agent/session` available as a temporary compatibility shim so later migration tasks can land incrementally.

- [ ] **Step 1: Write the failing tests**

```go
func TestValidateBackendRejectsUnknownBackend(t *testing.T) {}
func TestParseStandaloneArgsPreservesSessionAndAttachFlags(t *testing.T) {}
```

- [ ] **Step 2: Run the focused tests**

Run: `cd orchestrator && go test ./internal/agent -run 'TestValidateBackend|TestParseStandaloneArgs'`
Expected: FAIL because the new agent-owned error and standalone helpers do not exist yet.

- [ ] **Step 3: Write the minimal implementation**

```go
type AgentError struct {
    Kind        string
    SessionName string
    Capture     string
    Err         error
}
```

- [ ] **Step 4: Re-run the package tests**

Run: `cd orchestrator && go test ./internal/agent`
Expected: PASS with the new `internal/agent` scaffolding in place while the legacy `session/*` package still compiles.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agent
git commit -m "refactor: add agent runtime scaffolding"
```

### Task 2: Add `internal/agent/tmux` and Concrete Agent Lifecycle Methods

**Files:**
- Create: `orchestrator/internal/agent/tmux/errors.go`
- Create: `orchestrator/internal/agent/tmux/interfaces.go`
- Create: `orchestrator/internal/agent/tmux/tmux.go`
- Create: `orchestrator/internal/agent/tmux/tmux_test.go`
- Modify: `orchestrator/internal/agent/codex.go`
- Modify: `orchestrator/internal/agent/claude.go`
- Modify: `orchestrator/internal/agent/agent.go`
- Modify: `orchestrator/internal/agent/agent_test.go`
- Modify: `orchestrator/internal/agent/standalone.go`
- Test: `orchestrator/internal/agent/tmux/tmux_test.go`, `orchestrator/internal/agent/agent_test.go`

**Covers:** `R2`, `R3`, `R4`, `E1`
**Owner:** `CodexAgent`, `ClaudeAgent`, `AgentTmuxRuntime`
**Why:** Introduce the real concrete runtime surface and reusable tmux mechanics while retaining any temporary adapters needed so `implement-with-reviewer` and launcher callers can be migrated in later tasks without breaking the build.

- [ ] **Step 1: Write the failing tests**

```go
func TestCodexAgentStartWaitSendCaptureClose(t *testing.T) {}
func TestClaudeAgentStartWaitSendCaptureClose(t *testing.T) {}
func TestOpenTmuxSessionReturnsAttachableHandle(t *testing.T) {}
```

- [ ] **Step 2: Run the focused tests**

Run: `cd orchestrator && go test ./internal/agent ./internal/agent/tmux`
Expected: FAIL because the concrete agents do not yet expose the new action-level methods and `internal/agent/tmux` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
type CodexAgent struct {
    sessionName string
    session     tmux.TmuxSessionLike
    pane        tmux.TmuxPaneLike
}
```

- [ ] **Step 4: Re-run the focused tests**

Run: `cd orchestrator && go test ./internal/agent ./internal/agent/tmux`
Expected: PASS with concrete `Start`, `WaitUntilReady`, `SendPrompt`, `Capture`, and `Close` methods plus the new tmux package, while temporary compatibility adapters still compile.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agent
git commit -m "refactor: add concrete tmux-backed agents"
```

### Task 3: Move `implement-with-reviewer` Loop and Prompt Ownership Into the Command Package

**Files:**
- Modify: `orchestrator/cmd/implement-with-reviewer/main.go`
- Modify: `orchestrator/cmd/implement-with-reviewer/main_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/run.go`
- Create: `orchestrator/cmd/implement-with-reviewer/run_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/types.go`
- Create: `orchestrator/cmd/implement-with-reviewer/prompts.go`
- Create: `orchestrator/cmd/implement-with-reviewer/prompts_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/turns.go`
- Create: `orchestrator/cmd/implement-with-reviewer/turns_test.go`
- Test: `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/cmd/implement-with-reviewer/run_test.go`, `orchestrator/cmd/implement-with-reviewer/prompts_test.go`, `orchestrator/cmd/implement-with-reviewer/turns_test.go`

**Covers:** `R5`, `R6`, `R7`, `R11`
**Owner:** `ImplementWithReviewerCommand`, `ImplementWithReviewerPrompts`, `ImplementWithReviewerTurnEngine`
**Why:** Move the orchestration loop into the command package, preserve the current CLI/exit behavior, and shift the startup-only FIFO instructions into every main-turn prompt so side-channel capability survives the no-startup-prompt design.

- [ ] **Step 1: Write the failing command tests**

```go
func TestRunUsesConcreteBackendFactories(t *testing.T) {}
func TestBuildImplementerPromptIncludesFixedFifoPaths(t *testing.T) {}
func TestExecuteTurnSlicesCaptureFromMarker(t *testing.T) {}
func TestRunReturnsExitCodeTwoForUnknownBackend(t *testing.T) {}
```

- [ ] **Step 2: Run the focused command tests**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer`
Expected: FAIL because `main.go` still delegates to `internal/reviewloop`, the command-local turn engine does not exist, and prompt builders do not yet inject the fixed FIFO instructions.

- [ ] **Step 3: Write the minimal implementation**

```go
func buildRoleContract(role string) string {
    return baseRoleContract(role) + "\n\n" + buildSideChannelInstructions(role)
}
```

- [ ] **Step 4: Re-run the command tests**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer`
Expected: PASS with the command package owning backend branching, CLI semantics, prompt construction, marker slicing, and done polling while `internal/reviewloop` still compiles as an unused compatibility package.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer
git commit -m "refactor: move reviewer loop into command package"
```

### Task 4: Move Side-Channel and Artifacts Into the Command Package While Preserving Format

**Files:**
- Create: `orchestrator/cmd/implement-with-reviewer/sidechannel.go`
- Create: `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/artifact_paths.go`
- Create: `orchestrator/cmd/implement-with-reviewer/artifact_writer.go`
- Create: `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`
- Modify: `orchestrator/cmd/implement-with-reviewer/run.go`
- Modify: `orchestrator/cmd/implement-with-reviewer/types.go`
- Test: `orchestrator/cmd/implement-with-reviewer/sidechannel_test.go`, `orchestrator/cmd/implement-with-reviewer/artifacts_test.go`, `orchestrator/internal/filechannel/fifo_test.go`

**Covers:** `R8`, `R9`, `E2`, `E3`
**Owner:** `ImplementWithReviewerSideChannel`, `ImplementWithReviewerArtifacts`
**Why:** Side-channel routing and run artifacts are command-specific workflow behavior, but the move must preserve the current file names and JSON schema so downstream readers do not break.

- [ ] **Step 1: Write the failing tests**

```go
func TestHandleChannelMessageUsesUndecoratedSendPrompt(t *testing.T) {}
func TestArtifactWriterPreservesLegacyRunLayout(t *testing.T) {}
```

- [ ] **Step 2: Run the focused tests**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer ./internal/filechannel`
Expected: FAIL because the command-owned side-channel and artifact writers do not exist yet and the legacy-compatible artifact expectations are not implemented.

- [ ] **Step 3: Write the minimal implementation**

```go
func wrapSideChannelMessage(body string) string {
    if !strings.HasSuffix(body, "\n") {
        body += "\n"
    }
    return "<side_channel_message>\n" + body + "</side_channel_message>\n"
}
```

- [ ] **Step 4: Re-run the focused tests**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer ./internal/filechannel`
Expected: PASS with command-owned side-channel routing, legacy-compatible artifact writing, and terminal propagation of file-channel failures while `internal/reviewloop` still compiles pending final cleanup.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer orchestrator/internal/filechannel
git commit -m "refactor: localize reviewer artifacts and side-channel"
```

### Task 5: Remove Legacy Runtime Packages and Align Launchers, Build Targets, and Contracts

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/main.go`
- Modify: `orchestrator/cmd/tmux_claude/main.go`
- Delete: `orchestrator/cmd/tmux_agent/main.go`
- Modify: `Makefile`
- Modify: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Delete: `orchestrator/internal/reviewloop/artifact_paths.go`
- Delete: `orchestrator/internal/reviewloop/artifact_writer.go`
- Delete: `orchestrator/internal/reviewloop/prompts.go`
- Delete: `orchestrator/internal/reviewloop/runner.go`
- Delete: `orchestrator/internal/reviewloop/runner_test.go`
- Delete: `orchestrator/internal/reviewloop/session_namer.go`
- Delete: `orchestrator/internal/reviewloop/sidechannel.go`
- Delete: `orchestrator/internal/reviewloop/sidechannel_test.go`
- Delete: `orchestrator/internal/reviewloop/types.go`
- Delete: `orchestrator/internal/agent/session/dependencies.go`
- Delete: `orchestrator/internal/agent/session/errors.go`
- Delete: `orchestrator/internal/agent/session/launch.go`
- Delete: `orchestrator/internal/agent/session/protocol.go`
- Delete: `orchestrator/internal/agent/session/runtime.go`
- Delete: `orchestrator/internal/agent/session/runtime_test.go`
- Delete: `orchestrator/internal/agent/session/standalone.go`
- Delete: `orchestrator/internal/agent/session/standalone_test.go`
- Delete: `orchestrator/internal/agent/session/types.go`
- Delete: `orchestrator/internal/agent/session/driver/protocol/protocol.go`
- Delete: `orchestrator/internal/agent/session/driver/protocol/protocol_test.go`
- Delete: `orchestrator/internal/agent/session/launcher/launcher.go`
- Delete: `orchestrator/internal/agent/session/launcher/launcher_test.go`
- Delete: `orchestrator/internal/agent/session/tmux/errors.go`
- Delete: `orchestrator/internal/agent/session/tmux/interfaces.go`
- Delete: `orchestrator/internal/agent/session/tmux/tmux.go`
- Delete: `orchestrator/internal/agent/session/tmux/tmux_test.go`
- Test: `orchestrator/internal/agent/standalone_test.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, full `go test` and `make build`

**Covers:** `R1`, `R4`, `R5`, `R8`, `R9`, `R10`
**Owner:** `LauncherCommands`, `BuildAndContracts`, `ConcreteAgentRuntime`
**Why:** After all callers have moved, remove the old runtime/reviewloop packages, switch the launcher binaries to the concrete agents, and align the build/docs with the final architecture.

- [ ] **Step 1: Write the failing cleanup/build checks**

```go
func TestStandaloneLaunchWaitsUntilReady(t *testing.T) {}
```

- [ ] **Step 2: Run the full verification before cleanup**

Run: `cd orchestrator && go test ./... && make build`
Expected: FAIL because launcher commands and build targets still point at legacy session helpers and the obsolete packages are still present.

- [ ] **Step 3: Write the minimal implementation**

```go
if err := agent.Start(); err != nil { ... }
if err := agent.WaitUntilReady(); err != nil { ... }
```

- [ ] **Step 4: Re-run the full verification**

Run: `cd orchestrator && go test ./... && make build`
Expected: PASS with `tmux_codex` / `tmux_claude` using the concrete agents, `tmux_agent` removed, contracts updated, and the legacy `session/*` and `reviewloop/*` trees deleted.

- [ ] **Step 5: Commit**

```bash
git add Makefile orchestrator/cmd orchestrator/internal/agent
git rm -r orchestrator/cmd/tmux_agent orchestrator/internal/reviewloop orchestrator/internal/agent/session
git commit -m "refactor: remove legacy runtime and align launchers"
```
