# Session Package Refactor Implementation Plan

**Goal:** Refactor the tmux-backed Codex and Claude launchers around a reusable `orchestrator/session` package while preserving the existing `tmux_codex` and `tmux_claude` operator contract.

**Architecture:** Put the shared lifecycle state machine directly in `orchestrator/session`, not in a separate `runtime` package, per implementation Q&A. Keep backend differences explicit through an internal backend interface with separate Codex and Claude implementations under `orchestrator/internal/session/backend`, and move the lower-level tmux, env, dirlock, and mkpipe helpers under `orchestrator/internal/session/*` before deleting the old `internal/agent`-shaped package tree.

**Tech Stack:** Go 1.26, standard library `flag`/`io`/`os`/`os/signal`/`sync`/`syscall`/`time`, tmux CLI, Unix FIFOs, existing harness command tests.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Expose a reusable public package at `orchestrator/session` with explicit `NewCodex(config)` and `NewClaude(config)` constructors, a shared session-handle type, and a small lifecycle-oriented config surface with no initial-prompt field. | `SessionLifecycle` | `SessionBackendPackage` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `NewCodex(Config) *Session`<br>`NewClaude(Config) *Session`<br>`type Config`<br>`type MkpipeConfig`<br>`type Lock`<br>`type LockPolicy` | `orchestrator/session/session_test.go` constructor/default-surface tests covering public methods and config defaults. |
| R2 | Keep backend choice explicit but internal by defining an internal backend interface plus separate Codex and Claude implementations, each owning its own default session name, launch command, and readiness matcher. | `SessionBackendPackage` | `SessionLifecycle` | `orchestrator/internal/session/backend/backend.go`<br>`orchestrator/internal/session/backend/codex.go`<br>`orchestrator/internal/session/backend/claude.go`<br>`orchestrator/internal/session/backend/backend_test.go` | `type Backend interface`<br>`(Codex).DefaultSessionName()`<br>`(Codex).Command()`<br>`(Codex).Ready(capture string) bool`<br>`(Claude).DefaultSessionName()`<br>`(Claude).Command()`<br>`(Claude).Ready(capture string) bool` | `orchestrator/internal/session/backend/backend_test.go` readiness/default-name regression tests for both backends. |
| R3 | Keep `tmux_codex` as a stable thin adapter that still owns flag parsing, usage validation, success wording, mkpipe banner wording, and exit-code mapping while delegating lifecycle work to `orchestrator/session`. | `CodexCommandAdapter` | `SessionLifecycle` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go` | `run(args, stdin, stdout, stderr, deps)`<br>`deps.newSession(session.Config)`<br>`(*Session).Start()`<br>`(*Session).Attach(session.AttachOptions)` | `orchestrator/cmd/tmux_codex/main_test.go` flag, banner, attach, and exit-code regression tests using a fake session handle. |
| R4 | Keep `tmux_claude` as the same kind of thin stable adapter, with Claude-specific defaults and wording preserved in command code rather than in the library. | `ClaudeCommandAdapter` | `SessionLifecycle` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go` | `run(args, stdin, stdout, stderr, deps)`<br>`deps.newSession(session.Config)`<br>`(*Session).Start()`<br>`(*Session).Attach(session.AttachOptions)` | `orchestrator/cmd/tmux_claude/main_test.go` flag, banner, attach, and exit-code regression tests using a fake session handle. |
| R5 | Move the tmux adapter under `orchestrator/internal/session/tmux` without changing its current responsibilities: tmux session creation/opening, pane handling, attach, send, capture, and tmux-shaped errors. | `SessionTmuxAdapter` | `SessionLifecycle` | `orchestrator/internal/session/tmux/errors.go`<br>`orchestrator/internal/session/tmux/interfaces.go`<br>`orchestrator/internal/session/tmux/tmux.go`<br>`orchestrator/internal/session/tmux/tmux_test.go` | `tmux.NewTmuxSession(name)`<br>`tmux.OpenTmuxSession(name)`<br>`TmuxSessionLike.NewPane()`<br>`TmuxSessionLike.Attach(stdin, stdout, stderr)`<br>`TmuxPaneLike.SendText(text)`<br>`TmuxPaneLike.Capture()`<br>`TmuxSessionLike.Close()` | `orchestrator/internal/session/tmux/tmux_test.go` moved regression suite covering create/open/attach/send/capture/close behavior. |
| R6 | Move the shared launch-environment builder under `orchestrator/internal/session/env` without changing the `.agentrc`, `~/.agent-bin`, `PATH`, and `stty -echo` contract. | `SessionEnvBuilder` | `SessionLifecycle` | `orchestrator/internal/session/env/env.go`<br>`orchestrator/internal/session/env/env_test.go` | `env.BuildLaunchCommand(command string, args ...string)` | `orchestrator/internal/session/env/env_test.go` moved regression tests for path resolution and launch command rendering. |
| R7 | Move the current working-directory lock implementation under `orchestrator/internal/session/dirlock`, keep its stale-lock behavior, and expose it through `session.CurrentDirectoryLockPolicy()` rather than from `cmd/*`. | `SessionDirLockPolicy` | `SessionLifecycle` | `orchestrator/internal/session/dirlock/lock.go`<br>`orchestrator/internal/session/dirlock/lock_test.go`<br>`orchestrator/session/session.go`<br>`orchestrator/session/session_test.go` | `dirlock.NewInCurrentDirectory()`<br>`CurrentDirectoryLockPolicy() LockPolicy`<br>`LockPolicy() (Lock, error)` | `orchestrator/internal/session/dirlock/lock_test.go` moved stale-lock tests plus `orchestrator/session/session_test.go` coverage that the public policy returns a usable lock factory. |
| R8 | Move mkpipe under `orchestrator/internal/session/mkpipe`, preserve its FIFO path resolution and delivery semantics, and make the session layer the owner of when listener startup and cleanup happen. | `SessionMkpipe` | `SessionLifecycle` | `orchestrator/internal/session/mkpipe/listener.go`<br>`orchestrator/internal/session/mkpipe/listener_test.go`<br>`orchestrator/session/session.go`<br>`orchestrator/session/session_test.go` | `mkpipe.Start(mkpipe.Config)`<br>`Listener.Path()`<br>`Listener.Messages()`<br>`Listener.Errors()`<br>`Listener.Close()` | `orchestrator/internal/session/mkpipe/listener_test.go` moved FIFO/path tests plus `orchestrator/session/session_test.go` attach-flow mkpipe orchestration tests. |
| R9 | `Session.Start()` must implement the detached launch path: acquire any configured lock, create a new tmux session, build the backend launch command through `internal/session/env`, send it into tmux, poll captures until the backend reports ready and the quiet-period window elapses, fail with a typed startup error if the ready-timeout expires first, release transient startup resources, and leave the session running in `started` state. | `SessionLifecycle` | `SessionBackendPackage`<br>`SessionTmuxAdapter`<br>`SessionEnvBuilder`<br>`SessionDirLockPolicy` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).Start() error`<br>`waitUntilReady()`<br>`Lock.Acquire()`<br>`Lock.Release()`<br>`Backend.Command()`<br>`Backend.Ready(capture)`<br>`TmuxPaneLike.Capture()` | `orchestrator/session/session_test.go` detached-start tests for lock hold duration, launch send, quiet-period readiness, ready-timeout failure, and success state transitions. |
| R10 | `Session.Attach()` on a brand-new handle must run the attached launch path: optional lock acquisition, startup, optional mkpipe listener setup after readiness, optional `BeforeAttach` callback, blocking tmux attach, cleanup of transient resources when attach returns or the launcher is interrupted, and no automatic backend-session shutdown on successful attach return. | `SessionLifecycle` | `SessionMkpipe`<br>`SessionTmuxAdapter`<br>`SessionDirLockPolicy` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).Attach(opts AttachOptions) error`<br>`type AttachOptions`<br>`type AttachInfo`<br>`AttachOptions.BeforeAttach(info)`<br>`signal.NotifyContext(...)` | `orchestrator/session/session_test.go` attached-launch tests covering startup order, mkpipe startup timing, hook timing, lock hold duration, cleanup on return or interrupt, and retained started state after successful attach return. |
| R11 | `Session.Attach()` on an already-started handle must reattach without rerunning startup, without retroactively enabling mkpipe, without reacquiring the startup-scoped lock, and while still invoking `BeforeAttach` immediately before handoff. | `SessionLifecycle` | `SessionTmuxAdapter` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).Attach(opts AttachOptions) error` on `started` state<br>`AttachOptions.BeforeAttach(info)` | `orchestrator/session/session_test.go` reattach tests asserting no second startup, no mkpipe start, no lock reacquisition, and hook invocation with an empty `MkpipePath`. |
| R12 | The public session handle must expose reusable live-session controls: `SessionName` from any state, `SendPrompt`, `Capture`, and idempotent `Close`, with a session-owned typed error boundary replacing the old `internal/agent.AgentError`. | `SessionLifecycle` | `SessionTmuxAdapter` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).SessionName() string`<br>`(*Session).SendPrompt(prompt string) error`<br>`(*Session).Capture() (string, error)`<br>`(*Session).Close() error`<br>`type Error` | `orchestrator/session/session_test.go` prompt/capture/close tests and typed-error assertions. |
| R13 | Retire the old `internal/agent` package tree and the old top-level `internal/dirlock` and `internal/mkpipe` packages once all callers and tests use the new `session` and `internal/session/*` paths. | `LegacySessionCleanup` | `SessionLifecycle`<br>`SessionBackendPackage`<br>`SessionTmuxAdapter`<br>`SessionEnvBuilder`<br>`SessionDirLockPolicy`<br>`SessionMkpipe` | `orchestrator/internal/agent/agent.go`<br>`orchestrator/internal/agent/agent_test.go`<br>`orchestrator/internal/agent/errors.go`<br>`orchestrator/internal/agent/codex.go`<br>`orchestrator/internal/agent/claude.go`<br>`orchestrator/internal/agent/env/env.go`<br>`orchestrator/internal/agent/env/env_test.go`<br>`orchestrator/internal/agent/tmux/errors.go`<br>`orchestrator/internal/agent/tmux/interfaces.go`<br>`orchestrator/internal/agent/tmux/tmux.go`<br>`orchestrator/internal/agent/tmux/tmux_test.go`<br>`orchestrator/internal/dirlock/lock.go`<br>`orchestrator/internal/dirlock/lock_test.go`<br>`orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go` | import-path migration plus file deletion after new packages are green | Final full-suite verification with `go test ./...` plus repo-scope grep that no Go sources still import the retired package paths. |
| E1 | `Start()` must reject mkpipe-enabled config with a clear error because mkpipe remains attach-only at the library boundary. | `SessionLifecycle` | `SessionMkpipe` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).Start() error` | `orchestrator/session/session_test.go` `TestStartRejectsMkpipeConfig`. |
| E2 | Repeated or contradictory lifecycle calls must error predictably: `Start()` from `started` or `closed`, `Attach()` from `closed`, and any restart attempt after failed startup should be rejected. | `SessionLifecycle` | None | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | state checks inside `Start()` / `Attach()` | `orchestrator/session/session_test.go` state-transition table tests. |
| E3 | A failed startup attempt on a brand-new handle must best-effort close any newly created tmux session, release any held lock, transition the handle to `closed`, and force callers to construct a new handle to retry. | `SessionLifecycle` | `SessionTmuxAdapter`<br>`SessionDirLockPolicy` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | startup cleanup path inside `Start()` / attached-start branch of `Attach()` | `orchestrator/session/session_test.go` cleanup tests for launch/build/readiness failures. |
| E4 | If the requested tmux session name already exists, startup must fail clearly and leave the existing tmux session untouched. | `SessionLifecycle` | `SessionTmuxAdapter` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | wrapped `tmux.NewTmuxSession(name)` failure surfaced as a session error | `orchestrator/session/session_test.go` startup failure test using a fake `newTmuxSession` that returns a name-collision error. |
| E5 | `BeforeAttach` must receive the resolved session name and the resolved absolute mkpipe path; `MkpipePath` must be empty when mkpipe is disabled for that attach call, including reattach on an already-started handle. | `SessionLifecycle` | `SessionMkpipe` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `AttachOptions.BeforeAttach(info AttachInfo)`<br>`AttachInfo.SessionName`<br>`AttachInfo.MkpipePath` | `orchestrator/session/session_test.go` hook metadata tests for mkpipe-enabled first attach, mkpipe-disabled first attach, and mkpipe-disabled reattach paths. |
| E6 | Nil attach streams must continue to fall back to `os.Stdin`, `os.Stdout`, and `os.Stderr` through the tmux adapter rather than forcing commands to supply explicit streams. | `SessionTmuxAdapter` | `SessionLifecycle` | `orchestrator/internal/session/tmux/tmux.go`<br>`orchestrator/internal/session/tmux/tmux_test.go`<br>`orchestrator/session/session_test.go` | `TmuxSessionLike.Attach(stdin, stdout, stderr)` | `orchestrator/internal/session/tmux/tmux_test.go` existing nil-stream attach regression coverage and `orchestrator/session/session_test.go` attach smoke test with nil options. |
| E7 | `SendPrompt()` and `Capture()` before successful startup must return typed session errors, while `Close()` from `new` or `closed` must be a no-op. | `SessionLifecycle` | None | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `(*Session).SendPrompt(prompt string) error`<br>`(*Session).Capture() (string, error)`<br>`(*Session).Close() error` | `orchestrator/session/session_test.go` pre-start and double-close error/no-op tests. |
| E8 | If attach fails after the backend session is ready, the session layer must still clean up transient attach-owned resources like mkpipe and held locks, but it must not auto-close the backend session and it must leave the handle in `started` state. | `SessionLifecycle` | `SessionMkpipe`<br>`SessionDirLockPolicy` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | attach failure branch inside `(*Session).Attach()` | `orchestrator/session/session_test.go` attach-error tests asserting listener cleanup and retained started state. |
| E9 | Readiness polling must require both the backend-specific ready matcher and a quiet-period stability window, and it must return a typed startup error with the latest capture if the session never stabilizes before the ready-timeout. | `SessionLifecycle` | `SessionBackendPackage`<br>`SessionTmuxAdapter` | `orchestrator/session/session.go`<br>`orchestrator/session/errors.go`<br>`orchestrator/session/session_test.go` | `waitUntilReady()`<br>`TmuxPaneLike.Capture()`<br>`Backend.Ready(capture)` | `orchestrator/session/session_test.go` quiet-period and ready-timeout tests for detached start and attached-start reuse. |
| E10 | If the launcher process is interrupted while `Attach()` is blocked after attach-owned resources are created, the session layer must best-effort close the mkpipe listener and release any held lock, but it must not auto-close the backend tmux session and it must leave the handle in `started` state once startup succeeded. | `SessionLifecycle` | `SessionMkpipe`<br>`SessionDirLockPolicy` | `orchestrator/session/session.go`<br>`orchestrator/session/session_test.go` | `signal.NotifyContext(...)`<br>`Listener.Close()`<br>`Lock.Release()` | `orchestrator/session/session_test.go` interrupt-cleanup tests asserting listener/lock cleanup and retained started state. |

## Component Responsibility Map

- `SessionLifecycle`: primary owner for the exported `orchestrator/session` surface, session state machine, readiness polling policy (ready matcher plus quiet-period and timeout), lock hold/release timing, detached start, attached launch, interrupt-safe attach cleanup, reattach, prompt send, capture, close behavior, and the new typed session error boundary. It collaborates with the internal helper packages and backends through unexported dependency injection for tests. It does not own CLI parsing, command wording, or low-level tmux/FIFO implementations.
- `SessionBackendPackage`: primary owner for backend-specific differences only: default session names, backend command selection, and readiness matchers. It collaborates with `SessionLifecycle` through a small internal interface. It does not own lock policy, tmux orchestration, mkpipe timing, or public API shape.
- `SessionTmuxAdapter`: primary owner for all tmux binary interactions under `internal/session/tmux`, including session creation/opening, pane allocation, attach, send, capture, and tmux-shaped errors. It does not own startup ordering, readiness polling, or lifecycle state enforcement.
- `SessionEnvBuilder`: primary owner for rendering the shell command that sources `.agentrc`, prepends `~/.agent-bin`, disables echo, and launches the backend command. It does not own tmux orchestration or readiness checks.
- `SessionDirLockPolicy`: primary owner for the current-working-directory lock implementation and stale-lock cleanup semantics. It does not decide when locks are acquired or released; `SessionLifecycle` owns that timing.
- `SessionMkpipe`: primary owner for FIFO path resolution, validation, creation, EOF-delimited reads, normalized prompt delivery, and deterministic listener teardown. It does not own whether a given session call should start mkpipe; `SessionLifecycle` owns attach-only policy and orchestration.
- `CodexCommandAdapter`: primary owner for `tmux_codex` flags, usage errors, success output, mkpipe banner wording, and exit-code mapping. It collaborates with `SessionLifecycle` only through `NewCodex`, `Start`, and `Attach`. It does not own lifecycle state or helper package wiring.
- `ClaudeCommandAdapter`: primary owner for the analogous `tmux_claude` command contract. It differs from `CodexCommandAdapter` only in Claude-specific defaults and wording.
- `LegacySessionCleanup`: primary owner for removing the retired `internal/agent` package tree and old helper paths after the new session-based flow is in place. It does not own runtime behavior; it only removes superseded code once replacement callers and tests are green.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CodexCommandAdapter` | `SessionLifecycle` | `session.NewCodex(session.Config{SessionName, Mkpipe, LockPolicy}) *session.Session` | `tmux_codex` still validates CLI flags locally, then passes only lifecycle settings into the session package. |
| `ClaudeCommandAdapter` | `SessionLifecycle` | `session.NewClaude(session.Config{SessionName, Mkpipe, LockPolicy}) *session.Session` | Same shape as Codex; only defaults and wording differ. |
| `SessionLifecycle` | `SessionBackendPackage` | `Backend.DefaultSessionName()`, `Backend.Command()`, `Backend.Ready(capture string) bool` | Backend implementations own only backend-specific choices. Session constructors resolve empty `Config.SessionName` through `DefaultSessionName()`. `Ready(capture)` is only one part of readiness; `SessionLifecycle` layers quiet-period and timeout rules around repeated captures. |
| `SessionLifecycle` | `SessionEnvBuilder` | `env.BuildLaunchCommand(command string, args ...string) (string, error)` | Build the shell command immediately before sending it into tmux. Any error here is a launch/startup failure and must trigger startup cleanup. |
| `SessionLifecycle` | `SessionTmuxAdapter` | `tmux.NewTmuxSession(name)`, `TmuxSessionLike.NewPane()`, `TmuxPaneLike.SendText(text)`, `TmuxPaneLike.Capture()`, `TmuxSessionLike.Attach(stdin, stdout, stderr)`, `TmuxSessionLike.Close()` | `SessionLifecycle` owns sequencing and state; the tmux package owns only direct tmux operations and process-stream fallback. Capture polling continues until the ready matcher plus quiet period succeed or the ready-timeout expires. |
| `SessionLifecycle` | `SessionDirLockPolicy` | `LockPolicy() (Lock, error)`, then `Lock.Acquire()` / `Lock.Release()` | Hold duration differs by mode: detached `Start()` releases after startup completes; attached startup holds through attach return or interrupt; reattach on a started handle does not reacquire. |
| `SessionLifecycle` | `SessionMkpipe` | `mkpipe.Start(mkpipe.Config{SessionName, DefaultBasename, RequestedPath}) (Listener, error)` plus `Messages()`, `Errors()`, `Close()` | Mkpipe starts only after a new attached session becomes ready. `SessionLifecycle` forwards each message to `SendPrompt`, logs listener/delivery errors via the caller-provided stderr stream, and always closes the listener on attach return, attach failure, or attach-time interrupt. |
| `SessionLifecycle` | `OS signal runtime` | `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` | Attached runs use an internal signal-context seam so mkpipe listeners and held locks are released even if the launcher is interrupted while blocked in `tmux attach-session`. |
| `SessionLifecycle` | `CodexCommandAdapter` | `AttachOptions.BeforeAttach(func(AttachInfo))` | The hook fires immediately before blocking `tmux attach-session` on both first attach and reattach. `tmux_codex` prints its current mkpipe banner there and nowhere else. |
| `SessionLifecycle` | `ClaudeCommandAdapter` | `AttachOptions.BeforeAttach(func(AttachInfo))` | Same contract as Codex, including reattach behavior, with Claude wording. |

## File Ownership Map

- Create `orchestrator/session/session.go` - owned by `SessionLifecycle`; public config types, constructors, lifecycle state machine, readiness timing defaults, signal/cleanup dependency seams for tests, and all method implementations.
- Create `orchestrator/session/errors.go` - owned by `SessionLifecycle`; typed session-level lifecycle/state/capture/close errors that replace `internal/agent.AgentError`.
- Create `orchestrator/session/session_test.go` - owned by `SessionLifecycle`; lifecycle, state, quiet-period/timeout readiness, attach interruption cleanup, mkpipe, lock, and typed-error regression coverage with same-package test fakes.

- Create `orchestrator/internal/session/backend/backend.go` - owned by `SessionBackendPackage`; internal backend interface shared by session lifecycle code.
- Create `orchestrator/internal/session/backend/codex.go` - owned by `SessionBackendPackage`; Codex-specific command/default-name/readiness implementation.
- Create `orchestrator/internal/session/backend/claude.go` - owned by `SessionBackendPackage`; Claude-specific command/default-name/readiness implementation.
- Create `orchestrator/internal/session/backend/backend_test.go` - owned by `SessionBackendPackage`; readiness/default regression tests for both backends.

- Create `orchestrator/internal/session/tmux/errors.go` - owned by `SessionTmuxAdapter`; tmux-specific error wrappers at the new package path.
- Create `orchestrator/internal/session/tmux/interfaces.go` - owned by `SessionTmuxAdapter`; session/pane interfaces used by `orchestrator/session` and tests.
- Create `orchestrator/internal/session/tmux/tmux.go` - owned by `SessionTmuxAdapter`; tmux process execution, attach fallback behavior, send, and capture logic.
- Create `orchestrator/internal/session/tmux/tmux_test.go` - owned by `SessionTmuxAdapter`; moved tmux adapter regression tests.

- Create `orchestrator/internal/session/env/env.go` - owned by `SessionEnvBuilder`; launch command rendering under the new package path.
- Create `orchestrator/internal/session/env/env_test.go` - owned by `SessionEnvBuilder`; moved env regression tests.

- Create `orchestrator/internal/session/dirlock/lock.go` - owned by `SessionDirLockPolicy`; working-directory lock implementation under the new package path.
- Create `orchestrator/internal/session/dirlock/lock_test.go` - owned by `SessionDirLockPolicy`; moved stale-lock and PID-file regression tests.

- Create `orchestrator/internal/session/mkpipe/listener.go` - owned by `SessionMkpipe`; FIFO path resolution, listener lifecycle, and normalized message delivery under the new package path.
- Create `orchestrator/internal/session/mkpipe/listener_test.go` - owned by `SessionMkpipe`; moved mkpipe regression tests.

- Modify `orchestrator/cmd/tmux_codex/main.go` - owned by `CodexCommandAdapter`; replace direct `internal/agent` orchestration with thin `orchestrator/session` calls while preserving flags, wording, and exit codes.
- Modify `orchestrator/cmd/tmux_codex/main_test.go` - owned by `CodexCommandAdapter`; replace fake agent/session seams with a fake `session` handle and keep the command-surface assertions.
- Modify `orchestrator/cmd/tmux_claude/main.go` - owned by `ClaudeCommandAdapter`; apply the same session-based adapter pattern for Claude.
- Modify `orchestrator/cmd/tmux_claude/main_test.go` - owned by `ClaudeCommandAdapter`; mirror the Codex command-test migration with Claude-specific expectations.

- Delete `orchestrator/internal/agent/agent.go` - owned by `LegacySessionCleanup`; remove the superseded launcher-facing agent interface once `orchestrator/session` is the public lifecycle boundary.
- Delete `orchestrator/internal/agent/agent_test.go` - owned by `LegacySessionCleanup`; retire tests that describe the old agent surface.
- Delete `orchestrator/internal/agent/errors.go` - owned by `LegacySessionCleanup`; replace with `orchestrator/session/errors.go`.
- Delete `orchestrator/internal/agent/codex.go` - owned by `LegacySessionCleanup`; replace with backend-specific implementation in `orchestrator/internal/session/backend/codex.go`.
- Delete `orchestrator/internal/agent/claude.go` - owned by `LegacySessionCleanup`; replace with backend-specific implementation in `orchestrator/internal/session/backend/claude.go`.
- Delete `orchestrator/internal/agent/env/env.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/env/env.go`.
- Delete `orchestrator/internal/agent/env/env_test.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/env/env_test.go`.
- Delete `orchestrator/internal/agent/tmux/errors.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/tmux/errors.go`.
- Delete `orchestrator/internal/agent/tmux/interfaces.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/tmux/interfaces.go`.
- Delete `orchestrator/internal/agent/tmux/tmux.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/tmux/tmux.go`.
- Delete `orchestrator/internal/agent/tmux/tmux_test.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/tmux/tmux_test.go`.
- Delete `orchestrator/internal/dirlock/lock.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/dirlock/lock.go`.
- Delete `orchestrator/internal/dirlock/lock_test.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/dirlock/lock_test.go`.
- Delete `orchestrator/internal/mkpipe/listener.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/mkpipe/listener.go`.
- Delete `orchestrator/internal/mkpipe/listener_test.go` - owned by `LegacySessionCleanup`; replaced by `orchestrator/internal/session/mkpipe/listener_test.go`.

## Implementation File Allowlist

**Primary implementation files:**
- `orchestrator/session/session.go`
- `orchestrator/session/errors.go`
- `orchestrator/session/session_test.go`
- `orchestrator/internal/session/backend/backend.go`
- `orchestrator/internal/session/backend/codex.go`
- `orchestrator/internal/session/backend/claude.go`
- `orchestrator/internal/session/backend/backend_test.go`
- `orchestrator/internal/session/tmux/errors.go`
- `orchestrator/internal/session/tmux/interfaces.go`
- `orchestrator/internal/session/tmux/tmux.go`
- `orchestrator/internal/session/tmux/tmux_test.go`
- `orchestrator/internal/session/env/env.go`
- `orchestrator/internal/session/env/env_test.go`
- `orchestrator/internal/session/dirlock/lock.go`
- `orchestrator/internal/session/dirlock/lock_test.go`
- `orchestrator/internal/session/mkpipe/listener.go`
- `orchestrator/internal/session/mkpipe/listener_test.go`
- `orchestrator/cmd/tmux_codex/main.go`
- `orchestrator/cmd/tmux_codex/main_test.go`
- `orchestrator/cmd/tmux_claude/main.go`
- `orchestrator/cmd/tmux_claude/main_test.go`

**Primary delete targets:**
- `orchestrator/internal/agent/agent.go`
- `orchestrator/internal/agent/agent_test.go`
- `orchestrator/internal/agent/errors.go`
- `orchestrator/internal/agent/codex.go`
- `orchestrator/internal/agent/claude.go`
- `orchestrator/internal/agent/env/env.go`
- `orchestrator/internal/agent/env/env_test.go`
- `orchestrator/internal/agent/tmux/errors.go`
- `orchestrator/internal/agent/tmux/interfaces.go`
- `orchestrator/internal/agent/tmux/tmux.go`
- `orchestrator/internal/agent/tmux/tmux_test.go`
- `orchestrator/internal/dirlock/lock.go`
- `orchestrator/internal/dirlock/lock_test.go`
- `orchestrator/internal/mkpipe/listener.go`
- `orchestrator/internal/mkpipe/listener_test.go`

**Incidental-only files:**
- None expected. Do not widen into `Makefile`, `orchestrator/go.mod`, `bin/tmux_codex`, or `bin/tmux_claude` unless the plan itself proves incomplete.

## Task List

All commands below assume the working directory is `orchestrator/`.

Baseline before feature work: `go test ./...` passes as of 2026-05-03.

### Task 1: SessionTmuxAdapter

**Files:**
- Create: `orchestrator/internal/session/tmux/errors.go`
- Create: `orchestrator/internal/session/tmux/interfaces.go`
- Create: `orchestrator/internal/session/tmux/tmux.go`
- Create: `orchestrator/internal/session/tmux/tmux_test.go`
- Test: `orchestrator/internal/session/tmux/tmux_test.go`

**Covers:** `R5`, `E6`
**Owner:** `SessionTmuxAdapter`
**Why:** The new session package depends on a stable tmux adapter at the new path. Migrate that dependency first so later tasks can import the new package without dragging the old `internal/agent/tmux` path forward.

- [ ] **Step 1: Port the current tmux regression tests to the new package path**

```go
func TestAttachFallsBackToProcessStreams(t *testing.T) {
	session := &TmuxSession{name: "demo"}
	// assert nil stdin/stdout/stderr still map to os.Stdin/os.Stdout/os.Stderr
}
```

- [ ] **Step 2: Run the new package tests to confirm the package does not exist yet**

Run: `go test ./internal/session/tmux -v`
Expected: FAIL with missing package or undefined symbols under `internal/session/tmux`

- [ ] **Step 3: Move `errors.go`, `interfaces.go`, and `tmux.go` into `internal/session/tmux` without changing behavior**

```go
package tmux

type TmuxSessionLike interface {
	Name() string
	Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	Close() error
	NewPane() (TmuxPaneLike, error)
}
```

- [ ] **Step 4: Run the moved tmux regression suite**

Run: `go test ./internal/session/tmux -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/tmux
git commit -m "refactor: move tmux adapter under session"
```

### Task 2: SessionEnvBuilder

**Files:**
- Create: `orchestrator/internal/session/env/env.go`
- Create: `orchestrator/internal/session/env/env_test.go`
- Test: `orchestrator/internal/session/env/env_test.go`

**Covers:** `R6`
**Owner:** `SessionEnvBuilder`
**Why:** The session lifecycle should depend on the same shell-command contract that commands already use today, but from its new package location.

- [ ] **Step 1: Port the existing env regression tests to the new package path**

```go
func TestBuildLaunchCommandPrependsAgentBinAndSourcesAgentRC(t *testing.T) {
	got, err := BuildLaunchCommand("codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `stty -echo`) {
		t.Fatalf("missing echo disable: %q", got)
	}
}
```

- [ ] **Step 2: Run the env package tests to verify the new path is still unimplemented**

Run: `go test ./internal/session/env -v`
Expected: FAIL with missing package or undefined symbols

- [ ] **Step 3: Move the current env implementation into `internal/session/env`**

```go
func BuildLaunchCommand(command string, args ...string) (string, error) {
	return "bash -lc " + shellQuote(buildShellScript(agentBin, command, args...)), nil
}
```

- [ ] **Step 4: Run the moved env regression suite**

Run: `go test ./internal/session/env -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/env
git commit -m "refactor: move launcher env builder under session"
```

### Task 3: SessionDirLockPolicy

**Files:**
- Create: `orchestrator/internal/session/dirlock/lock.go`
- Create: `orchestrator/internal/session/dirlock/lock_test.go`
- Test: `orchestrator/internal/session/dirlock/lock_test.go`

**Covers:** `R7`
**Owner:** `SessionDirLockPolicy`
**Why:** The lock implementation moves under the session subtree, but its stale-lock and PID-file semantics should not change during this refactor.

- [ ] **Step 1: Port the existing dirlock regression tests to the new package path**

```go
func TestAcquireRemovesStaleLock(t *testing.T) {
	lock := New(filepath.Join(t.TempDir(), DirName))
	if err := lock.Acquire(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the new dirlock package tests to confirm the path is still missing**

Run: `go test ./internal/session/dirlock -v`
Expected: FAIL with missing package or undefined symbols

- [ ] **Step 3: Move the current dirlock implementation into `internal/session/dirlock`**

```go
func NewInCurrentDirectory() (*Lock, error) {
	workingDir, err := os.Getwd()
	// unchanged stale-lock behavior
}
```

- [ ] **Step 4: Run the moved dirlock regression suite**

Run: `go test ./internal/session/dirlock -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/dirlock
git commit -m "refactor: move dirlock under session"
```

### Task 4: SessionMkpipe

**Files:**
- Create: `orchestrator/internal/session/mkpipe/listener.go`
- Create: `orchestrator/internal/session/mkpipe/listener_test.go`
- Test: `orchestrator/internal/session/mkpipe/listener_test.go`

**Covers:** `R8`
**Owner:** `SessionMkpipe`
**Why:** Session attach flow will own mkpipe orchestration, but the FIFO semantics themselves should move unchanged first so session tests can build on a stable helper.

- [ ] **Step 1: Port the existing mkpipe listener regression tests to the new package path**

```go
func TestListenerNormalizesMessagesPreservesInternalNewlinesAndSuppressesWhitespace(t *testing.T) {
	listener, err := Start(Config{WorkingDir: t.TempDir(), SessionName: "codex", DefaultBasename: "codex"})
	if err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the new mkpipe package tests to confirm the path is still unimplemented**

Run: `go test ./internal/session/mkpipe -v`
Expected: FAIL with missing package or undefined symbols

- [ ] **Step 3: Move the current mkpipe implementation into `internal/session/mkpipe`**

```go
type Listener interface {
	Path() string
	Messages() <-chan string
	Errors() <-chan error
	Close() error
}
```

- [ ] **Step 4: Run the moved mkpipe regression suite**

Run: `go test ./internal/session/mkpipe -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/mkpipe
git commit -m "refactor: move mkpipe under session"
```

### Task 5: SessionBackendPackage

**Files:**
- Create: `orchestrator/internal/session/backend/backend.go`
- Create: `orchestrator/internal/session/backend/codex.go`
- Create: `orchestrator/internal/session/backend/claude.go`
- Create: `orchestrator/internal/session/backend/backend_test.go`
- Test: `orchestrator/internal/session/backend/backend_test.go`

**Covers:** `R2`
**Owner:** `SessionBackendPackage`
**Why:** Backend-specific behavior must stay explicit and readable, but it should no longer carry the lifecycle orchestration. This task isolates only the parts that differ between Codex and Claude.

- [ ] **Step 1: Write failing backend tests for defaults and readiness matchers**

```go
func TestCodexDefaultsAndReadyMatcher(t *testing.T) {
	var b Backend = Codex{}
	if b.DefaultSessionName() != "codex" {
		t.Fatalf("default session = %q", b.DefaultSessionName())
	}
	if !b.Ready("OpenAI Codex\n› ") {
		t.Fatal("expected Codex prompt to be ready")
	}
}
```

- [ ] **Step 2: Run the backend package tests to confirm the package is not implemented yet**

Run: `go test ./internal/session/backend -v`
Expected: FAIL with missing package or undefined symbols

- [ ] **Step 3: Implement the backend interface plus explicit Codex and Claude structs**

```go
type Backend interface {
	DefaultSessionName() string
	Command() (string, []string)
	Ready(capture string) bool
}

type Codex struct{}
type Claude struct{}
```

- [ ] **Step 4: Run the backend regression suite**

Run: `go test ./internal/session/backend -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/backend
git commit -m "refactor: add session backend implementations"
```

### Task 6: SessionLifecycle Detached Flow

**Files:**
- Create: `orchestrator/session/session.go`
- Create: `orchestrator/session/errors.go`
- Create: `orchestrator/session/session_test.go`
- Test: `orchestrator/session/session_test.go`

**Covers:** `R1`, `R7`, `R9`, `R12`, `E1`, `E2`, `E3`, `E4`, `E7`, `E9`
**Owner:** `SessionLifecycle`
**Why:** This establishes the new public session surface, the typed error boundary, the built-in lock policy hook, and the detached lifecycle path that later attach logic can reuse, including the readiness-stabilization rules that keep startup deterministic.

- [ ] **Step 1: Write failing session tests for constructors, detached start, typed errors, and startup cleanup**

```go
func TestNewCodexUsesBackendDefaultSessionName(t *testing.T) {}

func TestStartRejectsMkpipeEnabledConfig(t *testing.T) {}

func TestStartWaitsForQuietReadyState(t *testing.T) {}

func TestStartReturnsStartupErrorWhenReadyTimeoutExpires(t *testing.T) {}

func TestStartupFailureClosesSessionReleasesLockAndTransitionsClosed(t *testing.T) {}

func TestSendPromptBeforeStartReturnsSessionError(t *testing.T) {}
```

- [ ] **Step 2: Run the targeted session tests to verify the package is not implemented yet**

Run: `go test ./session -run 'Test(NewCodexUsesBackendDefaultSessionName|StartRejectsMkpipeEnabledConfig|StartWaitsForQuietReadyState|StartReturnsStartupErrorWhenReadyTimeoutExpires|StartupFailureClosesSessionReleasesLockAndTransitionsClosed|SendPromptBeforeStartReturnsSessionError)' -v`
Expected: FAIL with missing package or undefined symbols under `session`

- [ ] **Step 3: Implement `Config`, `MkpipeConfig`, `LockPolicy`, `CurrentDirectoryLockPolicy`, `Session`, `Error`, and detached lifecycle methods, including internal ready-timeout / quiet-period / poll-interval defaults and test seams for capture timing**

```go
type Session struct {
	backend    backend.Backend
	session    tmux.TmuxSessionLike
	pane       tmux.TmuxPaneLike
	sessionName string
	lockPolicy LockPolicy
	mkpipe     *MkpipeConfig
	state      state
	deps       deps // tmux/env/time/signal seams for tests
	sendMu     sync.Mutex
}
```

- [ ] **Step 4: Run the detached-flow session tests**

Run: `go test ./session -run 'Test(NewCodexUsesBackendDefaultSessionName|StartRejectsMkpipeEnabledConfig|StartWaitsForQuietReadyState|StartReturnsStartupErrorWhenReadyTimeoutExpires|StartLaunchesAndWaitsReady|StartupFailureClosesSessionReleasesLockAndTransitionsClosed|SendPromptBeforeStartReturnsSessionError|CloseOnNewAndClosedIsNoOp)' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add session
git commit -m "refactor: add public session detached lifecycle"
```

### Task 7: SessionLifecycle Attach Flow

**Files:**
- Modify: `orchestrator/session/session.go`
- Modify: `orchestrator/session/errors.go`
- Modify: `orchestrator/session/session_test.go`
- Test: `orchestrator/session/session_test.go`

**Covers:** `R10`, `R11`, `E5`, `E8`, `E10`
**Owner:** `SessionLifecycle`
**Why:** Attached launch, mkpipe timing, reattach semantics, reattach hook behavior, and attach failure or interruption cleanup are the parts most likely to regress if they are left implicit. This task makes that sequencing explicit in code and tests.

- [ ] **Step 1: Write failing attach-flow tests for new-handle attach, reattach, hook metadata, and attach-error cleanup**

```go
func TestAttachNewHandleWaitsForQuietReadyStateBeforeMkpipe(t *testing.T) {}

func TestAttachNewHandleStartsMkpipeBeforeAttachAndInvokesBeforeAttach(t *testing.T) {}

func TestAttachStartedHandleDoesNotRestartOrReacquireLock(t *testing.T) {}

func TestReattachStillInvokesBeforeAttachWithEmptyMkpipePath(t *testing.T) {}

func TestAttachFailureKeepsStartedStateAndCleansTransientResources(t *testing.T) {}

func TestAttachInterruptCleansMkpipeAndReleasesLock(t *testing.T) {}

func TestAttachReturnDoesNotCloseBackendSession(t *testing.T) {}
```

- [ ] **Step 2: Run the targeted attach-flow tests to verify they fail against the detached-only implementation**

Run: `go test ./session -run 'TestAttachNewHandleWaitsForQuietReadyStateBeforeMkpipe|TestAttachNewHandleStartsMkpipeBeforeAttachAndInvokesBeforeAttach|TestAttachStartedHandleDoesNotRestartOrReacquireLock|TestReattachStillInvokesBeforeAttachWithEmptyMkpipePath|TestAttachFailureKeepsStartedStateAndCleansTransientResources|TestAttachInterruptCleansMkpipeAndReleasesLock|TestAttachReturnDoesNotCloseBackendSession' -v`
Expected: FAIL with missing `Attach` behavior or incorrect state/cleanup behavior

- [ ] **Step 3: Implement `AttachOptions`, `AttachInfo`, attach startup reuse, mkpipe orchestration, `BeforeAttach`, signal-driven interrupt cleanup, and reattach semantics**

```go
func (s *Session) Attach(opts AttachOptions) error {
	switch s.state {
	case stateNew:
		// startup, optional mkpipe, BeforeAttach, interrupt-safe cleanup, tmux attach
	case stateStarted:
		// reattach only; no startup, no mkpipe, no lock reacquire
	default:
		return newSessionError(ErrorKindState, s.sessionName, "", errClosed)
	}
}
```

- [ ] **Step 4: Run the attach-flow session tests**

Run: `go test ./session -run 'TestAttachNewHandleWaitsForQuietReadyStateBeforeMkpipe|TestAttachNewHandleStartsMkpipeBeforeAttachAndInvokesBeforeAttach|TestAttachStartedHandleDoesNotRestartOrReacquireLock|TestReattachStillInvokesBeforeAttachWithEmptyMkpipePath|TestAttachFailureKeepsStartedStateAndCleansTransientResources|TestAttachInterruptCleansMkpipeAndReleasesLock|TestAttachReturnDoesNotCloseBackendSession|TestBeforeAttachEmptyMkpipePathWhenDisabled' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add session
git commit -m "refactor: implement session attach lifecycle"
```

### Task 8: CodexCommandAdapter

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/main.go`
- Modify: `orchestrator/cmd/tmux_codex/main_test.go`
- Test: `orchestrator/cmd/tmux_codex/main_test.go`

**Covers:** `R3`
**Owner:** `CodexCommandAdapter`
**Why:** The command should become a thin session adapter without changing its user-facing contract. This task replaces direct agent/tmux/mkpipe orchestration with a fakeable `session` dependency.

- [ ] **Step 1: Rewrite Codex command tests around a fake session handle instead of a fake agent plus helper seams**

```go
type fakeCodexSession struct {
	name      string
	startErr  error
	attachErr error
	started   bool
	attached  bool
	before    session.AttachInfo
}
```

- [ ] **Step 2: Run the Codex command tests to verify they fail until `main.go` uses the new session package**

Run: `go test ./cmd/tmux_codex -v`
Expected: FAIL with outdated deps or missing `session`-based wiring

- [ ] **Step 3: Replace direct `internal/agent` orchestration with `session.NewCodex` plus `Start` / `Attach`**

```go
sess := deps.newSession(session.Config{
	SessionName: parsed.sessionName,
	LockPolicy:  session.CurrentDirectoryLockPolicy(),
	Mkpipe:      mkpipeConfig(parsed),
})
```

- [ ] **Step 4: Run the Codex command regression suite**

Run: `go test ./cmd/tmux_codex -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/tmux_codex/main.go cmd/tmux_codex/main_test.go
git commit -m "refactor: switch tmux_codex to session package"
```

### Task 9: ClaudeCommandAdapter

**Files:**
- Modify: `orchestrator/cmd/tmux_claude/main.go`
- Modify: `orchestrator/cmd/tmux_claude/main_test.go`
- Test: `orchestrator/cmd/tmux_claude/main_test.go`

**Covers:** `R4`
**Owner:** `ClaudeCommandAdapter`
**Why:** Claude needs the same thin-adapter treatment as Codex, but the tests must continue to pin Claude-specific defaults and wording.

- [ ] **Step 1: Rewrite Claude command tests around a fake session handle**

```go
type fakeClaudeSession struct {
	name      string
	startErr  error
	attachErr error
	started   bool
	attached  bool
}
```

- [ ] **Step 2: Run the Claude command tests to verify they fail until `main.go` switches to the session package**

Run: `go test ./cmd/tmux_claude -v`
Expected: FAIL with outdated deps or missing `session`-based wiring

- [ ] **Step 3: Replace direct `internal/agent` orchestration with `session.NewClaude` plus `Start` / `Attach`**

```go
sess := deps.newSession(session.Config{
	SessionName: parsed.sessionName,
	LockPolicy:  session.CurrentDirectoryLockPolicy(),
	Mkpipe:      mkpipeConfig(parsed),
})
```

- [ ] **Step 4: Run the Claude command regression suite**

Run: `go test ./cmd/tmux_claude -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/tmux_claude/main.go cmd/tmux_claude/main_test.go
git commit -m "refactor: switch tmux_claude to session package"
```

### Task 10: LegacySessionCleanup

**Files:**
- Delete: `orchestrator/internal/agent/agent.go`
- Delete: `orchestrator/internal/agent/agent_test.go`
- Delete: `orchestrator/internal/agent/errors.go`
- Delete: `orchestrator/internal/agent/codex.go`
- Delete: `orchestrator/internal/agent/claude.go`
- Delete: `orchestrator/internal/agent/env/env.go`
- Delete: `orchestrator/internal/agent/env/env_test.go`
- Delete: `orchestrator/internal/agent/tmux/errors.go`
- Delete: `orchestrator/internal/agent/tmux/interfaces.go`
- Delete: `orchestrator/internal/agent/tmux/tmux.go`
- Delete: `orchestrator/internal/agent/tmux/tmux_test.go`
- Delete: `orchestrator/internal/dirlock/lock.go`
- Delete: `orchestrator/internal/dirlock/lock_test.go`
- Delete: `orchestrator/internal/mkpipe/listener.go`
- Delete: `orchestrator/internal/mkpipe/listener_test.go`
- Test: full suite under `orchestrator/`

**Covers:** `R13`
**Owner:** `LegacySessionCleanup`
**Why:** The refactor is incomplete until the old `internal/agent`-shaped architecture is actually gone and the repository no longer imports or tests the superseded helper paths.

- [ ] **Step 1: Run the full suite before deletion to confirm the replacement code is green**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Delete the retired `internal/agent` tree and the old top-level helper packages**

```text
Remove the files listed above after all imports point at `session` / `internal/session/*`.
```

- [ ] **Step 3: Run the full suite again after deletion**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Verify no Go source still references the retired package paths**

Run: `rg -n 'internal/agent|internal/dirlock|internal/mkpipe|internal/agent/env|internal/agent/tmux' .`
Expected: no matches in Go source files under `orchestrator/`

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: remove legacy agent session packages"
```
