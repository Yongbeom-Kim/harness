# Session Package Refactor Implementation Decision Log

Date: 2026-05-03
Topic: implementation architecture for the tmux-backed session package refactor

Inputs reviewed:

- `docs/development/2026-05-03-session-package-refactor/design-document.md`
- `docs/development/2026-05-03-session-package-refactor/decisions-raw.md`
- current `orchestrator/cmd/*`, `orchestrator/internal/agent/*`, `orchestrator/internal/{mkpipe,dirlock}/*`, and related tests

## Round 1

### 1. What concrete public handle should `orchestrator/session` expose from `session.NewCodex` and `session.NewClaude`?

Options presented:

- A: Return one exported `*session.Session` wrapper that owns the backend descriptor plus a private runtime instance and exposes `SessionName`, `Start`, `Attach`, `SendPrompt`, `Capture`, and `Close`; keep backend-specific types internal.
- B: Return a public `session.Handle` interface and keep the concrete type hidden.
- C: Return backend-specific public types like `*session.CodexSession` and `*session.ClaudeSession`.
- D: Re-export an `internal/session/runtime` type directly.

User answer:

> 1A

Decision:

- `orchestrator/session` should return one exported `*session.Session` handle.
- Backend-specific behavior remains internal behind explicit public constructors.

### 2. How should the new runtime package be made unit-testable?

Options presented:

- A: Give `internal/session/runtime` an unexported dependency bundle for tmux create/open, env build, mkpipe start, lock acquisition, capture timing, and cleanup hooks; production constructors wire real implementations while tests inject fakes.
- B: Keep package-level function vars, similar to the current `cmd/*` dependency seams.
- C: Put test hooks directly on public `session.Config`.
- D: Avoid unit seams and use mostly real tmux integration tests.

User answer:

> 2A

Decision:

- `internal/session/runtime` should own an internal dependency bundle for tests.
- Production wiring stays in constructors; public config remains product-facing rather than test-facing.

### 3. Once lifecycle moves into `session`, how much code should remain duplicated in `cmd/tmux_codex` and `cmd/tmux_claude`?

Options presented:

- A: Keep both `main.go` files as separate thin adapters with their own flag parsing, labels, and banner wording; do not add a new shared CLI helper package.
- B: Add one shared internal CLI helper for the common `run` flow, but keep parsing local.
- C: Share both parsing and run logic through a common command helper package.
- D: Replace the two binaries with one shared binary.

User answer:

> 3A

Decision:

- `cmd/tmux_codex` and `cmd/tmux_claude` remain separate thin adapters.
- No new shared CLI helper package is introduced in this refactor.

### 4. How should we phase the removal of `internal/agent`?

Options presented:

- A: Add `orchestrator/session` and `internal/session/*` first, switch `cmd/*` and new tests to the new handle, then delete `internal/agent` and its old imports in a final cleanup task.
- B: Rename or move `internal/agent/*` in place immediately and fix compile breaks as they surface.
- C: Keep `internal/agent` as a permanent compatibility wrapper over the new runtime.
- D: Leave `internal/agent` in place and add only the public `session` package.

User answer:

> 4A

Decision:

- The migration should be staged: add the new session packages first, switch command callers and tests, then delete `internal/agent` in final cleanup.

### 5. Where should the behavioral regression tests primarily live for this refactor?

Options presented:

- A: Put lifecycle, state, mkpipe, lock, and failure-sequencing tests in `internal/session/runtime`; keep `orchestrator/session` tests focused on constructor and public-surface coverage; leave `cmd/*` tests focused on flags, banner text, and exit-code mapping.
- B: Push most behavior tests up to `cmd/*` and keep runtime lightly tested.
- C: Test almost everything through the public `session` package and remove most command tests.
- D: Rely mainly on manual tmux verification once the new package compiles.

User answer:

> 5A

Decision:

- Runtime behavior belongs primarily in `internal/session/runtime` tests.
- Public `session` tests stay thin, and `cmd/*` tests remain focused on command-surface behavior.

## Round 2

### 6. How should the session state machine and lifecycle error handling be organized inside `internal/session/runtime`?

Options presented:

- A: Keep one runtime-owned `Session` struct in `internal/session/runtime/session.go`, put state-transition and lifecycle wrapper errors in `internal/session/runtime/errors.go`, and keep state/failure tests in `internal/session/runtime/session_test.go`.
- B: Keep all runtime code, state, and errors in one large `runtime.go`.
- C: Push state errors up into the public `orchestrator/session` package.
- D: Leave state/error handling spread across `cmd/*` and helper packages.

User answer:

> 6A

Decision:

- Initial direction was to centralize the state machine and lifecycle errors in one dedicated implementation package.
- This decision was later superseded by Round 3 when the user rejected the separate `runtime` package and moved lifecycle ownership into `orchestrator/session`.

### 7. How should the existing helper packages move under `internal/session/*`?

Options presented:

- A: Create real packages at `internal/session/{tmux,env,dirlock,mkpipe}` by moving/adapting the current implementations and their tests there, then delete the old `internal/agent/{tmux,env}` and old top-level `internal/{dirlock,mkpipe}` packages once callers switch.
- B: Keep the old package paths and add thin wrappers under `internal/session/*`.
- C: Move only `tmux` and `env`; leave `dirlock` and `mkpipe` where they are.
- D: Keep all helpers where they are and only add `runtime`/`backend`.

User answer:

> 7A

Decision:

- Move helper implementations and tests into real `internal/session/{tmux,env,dirlock,mkpipe}` packages.
- Delete the old helper package paths after callers and tests migrate.

## Round 3

### 8. What should the internal backend package expose to the runtime?

Clarification provided:

- `backend.Descriptor` meant a small internal value describing what differs between Codex and Claude, such as launch command, default session name, readiness matcher, and any labels needed by command hooks.

Options presented:

- A: A small internal backend definition struct, named `backend.Descriptor` or similar, plus `backend.Codex()` and `backend.Claude()` helpers that supply default session name, launch command plus args, readiness matcher, and any command-hook labels.
- B: Keep the old `CodexAgent` and `ClaudeAgent` concrete types as the runtime’s backend layer.
- C: Make backend descriptors public and let callers assemble them.
- D: Put backend differences in config files instead of Go types.

User answer:

> The difference should be implemented differently for each of Codex and Claude. We should have a backend interface and then a Claude struct and a Codex struct, and each of these should implement its own behavior. Duplicating some code is acceptable here to keep the code easier to read and maintain.

Decision:

- Do not use a data-driven backend descriptor abstraction.
- Model backend-specific behavior through a backend interface with separate Codex and Claude implementations.
- Accept some duplication in backend implementations to preserve readability.

### 9. Given the reviewed design’s `internal/session/runtime` package, which layer should enforce library-level validation and state transitions such as “mkpipe is attach-only,” “Start cannot be called twice,” and “closed handles cannot restart”?

Options presented:

- A: `internal/session/runtime` owns all lifecycle/state enforcement; the public `session` package only applies constructor defaults and forwards calls.
- B: The public `session` package enforces state and runtime only performs IO.
- C: `cmd/*` should re-check these rules on every call.
- D: Helper packages like `tmux` and `mkpipe` should enforce the whole lifecycle contract.

User answer:

> If the only purpose of `runtime` is to do things like create a session handle, we should not have the `runtime` package. This logic should sit directly in `session`; the lifecycle is shared there and can depend on backend interfaces.

Decision:

- Remove the separate `internal/session/runtime` package from the implementation model.
- Put lifecycle/state enforcement directly in the public `orchestrator/session` package.
- Helper packages remain below `internal/session/*`, but session orchestration and state ownership live in `orchestrator/session`.

### 10. How should the stable command banners integrate with the new attach flow?

Clarification provided:

- `BeforeAttach` means a callback in `AttachOptions` that runs immediately before the blocking `tmux attach-session` handoff and receives `AttachInfo{SessionName, MkpipePath}`.

Options presented:

- A: `cmd/tmux_codex` and `cmd/tmux_claude` pass `AttachOptions.BeforeAttach`, receive `AttachInfo{SessionName, MkpipePath}`, and print their existing user-facing lines there; the session library never prints operator banners.
- B: The runtime prints attach banners itself and commands only map exit codes.
- C: `Attach()` should return banner info after attach returns and commands print it then.
- D: Commands should keep computing mkpipe paths and reopening tmux sessions themselves before attach.

User answer:

> 10A

Decision:

- Commands keep ownership of operator-facing banners through `AttachOptions.BeforeAttach`.
- The session library provides attach metadata but does not print user-facing banners.

## Round 4

### 11. Where should the backend interface and the concrete Codex/Claude implementations live now that there is no separate `runtime` package?

Options presented:

- A: Keep them unexported inside `orchestrator/session`, with a private backend interface plus `backend_codex.go` and `backend_claude.go`; `NewCodex` and `NewClaude` choose the implementation.
- B: Put them in `orchestrator/internal/session/backend`, while `orchestrator/session` owns lifecycle and state.
- C: Export the backend interface from `orchestrator/session` so callers can provide custom backends.
- D: Keep the old `internal/agent/codex.go` and `internal/agent/claude.go` as the backend implementation layer.

User answer:

> 11B

Decision:

- Keep backend-specific implementations under `orchestrator/internal/session/backend`.
- `orchestrator/session` remains the lifecycle/state owner and consumes those internal backends.

### 12. With `runtime` removed, where should the main lifecycle and state tests live?

Options presented:

- A: Move them into `orchestrator/session/session_test.go` (and supporting test files in the same package), while helper-package tests stay with the helpers and `cmd/*` tests stay focused on flags, banners, and exit codes.
- B: Push most lifecycle testing up into `cmd/*`.
- C: Put the lifecycle tests in `internal/session/backend`.
- D: Rely mostly on manual tmux validation.

User answer:

> 12A

Decision:

- Lifecycle and state regression coverage should live primarily in `orchestrator/session` tests.
- Helper packages keep helper-specific tests, and `cmd/*` tests stay command-surface focused.

### 13. How should the `orchestrator/session` package itself be split across files?

Options presented:

- A: Use a small set of focused files: `session.go` for public types/constructors, `lifecycle.go` for `Start`/`Attach`/`Close`/`SendPrompt`/`Capture`, `errors.go` for session-owned lifecycle/state errors, plus separate backend files.
- B: Keep the whole package in one large `session.go`.
- C: Split each method into its own file.
- D: Keep no session-owned error file; helper packages should provide all errors.

User answer:

> Keep the lifecycle in `session.go`; keep session-owned errors in `errors.go`; only split helper/backend code into separate files where it materially improves readability.

Decision:

- `orchestrator/session/session.go` should contain the main lifecycle/state implementation.
- `orchestrator/session/errors.go` should contain session-level lifecycle/state error types.
- Do not force an extra `lifecycle.go` split.

### 14. Should the new session layer keep a typed lifecycle error wrapper similar to the current `internal/agent.AgentError`?

Options presented:

- A: Yes. Add a `session`-owned typed error for lifecycle/state/capture/close failures so callers and tests have one stable session-level error boundary after `internal/agent` is removed.
- B: No. Return only plain wrapped `error` values with no typed session error.
- C: Reuse `internal/agent.AgentError` even after the refactor.
- D: Only type state-transition errors; startup/capture/close should stay plain.

User answer:

> 14A

Decision:

- Add a `session`-owned typed lifecycle error wrapper in `orchestrator/session/errors.go`.
- Use that as the stable error boundary after `internal/agent` is removed.
