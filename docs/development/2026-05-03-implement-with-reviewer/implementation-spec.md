# Implement-With-Reviewer Implementation Plan

**Goal:** Add `implement-with-reviewer` as a shared-tmux bootstrap command and refactor the reusable runtime layer so commands own tmux-session lifecycle while runtimes own one pane/backend plus attach-scoped mkpipe lifecycle.

**Architecture:** Move the current `orchestrator/internal/session` package family to `orchestrator/internal/agentruntime`, rename the top-level reusable type to `Runtime`, and strip it down to pane/backend responsibilities only: launch, readiness wait, prompt send, capture, runtime-owned attach-scoped mkpipe start/stop, and pane-only close. Command packages create tmux sessions, acquire/release the current-directory lock from `orchestrator/internal/dirlock`, attach to tmux directly, and for `implement-with-reviewer` bootstrap two runtimes in one shared tmux session before seeding the inter-agent protocol with the absolute peer mkpipe paths returned by those runtimes.

**Tech Stack:** Go 1.26, standard library `flag` / `fmt` / `io` / `os` / `time` / `sync`, tmux CLI, Unix FIFOs, existing harness launcher tests, repo `Makefile`, command contract docs.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Ship `implement-with-reviewer` with the required CLI shape: `--implementer` / `-i`, `--reviewer` / `-r`, and exactly one positional prompt; no stdin task input. | `ImplementWithReviewerCLI` | None | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go` | `run(args, stdin, stdout, stderr, deps)`<br>`parseArgs(args, stderr)`<br>`parseBackend(name)` | `orchestrator/cmd/implement-with-reviewer/main_test.go` parser, help, and constructor-selection tests. |
| E1 | Invalid workflow usage exits `2`: missing `-i`/`-r`, unsupported backend, missing prompt, extra prompt, or whitespace-only prompt after shell parsing; `-h` exits `0`. | `ImplementWithReviewerCLI` | None | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go` | `parseArgs(args, stderr)` | `orchestrator/cmd/implement-with-reviewer/main_test.go` validation table covering all usage errors and `-h`. |
| R2 | `implement-with-reviewer` creates one shared tmux session, uses the default pane for the implementer, allocates one second pane for the reviewer, starts both runtimes, prints one pre-attach status line, and auto-attaches. | `ImplementWithReviewerCLI` | `TmuxPaneLifecycle`<br>`AgentRuntimeLifecycle` | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go`<br>`orchestrator/internal/agentruntime/tmux/interfaces.go` | `tmux.NewTmuxSession(name)`<br>`TmuxSessionLike.NewPane()`<br>`TmuxSessionLike.Attach(stdin, stdout, stderr)` | `orchestrator/cmd/implement-with-reviewer/main_test.go` bootstrap-order and pre-attach output tests. |
| E2 | Any workflow bootstrap failure before attach returns exit `1` and does best-effort cleanup in the right order: stop any started runtime mkpipes, close the tmux session, release the lock, and do not claim success. | `ImplementWithReviewerCLI` | `RuntimeMkpipeLifecycle`<br>`DirLockSupport`<br>`TmuxPaneLifecycle` | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go` | `cleanupBootstrapFailure(...)`<br>`Runtime.StopMkpipe()`<br>`TmuxSessionLike.Close()`<br>`Lock.Release()` | `orchestrator/cmd/implement-with-reviewer/main_test.go` partial-start cleanup tests for runtime start failure, mkpipe start failure, and prompt-send failure. |
| R3 | Seeded implementer and reviewer prompts must include the original task, explicit role, shared-session context, exact literal markers, the resolved absolute peer mkpipe path, and the protocol rules for handoff, approval, changes requested, blocked, peer-first clarification, and idle-on-approval behavior. | `ImplementWithReviewerProtocol` | `ImplementWithReviewerCLI` | `orchestrator/cmd/implement-with-reviewer/protocol.go`<br>`orchestrator/cmd/implement-with-reviewer/protocol_test.go` | `buildImplementerPrompt(task, reviewerPipePath, sessionName)`<br>`buildReviewerPrompt(task, implementerPipePath, sessionName)`<br>`const markerImplementationReady` | `orchestrator/cmd/implement-with-reviewer/protocol_test.go` marker literal, prompt-content, and marker-semantics tests. |
| E3 | Neither initial prompt may be sent until both runtimes are ready, both runtime-owned mkpipe listeners are started, and both absolute peer paths are known; the reviewer prompt must tell the reviewer to wait for implementer handoff. | `ImplementWithReviewerCLI` | `ImplementWithReviewerProtocol`<br>`RuntimeMkpipeLifecycle` | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go`<br>`orchestrator/cmd/implement-with-reviewer/protocol.go`<br>`orchestrator/cmd/implement-with-reviewer/protocol_test.go` | `Runtime.Start()`<br>`Runtime.StartMkpipe() (string, error)`<br>`Runtime.SendPrompt(prompt)`<br>`buildReviewerPrompt(task, implementerPipePath, sessionName)` | `orchestrator/cmd/implement-with-reviewer/main_test.go` sequencing test asserting both runtimes start, both runtime mkpipes start, and only then either prompt send occurs plus `orchestrator/cmd/implement-with-reviewer/protocol_test.go` reviewer wait-instruction coverage. |
| R4 | Rename the reusable runtime package family from `orchestrator/internal/session` to `orchestrator/internal/agentruntime`, while moving the current-directory lock package to `orchestrator/internal/dirlock` because commands now own that concern. | `AgentRuntimeLifecycle` | `BackendAdapters`<br>`RuntimeMkpipeLifecycle`<br>`TmuxPaneLifecycle`<br>`DirLockSupport` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/errors.go`<br>`orchestrator/internal/agentruntime/backend/backend.go`<br>`orchestrator/internal/agentruntime/env/env.go`<br>`orchestrator/internal/agentruntime/mkpipe/listener.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/dirlock/lock.go` | package imports under `internal/agentruntime/...` and `internal/dirlock` | `go test ./internal/agentruntime/... ./internal/dirlock -v` plus final `rg -n 'internal/session' orchestrator` cleanup check. |
| R5 | The top-level reusable type becomes `Runtime`, not `Session`, and it owns only one pane/backend lifecycle: `Start`, `SendPrompt`, `Capture`, `StartMkpipe`, `MkpipeErrors`, `StopMkpipe`, and `Close`; it does not create tmux sessions, acquire locks, or attach to tmux. | `AgentRuntimeLifecycle` | `BackendAdapters`<br>`RuntimeMkpipeLifecycle` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/errors.go`<br>`orchestrator/internal/agentruntime/runtime_test.go` | `NewCodex(session, pane, config) *Runtime`<br>`NewClaude(...)`<br>`NewCursor(...)`<br>`(*Runtime).Start()`<br>`(*Runtime).StartMkpipe()`<br>`(*Runtime).MkpipeErrors()`<br>`(*Runtime).StopMkpipe()`<br>`(*Runtime).Capture()`<br>`(*Runtime).Close()` | `orchestrator/internal/agentruntime/runtime_test.go` start/readiness/send/capture/state tests plus mkpipe state-rule and async-error-exposure tests. |
| R6 | Runtime `Close()` must close only its own pane/backend; add pane-level close support in the tmux layer and keep whole-session close as a command-owned operation. | `TmuxPaneLifecycle` | `AgentRuntimeLifecycle` | `orchestrator/internal/agentruntime/tmux/interfaces.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go`<br>`orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go` | `TmuxPaneLike.Close() error`<br>`(*TmuxPane).Close()`<br>`(*Runtime).Close()` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` kill-pane test and `orchestrator/internal/agentruntime/runtime_test.go` assertion that runtime close never calls session close. |
| R7 | Each runtime owns its own attach-scoped mkpipe lifecycle by composing the reusable listener primitive in `orchestrator/internal/agentruntime/mkpipe`; the reusable runtime exposes explicit `StartMkpipe` / `MkpipeErrors` / `StopMkpipe` APIs. | `RuntimeMkpipeLifecycle` | `AgentRuntimeLifecycle`<br>`SingleAgentLauncherCLI`<br>`ImplementWithReviewerCLI` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/internal/agentruntime/mkpipe/listener.go`<br>`orchestrator/internal/agentruntime/mkpipe/listener_test.go` | `Runtime.StartMkpipe() (string, error)`<br>`Runtime.MkpipeErrors() <-chan error`<br>`Runtime.StopMkpipe() error`<br>`mkpipe.ResolvePath(Config)`<br>`mkpipe.Start(Config)`<br>`Runtime.SendPrompt(prompt)` | `orchestrator/internal/agentruntime/runtime_test.go` start/stop/idempotency/forwarding/async-error tests plus `orchestrator/internal/agentruntime/mkpipe/listener_test.go` listener lifecycle tests. |
| E4 | The default mkpipe path for shared-session workflow runtimes must be role-specific: `.<sanitized-shared-session-name>-implementer.mkpipe` and `.<sanitized-shared-session-name>-reviewer.mkpipe`; prompts must receive absolute peer paths. | `RuntimeMkpipeLifecycle` | `ImplementWithReviewerCLI`<br>`ImplementWithReviewerProtocol` | `orchestrator/internal/agentruntime/mkpipe/listener.go`<br>`orchestrator/internal/agentruntime/mkpipe/listener_test.go`<br>`orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/cmd/implement-with-reviewer/protocol.go` | `mkpipe.Config{BasenameOverride, SessionName, RequestedPath}`<br>`mkpipe.ResolvePath(cfg)`<br>`Runtime.StartMkpipe()`<br>`buildImplementerPrompt(...)`<br>`buildReviewerPrompt(...)` | `orchestrator/internal/agentruntime/mkpipe/listener_test.go` basename-override resolution tests and `orchestrator/cmd/implement-with-reviewer/main_test.go` absolute-peer-path prompt tests. |
| E5 | When attach returns or the launcher process exits, runtime-owned mkpipe listeners stop, lock is released, and the tmux session remains alive; listener delivery failures while attached are logged and dropped rather than killing the session. | `RuntimeMkpipeLifecycle` | `SingleAgentLauncherCLI`<br>`ImplementWithReviewerCLI` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go`<br>`orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `Runtime.MkpipeErrors()`<br>`Runtime.StopMkpipe()`<br>`TmuxSessionLike.Attach(...)` cleanup path | Runtime tests for idempotent stop and async error exposure plus command suites for pre-attach failure handling, post-attach cleanup/error logging, and manual smoke for detach behavior. |
| R8 | `tmux_codex`, `tmux_claude`, and `tmux_cursor` must preserve their current CLI contracts, output wording, exit codes, and attach-only mkpipe semantics while switching to command-owned tmux session/bootstrap and `agentruntime.Runtime`. | `SingleAgentLauncherCLI` | `AgentRuntimeLifecycle`<br>`DirLockSupport`<br>`TmuxPaneLifecycle`<br>`RuntimeMkpipeLifecycle` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `run(...)`<br>`parseArgs(...)`<br>`dirlock.NewInCurrentDirectory()`<br>`tmux.NewTmuxSession(name)`<br>`Runtime.Start()`<br>`Runtime.StartMkpipe()` | Existing command suites expanded to cover lock/session/runtime sequencing, unchanged banners, and attach-scoped forwarding. |
| E6 | Single-agent `--mkpipe` remains attach-only: the command starts runtime mkpipe after runtime readiness, prints the absolute FIFO path in the same pre-attach line as today, stops the runtime mkpipe after attach returns, and rejects `--mkpipe` without `--attach`. | `SingleAgentLauncherCLI` | `RuntimeMkpipeLifecycle` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `Runtime.StartMkpipe()`<br>`Runtime.StopMkpipe()`<br>`extractMkpipeArgs(args)` | Existing command test suites plus new attach-cleanup assertions. |
| E7 | `implement-with-reviewer` session names come from a dedicated helper that produces a tmux-safe unique value with the fixed `implement-with-reviewer-` prefix; tests assert the prefix, non-empty unique suffix, and sanitization rather than an exact timestamp string. | `ImplementWithReviewerProtocol` | `ImplementWithReviewerCLI` | `orchestrator/cmd/implement-with-reviewer/protocol.go`<br>`orchestrator/cmd/implement-with-reviewer/protocol_test.go`<br>`orchestrator/cmd/implement-with-reviewer/main.go` | `generateSessionName(now time.Time)` | `orchestrator/cmd/implement-with-reviewer/protocol_test.go` helper tests for prefix, suffix, and tmux-safe output. |
| R9 | Move the current-directory lock implementation to `orchestrator/internal/dirlock` and make all commands acquire/release it directly around tmux bootstrap and attach. | `DirLockSupport` | `SingleAgentLauncherCLI`<br>`ImplementWithReviewerCLI` | `orchestrator/internal/dirlock/lock.go`<br>`orchestrator/internal/dirlock/lock_test.go`<br>`orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main.go` | `dirlock.NewInCurrentDirectory()`<br>`Lock.Acquire()`<br>`Lock.Release()` | `orchestrator/internal/dirlock/lock_test.go` plus command tests that assert release on success and failure paths. |
| R10 | Update docs and build surface so the supported binaries are exactly `tmux_codex`, `tmux_claude`, `tmux_cursor`, and `implement-with-reviewer`; `make build` must produce `bin/implement-with-reviewer`. | `ContractDocsAndBuildSurface` | `ImplementWithReviewerCLI`<br>`SingleAgentLauncherCLI` | `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md`<br>`orchestrator/cmd/tmux_cursor/CONTRACT.md`<br>`Makefile` | Contract doc `## Product Surface` and `## Invocation` sections<br>`build:` target | Manual doc verification with `rg` and `make build`, then `test -x bin/implement-with-reviewer`. |
| R11 | Remove the superseded `orchestrator/internal/session` tree after all callers switch to `internal/agentruntime`; no code or tests should still import `internal/session`. | `LegacySessionCleanup` | `AgentRuntimeLifecycle`<br>`SingleAgentLauncherCLI` | delete `orchestrator/internal/session/errors.go`<br>delete `orchestrator/internal/session/session.go`<br>delete `orchestrator/internal/session/session_test.go`<br>delete `orchestrator/internal/session/backend/backend.go`<br>delete `orchestrator/internal/session/backend/backend_test.go`<br>delete `orchestrator/internal/session/backend/codex.go`<br>delete `orchestrator/internal/session/backend/claude.go`<br>delete `orchestrator/internal/session/backend/cursor.go`<br>delete `orchestrator/internal/session/env/env.go`<br>delete `orchestrator/internal/session/env/env_test.go`<br>delete `orchestrator/internal/session/mkpipe/listener.go`<br>delete `orchestrator/internal/session/mkpipe/listener_test.go`<br>delete `orchestrator/internal/session/tmux/errors.go`<br>delete `orchestrator/internal/session/tmux/interfaces.go`<br>delete `orchestrator/internal/session/tmux/tmux.go`<br>delete `orchestrator/internal/session/tmux/tmux_test.go`<br>delete `orchestrator/internal/session/dirlock/lock.go`<br>delete `orchestrator/internal/session/dirlock/lock_test.go` | `rg -n 'internal/session' orchestrator` | Final `rg` check returns no hits and `cd orchestrator && go test ./...` passes. |

## Component Responsibility Map

- `ImplementWithReviewerCLI`: primary owner for `R1`, `E1`, `R2`, `E2`, and `E3`. It parses CLI flags, acquires/releases the current-directory lock, creates the shared tmux session, allocates the two panes, constructs the two runtimes, starts both runtimes, starts both runtime-owned mkpipes, prints the pre-attach status line, attaches to tmux, and performs bootstrap cleanup. It does not own marker text, prompt wording, or backend readiness rules.
- `ImplementWithReviewerProtocol`: primary owner for `R3` and `E7`. It defines the exact marker literals, the dedicated workflow session-name helper, and the seeded implementer/reviewer prompts, including peer absolute mkpipe paths, the reviewer wait-for-handoff rule, approval-idle behavior, and peer-first blocked/clarification rules. It does not own runtime startup or listener sequencing.
- `AgentRuntimeLifecycle`: primary owner for `R4` and `R5`. It is the reusable pane-scoped runtime abstraction: explicit backend constructors, start/readiness state, prompt sending, capture, runtime-owned mkpipe start/stop, pane-only close, and typed runtime errors. It does not own tmux session creation, tmux attach, or directory locking.
- `TmuxPaneLifecycle`: primary owner for `R6`. It owns tmux session/pane adapters and the new pane-close capability used by runtime `Close()`. It does not own readiness logic, mkpipe, or CLI validation.
- `RuntimeMkpipeLifecycle`: primary owner for `R7`, `E4`, and `E5`. It owns the reusable FIFO listener primitive plus runtime-layer start/stop, forwarding, error logging, and cleanup semantics for attach-scoped mkpipe delivery. It does not own workflow prompt text or backend readiness rules.
- `SingleAgentLauncherCLI`: primary owner for `R8` and `E6`. It preserves the operator-facing contracts of `tmux_codex`, `tmux_claude`, and `tmux_cursor` while moving session creation and lock ownership into command code and invoking runtime-owned mkpipe lifecycle only for attach-mode runs. It does not own backend differences or tmux adapter internals.
- `DirLockSupport`: primary owner for `R9`. It provides the current-directory lock implementation at `orchestrator/internal/dirlock` and nothing more. It does not own command sequencing.
- `ContractDocsAndBuildSurface`: primary owner for `R10`. It keeps the contract docs truthful and extends `make build` to produce the new binary. It does not own runtime behavior.
- `LegacySessionCleanup`: primary owner for `R11`. It removes the obsolete `internal/session` tree only after all imports and tests have switched to the new paths. It does not invent new behavior.
- `BackendAdapters`: collaborator on `R4` and `R5`. It owns backend-specific launch commands and readiness matchers for Codex, Claude, and Cursor under the new `internal/agentruntime/backend` path. It does not own tmux session or mkpipe lifecycle.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `ImplementWithReviewerCLI` | `DirLockSupport` | `dirlock.NewInCurrentDirectory() -> Lock` | Acquire before tmux session creation. Release on bootstrap failure or after `attach-session` returns. |
| `ImplementWithReviewerCLI` | `TmuxPaneLifecycle` | `tmux.NewTmuxSession(sessionName)`<br>`TmuxSessionLike.NewPane()`<br>`TmuxSessionLike.Attach(stdin, stdout, stderr)` | Implementer uses the first `NewPane()` result (default pane); reviewer uses the second pane returned after `split-window`. |
| `ImplementWithReviewerCLI` | `AgentRuntimeLifecycle` | `NewCodex(session, pane, config)` / `NewClaude(...)` / `NewCursor(...)` | Backend selection stays local to the command. Constructors stay explicit; no exported enum/parser is needed. |
| `AgentRuntimeLifecycle` | `BackendAdapters` | `Backend.Launch(pane, buildLaunchCommand)`<br>`Backend.WaitUntilReady(pane, opts)`<br>`Backend.SendPrompt(pane, prompt)` | Runtime start delegates launch/readiness to backend implementations and uses the shared env builder. |
| `ImplementWithReviewerCLI` | `AgentRuntimeLifecycle` | `Runtime.StartMkpipe() (absolutePath, error)`<br>`Runtime.MkpipeErrors() <-chan error`<br>`Runtime.StopMkpipe() error` | Start both runtimes first, then start both runtime-owned mkpipes. Treat `MkpipeErrors()` as bootstrap-fatal until `Attach()` begins; after attach handoff, log and drop those errors until teardown. |
| `RuntimeMkpipeLifecycle` | `orchestrator/internal/agentruntime/mkpipe` | `mkpipe.ResolvePath(Config)`<br>`mkpipe.Start(Config)`<br>`Listener.Close()` | Runtime start/stop methods may reuse the raw listener primitive and exported path resolver, but commands interact through runtime methods rather than directly through `mkpipe.Start`. |
| `RuntimeMkpipeLifecycle` | `AgentRuntimeLifecycle` | forward `listener.Messages()` into `Runtime.SendPrompt(prompt)` | Bootstrap delivery failures abort the command. After attach begins, delivery failures are logged to stderr and dropped. |
| `ImplementWithReviewerCLI` | `ImplementWithReviewerProtocol` | `generateSessionName(now)`<br>`buildImplementerPrompt(task, reviewerPath, sessionName)`<br>`buildReviewerPrompt(task, implementerPath, sessionName)` | Generate the session name before `tmux.NewTmuxSession(...)`. Build prompts only after both runtimes are started and both `Runtime.StartMkpipe()` calls have returned absolute paths. |
| `SingleAgentLauncherCLI` | `DirLockSupport` / `TmuxPaneLifecycle` / `AgentRuntimeLifecycle` / `RuntimeMkpipeLifecycle` | `Lock.Acquire()` -> `tmux.NewTmuxSession(name)` -> `NewPane()` -> `Runtime.Start()` -> optional `Runtime.StartMkpipe()` -> optional `Runtime.MkpipeErrors()` watch -> `Attach()` -> optional `Runtime.StopMkpipe()` | This sequencing preserves existing detached and attach-only behavior while moving ownership out of the runtime package. Commands must fail fast on mkpipe delivery errors before attach and switch to stderr logging after attach begins. |
| `AgentRuntimeLifecycle` | `TmuxPaneLifecycle` | `TmuxPaneLike.SendText(text)`<br>`TmuxPaneLike.Capture()`<br>`TmuxPaneLike.Close()` | Runtime `Close()` must target only the pane. Whole-session close stays on `TmuxSessionLike.Close()`. |

## File Ownership Map

- Create `orchestrator/internal/dirlock/lock.go` - owned by `DirLockSupport`; moved current-directory lock implementation used directly by command packages.
- Create `orchestrator/internal/dirlock/lock_test.go` - owned by `DirLockSupport`; stale-lock and PID-file regression suite after the move.

- Create `orchestrator/internal/agentruntime/runtime.go` - owned by `AgentRuntimeLifecycle`; explicit backend constructors, runtime state machine, `Start`, `SendPrompt`, `Capture`, `StartMkpipe`, `MkpipeErrors`, `StopMkpipe`, `Close`, and runtime error/reporting helpers with no attach or lock lifecycle.
- Create `orchestrator/internal/agentruntime/errors.go` - owned by `AgentRuntimeLifecycle`; typed runtime errors after removing attach/session-creation concerns from the reusable type.
- Create `orchestrator/internal/agentruntime/runtime_test.go` - owned by `AgentRuntimeLifecycle`; fake session/pane/listener tests for start sequencing, mkpipe state rules, asynchronous error exposure, prompt/capture behavior, and pane-only close.

- Create `orchestrator/internal/agentruntime/backend/backend.go` - owned by `BackendAdapters`; backend interface and shared readiness helpers at the new path.
- Create `orchestrator/internal/agentruntime/backend/backend_test.go` - owned by `BackendAdapters`; Codex/Claude/Cursor launch/readiness regressions after the package move.
- Create `orchestrator/internal/agentruntime/backend/codex.go` - owned by `BackendAdapters`; Codex launcher command and ready matcher.
- Create `orchestrator/internal/agentruntime/backend/claude.go` - owned by `BackendAdapters`; Claude launcher command and ready matcher.
- Create `orchestrator/internal/agentruntime/backend/cursor.go` - owned by `BackendAdapters`; Cursor launcher command and ready matcher.

- Create `orchestrator/internal/agentruntime/env/env.go` - owned by `BackendAdapters`; shared launch-command builder at the new path.
- Create `orchestrator/internal/agentruntime/env/env_test.go` - owned by `BackendAdapters`; existing path/prepend/stty regression tests after the move.

- Create `orchestrator/internal/agentruntime/mkpipe/listener.go` - owned by `RuntimeMkpipeLifecycle`; exported absolute-path resolution plus raw listener primitive with `BasenameOverride` support for runtime-owned attach-scoped forwarding.
- Create `orchestrator/internal/agentruntime/mkpipe/listener_test.go` - owned by `RuntimeMkpipeLifecycle`; default-path, absolute-path, existing-target, listener-lifecycle, and basename-override tests.

- Create `orchestrator/internal/agentruntime/tmux/errors.go` - owned by `TmuxPaneLifecycle`; tmux error wrappers including the new pane-close error.
- Create `orchestrator/internal/agentruntime/tmux/interfaces.go` - owned by `TmuxPaneLifecycle`; session/pane interfaces with `TmuxPaneLike.Close()`.
- Create `orchestrator/internal/agentruntime/tmux/tmux.go` - owned by `TmuxPaneLifecycle`; tmux session creation/opening, pane allocation, attach, send/capture, and pane close.
- Create `orchestrator/internal/agentruntime/tmux/tmux_test.go` - owned by `TmuxPaneLifecycle`; interface assertions plus new pane-close coverage via an injectable command-runner seam.

- Modify `orchestrator/cmd/tmux_codex/main.go` - owned by `SingleAgentLauncherCLI`; command-owned lock, tmux session creation, pane allocation, runtime creation, optional runtime-owned mkpipe start, and direct tmux attach.
- Modify `orchestrator/cmd/tmux_codex/main_test.go` - owned by `SingleAgentLauncherCLI`; fake lock/session/pane/runtime tests that preserve current banners and exit-code behavior.
- Modify `orchestrator/cmd/tmux_claude/main.go` - owned by `SingleAgentLauncherCLI`; same ownership split for Claude, including attach-scoped runtime mkpipe start/stop.
- Modify `orchestrator/cmd/tmux_claude/main_test.go` - owned by `SingleAgentLauncherCLI`; same regression coverage for Claude.
- Modify `orchestrator/cmd/tmux_cursor/main.go` - owned by `SingleAgentLauncherCLI`; same ownership split for Cursor, including attach-scoped runtime mkpipe start/stop.
- Modify `orchestrator/cmd/tmux_cursor/main_test.go` - owned by `SingleAgentLauncherCLI`; same regression coverage for Cursor.

- Create `orchestrator/cmd/implement-with-reviewer/main.go` - owned by `ImplementWithReviewerCLI`; workflow CLI parsing, lock/session bootstrap, runtime creation, runtime mkpipe start, prompt seeding, cleanup, and tmux attach.
- Create `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `ImplementWithReviewerCLI`; parser, startup ordering, cleanup, and status-line tests.
- Create `orchestrator/cmd/implement-with-reviewer/protocol.go` - owned by `ImplementWithReviewerProtocol`; exact marker constants, dedicated session-name helper, and prompt builders that encode handoff, approval, blocked, and clarification rules.
- Create `orchestrator/cmd/implement-with-reviewer/protocol_test.go` - owned by `ImplementWithReviewerProtocol`; marker literal, session-name helper, prompt-content, and marker-semantics tests.
- Create `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `ContractDocsAndBuildSurface`; operator contract for the new workflow command.

- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `ContractDocsAndBuildSurface`; update shared product-surface wording to include the new workflow binary.
- Modify `orchestrator/cmd/tmux_claude/CONTRACT.md` - owned by `ContractDocsAndBuildSurface`; same product-surface update.
- Modify `orchestrator/cmd/tmux_cursor/CONTRACT.md` - owned by `ContractDocsAndBuildSurface`; same product-surface update.
- Modify `Makefile` - owned by `ContractDocsAndBuildSurface`; add `bin/implement-with-reviewer` to the `build:` target.

- Delete `orchestrator/internal/session/errors.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/errors.go`.
- Delete `orchestrator/internal/session/session.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/runtime.go`.
- Delete `orchestrator/internal/session/session_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/runtime_test.go`.
- Delete `orchestrator/internal/session/backend/backend.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/backend/backend.go`.
- Delete `orchestrator/internal/session/backend/backend_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/backend/backend_test.go`.
- Delete `orchestrator/internal/session/backend/codex.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/backend/codex.go`.
- Delete `orchestrator/internal/session/backend/claude.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/backend/claude.go`.
- Delete `orchestrator/internal/session/backend/cursor.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/backend/cursor.go`.
- Delete `orchestrator/internal/session/env/env.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/env/env.go`.
- Delete `orchestrator/internal/session/env/env_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/env/env_test.go`.
- Delete `orchestrator/internal/session/mkpipe/listener.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/mkpipe/listener.go`.
- Delete `orchestrator/internal/session/mkpipe/listener_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/mkpipe/listener_test.go`.
- Delete `orchestrator/internal/session/tmux/errors.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/tmux/errors.go`.
- Delete `orchestrator/internal/session/tmux/interfaces.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/tmux/interfaces.go`.
- Delete `orchestrator/internal/session/tmux/tmux.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/tmux/tmux.go`.
- Delete `orchestrator/internal/session/tmux/tmux_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/agentruntime/tmux/tmux_test.go`.
- Delete `orchestrator/internal/session/dirlock/lock.go` - owned by `LegacySessionCleanup`; superseded by `internal/dirlock/lock.go`.
- Delete `orchestrator/internal/session/dirlock/lock_test.go` - owned by `LegacySessionCleanup`; superseded by `internal/dirlock/lock_test.go`.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/dirlock/lock.go`
- `orchestrator/internal/dirlock/lock_test.go`
- `orchestrator/internal/agentruntime/runtime.go`
- `orchestrator/internal/agentruntime/errors.go`
- `orchestrator/internal/agentruntime/runtime_test.go`
- `orchestrator/internal/agentruntime/backend/backend.go`
- `orchestrator/internal/agentruntime/backend/backend_test.go`
- `orchestrator/internal/agentruntime/backend/codex.go`
- `orchestrator/internal/agentruntime/backend/claude.go`
- `orchestrator/internal/agentruntime/backend/cursor.go`
- `orchestrator/internal/agentruntime/env/env.go`
- `orchestrator/internal/agentruntime/env/env_test.go`
- `orchestrator/internal/agentruntime/mkpipe/listener.go`
- `orchestrator/internal/agentruntime/mkpipe/listener_test.go`
- `orchestrator/internal/agentruntime/tmux/errors.go`
- `orchestrator/internal/agentruntime/tmux/interfaces.go`
- `orchestrator/internal/agentruntime/tmux/tmux.go`
- `orchestrator/internal/agentruntime/tmux/tmux_test.go`
- `orchestrator/cmd/tmux_codex/main.go`
- `orchestrator/cmd/tmux_codex/main_test.go`
- `orchestrator/cmd/tmux_claude/main.go`
- `orchestrator/cmd/tmux_claude/main_test.go`
- `orchestrator/cmd/tmux_cursor/main.go`
- `orchestrator/cmd/tmux_cursor/main_test.go`
- `orchestrator/cmd/implement-with-reviewer/main.go`
- `orchestrator/cmd/implement-with-reviewer/main_test.go`
- `orchestrator/cmd/implement-with-reviewer/protocol.go`
- `orchestrator/cmd/implement-with-reviewer/protocol_test.go`
- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`
- `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- `Makefile`

**Primary delete targets:**
- `orchestrator/internal/session/errors.go`
- `orchestrator/internal/session/session.go`
- `orchestrator/internal/session/session_test.go`
- `orchestrator/internal/session/backend/backend.go`
- `orchestrator/internal/session/backend/backend_test.go`
- `orchestrator/internal/session/backend/codex.go`
- `orchestrator/internal/session/backend/claude.go`
- `orchestrator/internal/session/backend/cursor.go`
- `orchestrator/internal/session/env/env.go`
- `orchestrator/internal/session/env/env_test.go`
- `orchestrator/internal/session/mkpipe/listener.go`
- `orchestrator/internal/session/mkpipe/listener_test.go`
- `orchestrator/internal/session/tmux/errors.go`
- `orchestrator/internal/session/tmux/interfaces.go`
- `orchestrator/internal/session/tmux/tmux.go`
- `orchestrator/internal/session/tmux/tmux_test.go`
- `orchestrator/internal/session/dirlock/lock.go`
- `orchestrator/internal/session/dirlock/lock_test.go`

**Incidental-only files:**
- None expected. Do not widen into `orchestrator/go.mod`, `scripts/.agentrc`, `scripts/bin/*`, or unrelated docs/tests. If a compile-breaking import list or generated snapshot forces a small supporting edit, stop and re-check the plan before expanding scope.

## Task List

All commands below assume the working directory is repo root `.../.workspace/harness`.

Baseline before feature work: `cd orchestrator && go test ./...` passes as of 2026-05-03.

Expected dirty-worktree context while implementing: the new development-doc folder for this feature may already exist, and `.harness_lock/pid` may churn during local runs. Do not treat either as feature work.

### Task 1: DirLockSupport

**Files:**
- Create: `orchestrator/internal/dirlock/lock.go`
- Create: `orchestrator/internal/dirlock/lock_test.go`
- Test: `orchestrator/internal/dirlock/lock_test.go`

**Covers:** `R9`
**Owner:** `DirLockSupport`
**Why:** Commands need a stable command-owned lock dependency before the runtime package can shed lock ownership cleanly.

- [ ] **Step 1: Port the existing dirlock regression tests to the new package path**

```go
func TestAcquireFailsWhenLockHeldByRunningPID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DirName)
	// seed pid file with os.Getpid() and assert LockHeldError
}
```

- [ ] **Step 2: Run the new dirlock package tests to verify the path is not implemented yet**

Run: `cd orchestrator && go test ./internal/dirlock -v`
Expected: FAIL with `package github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock is not in std` or missing-package errors.

- [ ] **Step 3: Move the current lock implementation into `internal/dirlock` without changing behavior**

```go
package dirlock

func NewInCurrentDirectory() (*Lock, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(wd, DirName)), nil
}
```

- [ ] **Step 4: Run the moved dirlock regression suite**

Run: `cd orchestrator && go test ./internal/dirlock -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/dirlock/lock.go orchestrator/internal/dirlock/lock_test.go
git commit -m "refactor: move dirlock to command-owned package"
```

### Task 2: TmuxPaneLifecycle

**Files:**
- Create: `orchestrator/internal/agentruntime/tmux/errors.go`
- Create: `orchestrator/internal/agentruntime/tmux/interfaces.go`
- Create: `orchestrator/internal/agentruntime/tmux/tmux.go`
- Create: `orchestrator/internal/agentruntime/tmux/tmux_test.go`
- Test: `orchestrator/internal/agentruntime/tmux/tmux_test.go`

**Covers:** `R6`
**Owner:** `TmuxPaneLifecycle`
**Why:** The reusable runtime cannot become pane-scoped until the tmux adapter exposes pane-level close independently from whole-session close.

- [ ] **Step 1: Port the existing tmux tests and add a new pane-close regression**

```go
func TestTmuxPaneCloseUsesKillPane(t *testing.T) {
	runner := &recordingRunner{}
	pane := &TmuxPane{target: "%9", runCommand: runner.run}

	if err := pane.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := runner.calls[0]; !reflect.DeepEqual(got, []string{"tmux", "kill-pane", "-t", "%9"}) {
		t.Fatalf("call = %v", got)
	}
}
```

- [ ] **Step 2: Run the new tmux package tests to confirm the new path and pane close do not exist yet**

Run: `cd orchestrator && go test ./internal/agentruntime/tmux -v`
Expected: FAIL with missing package or `undefined: (*TmuxPane).Close`.

- [ ] **Step 3: Move the tmux adapter into `internal/agentruntime/tmux` and add `TmuxPaneLike.Close()`**

```go
type TmuxPaneLike interface {
	SendText(text string) error
	Capture() (string, error)
	Close() error
}

func (p *TmuxPane) Close() error {
	_, err := p.runCommand("tmux", "kill-pane", "-t", p.target)
	if err != nil {
		return &KillPaneError{Target: p.target, Err: err}
	}
	return nil
}
```

- [ ] **Step 4: Run the tmux adapter regression suite**

Run: `cd orchestrator && go test ./internal/agentruntime/tmux -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agentruntime/tmux
git commit -m "refactor: move tmux adapter and add pane close"
```

### Task 3: BackendAdapters

**Files:**
- Create: `orchestrator/internal/agentruntime/backend/backend.go`
- Create: `orchestrator/internal/agentruntime/backend/backend_test.go`
- Create: `orchestrator/internal/agentruntime/backend/codex.go`
- Create: `orchestrator/internal/agentruntime/backend/claude.go`
- Create: `orchestrator/internal/agentruntime/backend/cursor.go`
- Create: `orchestrator/internal/agentruntime/env/env.go`
- Create: `orchestrator/internal/agentruntime/env/env_test.go`
- Test: `orchestrator/internal/agentruntime/backend/backend_test.go`
- Test: `orchestrator/internal/agentruntime/env/env_test.go`

**Covers:** `R4`
**Owner:** `BackendAdapters`
**Why:** Runtime start still depends on the shared launch-command builder plus backend-specific launch/readiness logic, but those must move under the new package tree before the new runtime can compile.

- [ ] **Step 1: Port the current backend and env regression suites to the new package paths**

```go
func TestCursorDefaultsLaunchPromptAndReadyMatcher(t *testing.T) {
	var b Backend = Cursor{}
	// existing cursor launch and readiness assertions
}
```

```go
func TestBuildLaunchCommandPrependsAgentBinAndSourcesAgentRC(t *testing.T) {
	got, err := BuildLaunchCommand("codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "stty -echo") {
		t.Fatalf("missing stty: %q", got)
	}
}
```

- [ ] **Step 2: Run the new support-package tests to verify the paths do not exist yet**

Run: `cd orchestrator && go test ./internal/agentruntime/backend ./internal/agentruntime/env -v`
Expected: FAIL with missing-package or undefined-symbol errors.

- [ ] **Step 3: Move backend and env code to `internal/agentruntime/...` and update imports to the new tmux path**

```go
package backend

import "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"

type Backend interface {
	DefaultSessionName() string
	Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error
	WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error
	SendPrompt(pane tmux.TmuxPaneLike, prompt string) error
}
```

- [ ] **Step 4: Run the moved backend/env test suites**

Run: `cd orchestrator && go test ./internal/agentruntime/backend ./internal/agentruntime/env -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agentruntime/backend orchestrator/internal/agentruntime/env
git commit -m "refactor: move backend and env packages under agentruntime"
```

### Task 4: RuntimeMkpipeLifecycle

**Files:**
- Create: `orchestrator/internal/agentruntime/mkpipe/listener.go`
- Create: `orchestrator/internal/agentruntime/mkpipe/listener_test.go`
- Test: `orchestrator/internal/agentruntime/mkpipe/listener_test.go`

**Covers:** `R7`, `E4`
**Owner:** `RuntimeMkpipeLifecycle`
**Why:** Both the workflow command and the single-agent launchers need the same raw FIFO primitive at the new path, plus one extra input for role-specific workflow basenames. Runtime methods own listener start/stop and forwarding; this task keeps the reusable package limited to path resolution and listener mechanics.

- [ ] **Step 1: Port the current mkpipe tests and add basename-override coverage**

```go
func TestStartUsesBasenameOverrideForSharedSessionRole(t *testing.T) {
	dir := t.TempDir()
	listener, err := Start(Config{
		WorkingDir:       dir,
		SessionName:      "implement-with-reviewer-1234",
		BasenameOverride: "implement-with-reviewer-1234-implementer",
		DefaultBasename:  "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	want := filepath.Join(dir, ".implement-with-reviewer-1234-implementer.mkpipe")
	if got := listener.Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the new mkpipe package tests to confirm the path is not implemented yet**

Run: `cd orchestrator && go test ./internal/agentruntime/mkpipe -v`
Expected: FAIL with missing package or `unknown field BasenameOverride`.

- [ ] **Step 3: Move the listener package, export absolute-path resolution, and add basename-override support**

```go
type Config struct {
	WorkingDir       string
	SessionName      string
	BasenameOverride string
	DefaultBasename  string
	RequestedPath    string
}

func ResolvePath(cfg Config) (string, error) {
	// RequestedPath still wins.
	basenameSource := cfg.SessionName
	if cfg.BasenameOverride != "" {
		basenameSource = cfg.BasenameOverride
	}
	basename := sanitizeSessionBasename(basenameSource, cfg.DefaultBasename)
	return filepath.Join(workingDir, "."+basename+".mkpipe"), nil
}
```

- [ ] **Step 4: Run the mkpipe regression suite**

Run: `cd orchestrator && go test ./internal/agentruntime/mkpipe -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agentruntime/mkpipe
git commit -m "refactor: move mkpipe and add workflow basename override"
```

### Task 5: AgentRuntimeLifecycle

**Files:**
- Create: `orchestrator/internal/agentruntime/runtime.go`
- Create: `orchestrator/internal/agentruntime/errors.go`
- Create: `orchestrator/internal/agentruntime/runtime_test.go`
- Test: `orchestrator/internal/agentruntime/runtime_test.go`

**Covers:** `R4`, `R5`, `R7`, `E5`
**Owner:** `AgentRuntimeLifecycle`
**Why:** This is the core ownership split. After this task, a runtime is just one pane/backend plus runtime-owned attach-scoped mkpipe and no longer creates sessions, acquires locks, or attaches to tmux.

- [ ] **Step 1: Port the current start/readiness tests and add failing runtime mkpipe lifecycle tests**

```go
func TestRuntimeStartWaitsForQuietReadyState(t *testing.T) {
	session := &fakeSession{name: "dev"}
	pane := &fakePane{captures: []string{"boot", "OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	rt := NewCodex(session, pane, Config{})
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseClosesOnlyPane(t *testing.T) {
	session := &fakeSession{name: "dev"}
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	rt := newReadyRuntime(session, pane)
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if !pane.closed || session.closed {
		t.Fatalf("pane.closed=%v session.closed=%v", pane.closed, session.closed)
	}
}

func TestRuntimeStartMkpipeRequiresStartedRuntime(t *testing.T) {
	rt := NewCodex(fakeSession("dev"), &fakePane{}, Config{Mkpipe: &MkpipeConfig{}})
	if _, err := rt.StartMkpipe(); err == nil {
		t.Fatal("expected StartMkpipe to require Start")
	}
}

func TestRuntimeMkpipeErrorsExposeAsyncDeliveryFailures(t *testing.T) {
	rt := newReadyRuntime(fakeSession("dev"), &fakePane{})
	// start mkpipe, inject a forwarding/listener error, and assert MkpipeErrors() receives it
}
```

- [ ] **Step 2: Run the new runtime tests to verify the package is still missing**

Run: `cd orchestrator && go test ./internal/agentruntime -v`
Expected: FAIL with missing package or `undefined: NewCodex`.

- [ ] **Step 3: Implement the pane-scoped `Runtime` type with runtime-owned mkpipe methods and no lock or attach ownership**

```go
type Runtime struct {
	backend   backend.Backend
	session   tmux.TmuxSessionLike
	pane      tmux.TmuxPaneLike
	mkpipeCfg *MkpipeConfig
	listener  mkpipe.Listener
	mkpipeErrs <-chan error
	state     state
	// ...
}

func (r *Runtime) StartMkpipe() (string, error) {
	if r.state != stateStarted {
		return "", newError(ErrorKindState, r.SessionName(), "", fmt.Errorf("runtime has not started"))
	}
	// resolve/start listener, save it, start forwarders, return absolute path
}

func (r *Runtime) StopMkpipe() error {
	// idempotent close of runtime-owned listener
	return nil
}

func (r *Runtime) MkpipeErrors() <-chan error {
	return r.mkpipeErrs
}

func (r *Runtime) Close() error {
	if r.listener != nil {
		_ = r.StopMkpipe()
	}
	return r.pane.Close()
}
```

- [ ] **Step 4: Run the runtime regression suite**

Run: `cd orchestrator && go test ./internal/agentruntime -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agentruntime/runtime.go orchestrator/internal/agentruntime/errors.go orchestrator/internal/agentruntime/runtime_test.go
git commit -m "refactor: add pane-scoped agent runtime"
```

### Task 6: SingleAgentLauncherCLI

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/main.go`
- Modify: `orchestrator/cmd/tmux_codex/main_test.go`
- Modify: `orchestrator/cmd/tmux_claude/main.go`
- Modify: `orchestrator/cmd/tmux_claude/main_test.go`
- Modify: `orchestrator/cmd/tmux_cursor/main.go`
- Modify: `orchestrator/cmd/tmux_cursor/main_test.go`
- Test: `orchestrator/cmd/tmux_codex/main_test.go`
- Test: `orchestrator/cmd/tmux_claude/main_test.go`
- Test: `orchestrator/cmd/tmux_cursor/main_test.go`

**Covers:** `R7`, `E5`, `R8`, `E6`, `R9`
**Owner:** `SingleAgentLauncherCLI`
**Why:** The three existing launchers must keep the same outward behavior while switching from the old runtime-owned session lifecycle to command-owned session/lock/bootstrap while invoking runtime-owned mkpipe only for attach-mode runs.

- [ ] **Step 1: Add failing command tests for lock/session/runtime sequencing and attach cleanup**

```go
func TestRunCodexAttachMkpipeStartsRuntimeAfterStartAndStopsItAfterAttach(t *testing.T) {
	// fake lock acquired once and released once
	// fake tmux session NewPane called once
	// fake runtime Start() before Runtime.StartMkpipe()
	// fake Runtime.StopMkpipe() after Attach() returns
}

func TestRunCodexAttachLogsRuntimeMkpipeErrorsAfterAttachBegins(t *testing.T) {
	// fake Runtime.MkpipeErrors() emits after Attach() starts
	// assert stderr log is written and the launcher still follows attach result semantics
}
```

- [ ] **Step 2: Run the three launcher suites to see the old `internal/session` seams fail the new expectations**

Run: `cd orchestrator && go test ./cmd/tmux_codex ./cmd/tmux_claude ./cmd/tmux_cursor -v`
Expected: FAIL with outdated dependencies (`session.Config`, missing tmux/lock/runtime seams, or unchanged attach ordering).

- [ ] **Step 3: Refactor each launcher in place to own lock + tmux session bootstrap directly**

```go
lock, err := deps.newLock()
if err != nil { /* print and return 1 */ }
if err := lock.Acquire(); err != nil { /* print and return 1 */ }

tmuxSession, err := deps.newTmuxSession(parsed.sessionName)
pane, err := tmuxSession.NewPane()
rt := deps.newRuntime(tmuxSession, pane, agentruntime.Config{})
if err := rt.Start(); err != nil { /* cleanup */ }
if parsed.attach && parsed.mkpipeEnabled {
	path, err := rt.StartMkpipe()
	mkpipeErrs := rt.MkpipeErrors()
	// fail fast if mkpipeErrs reports a bootstrap delivery error before attach begins
	// print existing attach banner wording with path
}
// once Attach() begins, drain mkpipeErrs to stderr until StopMkpipe() during teardown
err = tmuxSession.Attach(stdin, stdout, stderr)
if parsed.attach && parsed.mkpipeEnabled {
	_ = rt.StopMkpipe()
}
```

- [ ] **Step 4: Run the three launcher suites**

Run: `cd orchestrator && go test ./cmd/tmux_codex ./cmd/tmux_claude ./cmd/tmux_cursor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/tmux_codex/main.go orchestrator/cmd/tmux_codex/main_test.go orchestrator/cmd/tmux_claude/main.go orchestrator/cmd/tmux_claude/main_test.go orchestrator/cmd/tmux_cursor/main.go orchestrator/cmd/tmux_cursor/main_test.go
git commit -m "refactor: move tmux launcher ownership to commands"
```

### Task 7: ImplementWithReviewerCLI

**Files:**
- Create: `orchestrator/cmd/implement-with-reviewer/main.go`
- Create: `orchestrator/cmd/implement-with-reviewer/main_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/protocol.go`
- Create: `orchestrator/cmd/implement-with-reviewer/protocol_test.go`
- Create: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- Test: `orchestrator/cmd/implement-with-reviewer/main_test.go`
- Test: `orchestrator/cmd/implement-with-reviewer/protocol_test.go`

**Covers:** `R1`, `E1`, `R2`, `E2`, `R3`, `R7`, `E3`, `E4`, `E5`, `E7`
**Owner:** `ImplementWithReviewerCLI`
**Why:** This task adds the new workflow binary itself and locks the exact startup ordering, runtime-owned mkpipe startup, protocol prompts, and cleanup semantics.

- [ ] **Step 1: Write the failing workflow CLI and protocol tests**

```go
func TestRunBootstrapsSharedSessionStartsBothRuntimeMkpipesThenSeedsPrompts(t *testing.T) {
	// assert:
	// 1. implementer runtime Start()
	// 2. reviewer runtime Start()
	// 3. implementer Runtime.StartMkpipe()
	// 4. reviewer Runtime.StartMkpipe()
	// 5. implementer SendPrompt(...)
	// 6. reviewer SendPrompt(...)
	// 7. one pre-attach status line
	// 8. tmux Attach(...)
}

func TestRunStopsBothRuntimeMkpipesAndReleasesLockAfterAttachReturns(t *testing.T) {
	// assert both Runtime.StopMkpipe() calls happen after Attach() returns
	// assert the shared tmux session is left running and the lock is released
}

func TestRunFailsIfRuntimeMkpipeReportsDeliveryErrorBeforeAttach(t *testing.T) {
	// start both runtimes and both runtime mkpipes
	// inject a pre-attach error through Runtime.MkpipeErrors()
	// assert exit 1, tmux session close, lock release, and both runtime mkpipes stopped
}

func TestBuildReviewerPromptIncludesImplementerPipeAndWaitInstruction(t *testing.T) {
	prompt := buildReviewerPrompt("ship it", "/abs/impl.pipe", "implement-with-reviewer-123")
	if !strings.Contains(prompt, "[IWR_IMPLEMENTATION_READY]") {
		t.Fatal("missing marker")
	}
	if !strings.Contains(prompt, "/abs/impl.pipe") {
		t.Fatal("missing peer path")
	}
}

func TestBuildPromptsDescribeApprovalAndBlockedProtocolRules(t *testing.T) {
	// assert [IWR_APPROVED] is terminal and leaves both agents idle
	// assert [IWR_BLOCKED] tells the agent to ask the peer first through mkpipe
}

func TestGenerateSessionNameUsesWorkflowPrefixAndTmuxSafeSuffix(t *testing.T) {
	// assert prefix "implement-with-reviewer-"
	// assert non-empty suffix and tmux-safe output
}
```

- [ ] **Step 2: Run the workflow package tests to confirm the command does not exist yet**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer -v`
Expected: FAIL with missing package or undefined prompt-builder symbols.

- [ ] **Step 3: Implement the workflow command and protocol helpers**

```go
const (
	markerImplementationReady = "[IWR_IMPLEMENTATION_READY]"
	markerChangesRequested    = "[IWR_CHANGES_REQUESTED]"
	markerApproved            = "[IWR_APPROVED]"
	markerBlocked             = "[IWR_BLOCKED]"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps workflowDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}
	sessionName := generateSessionName(time.Now())
	// acquire lock
	// create tmux session + two panes
	// construct implementer/reviewer runtimes
	// start both runtimes
	// start both runtime-owned mkpipes and capture their absolute paths
	// treat Runtime.MkpipeErrors() as bootstrap-fatal until Attach() begins
	// send seeded prompts with peer paths
	// print pre-attach line
	// once Attach() begins, drain Runtime.MkpipeErrors() to stderr and stop both runtime mkpipes on return
}
```

- [ ] **Step 4: Run the workflow test suite**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer/main.go orchestrator/cmd/implement-with-reviewer/main_test.go orchestrator/cmd/implement-with-reviewer/protocol.go orchestrator/cmd/implement-with-reviewer/protocol_test.go orchestrator/cmd/implement-with-reviewer/CONTRACT.md
git commit -m "feat: add implement-with-reviewer workflow"
```

### Task 8: ContractDocsAndLegacyCleanup

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_claude/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- Modify: `Makefile`
- Delete: `orchestrator/internal/session/errors.go`
- Delete: `orchestrator/internal/session/session.go`
- Delete: `orchestrator/internal/session/session_test.go`
- Delete: `orchestrator/internal/session/backend/backend.go`
- Delete: `orchestrator/internal/session/backend/backend_test.go`
- Delete: `orchestrator/internal/session/backend/codex.go`
- Delete: `orchestrator/internal/session/backend/claude.go`
- Delete: `orchestrator/internal/session/backend/cursor.go`
- Delete: `orchestrator/internal/session/env/env.go`
- Delete: `orchestrator/internal/session/env/env_test.go`
- Delete: `orchestrator/internal/session/mkpipe/listener.go`
- Delete: `orchestrator/internal/session/mkpipe/listener_test.go`
- Delete: `orchestrator/internal/session/tmux/errors.go`
- Delete: `orchestrator/internal/session/tmux/interfaces.go`
- Delete: `orchestrator/internal/session/tmux/tmux.go`
- Delete: `orchestrator/internal/session/tmux/tmux_test.go`
- Delete: `orchestrator/internal/session/dirlock/lock.go`
- Delete: `orchestrator/internal/session/dirlock/lock_test.go`

**Covers:** `R10`, `R11`
**Owner:** `ContractDocsAndBuildSurface`
**Why:** Finish the public contract and remove the old package tree only after all runtime/command callers have moved to the new paths.

- [ ] **Step 1: Add one failing end-to-end verification command for legacy-path cleanup**

Run: `cd orchestrator && rg -n 'internal/session' .`
Expected: non-zero AFTER cleanup work is complete; before the cleanup delete step it should still print the old path hits.

- [ ] **Step 2: Update docs/build surface and remove the obsolete `internal/session` tree**

```make
build:
	cd orchestrator && go build -o ../bin/tmux_codex ./cmd/tmux_codex
	cd orchestrator && go build -o ../bin/tmux_claude ./cmd/tmux_claude
	cd orchestrator && go build -o ../bin/tmux_cursor ./cmd/tmux_cursor
	cd orchestrator && go build -o ../bin/implement-with-reviewer ./cmd/implement-with-reviewer
```

```markdown
The supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
- `implement-with-reviewer`
```

- [ ] **Step 3: Run the final verification suite**

Run: `cd orchestrator && go test ./...`
Expected: PASS

Run: `cd .. && make build`
Expected: PASS and `bin/implement-with-reviewer` exists

Run: `cd orchestrator && rg -n 'internal/session' .`
Expected: no output

- [ ] **Step 4: Commit**

```bash
git add orchestrator/cmd/tmux_codex/CONTRACT.md orchestrator/cmd/tmux_claude/CONTRACT.md orchestrator/cmd/tmux_cursor/CONTRACT.md Makefile
git add -A orchestrator/internal/agentruntime orchestrator/internal/dirlock orchestrator/cmd/implement-with-reviewer
git rm orchestrator/internal/session/errors.go orchestrator/internal/session/session.go orchestrator/internal/session/session_test.go orchestrator/internal/session/backend/backend.go orchestrator/internal/session/backend/backend_test.go orchestrator/internal/session/backend/codex.go orchestrator/internal/session/backend/claude.go orchestrator/internal/session/backend/cursor.go orchestrator/internal/session/env/env.go orchestrator/internal/session/env/env_test.go orchestrator/internal/session/mkpipe/listener.go orchestrator/internal/session/mkpipe/listener_test.go orchestrator/internal/session/tmux/errors.go orchestrator/internal/session/tmux/interfaces.go orchestrator/internal/session/tmux/tmux.go orchestrator/internal/session/tmux/tmux_test.go orchestrator/internal/session/dirlock/lock.go orchestrator/internal/session/dirlock/lock_test.go
git commit -m "feat: add implement-with-reviewer and agentruntime refactor"
```
