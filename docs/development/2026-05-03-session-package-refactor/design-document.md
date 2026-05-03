# Session Package Refactor Design

**Date:** 2026-05-03
**Status:** Ready for implementation planning
**Feature Area:** `orchestrator/session`, `orchestrator/internal/session/*`, `tmux_codex`, `tmux_claude`

## Summary

Refactor the current tmux-backed launcher implementation around a reusable session abstraction.

The new design introduces:

- a public `orchestrator/session` package that exposes stable constructors and a session handle
- a new `orchestrator/internal/session/*` package tree that owns the tmux-backed lifecycle implementation
- thin `tmux_codex` and `tmux_claude` entrypoints that only parse flags, construct a session, choose detached vs attached flow, and preserve current output and exit-code behavior

The intent is to eliminate repeated lifecycle orchestration that is currently spread across `cmd/tmux_codex`, `cmd/tmux_claude`, and the backend-specific `internal/agent` code, while preserving the current operator-facing command surface.

## Problem

The current implementation has two layers of repeated logic:

- `cmd/tmux_codex` and `cmd/tmux_claude` duplicate launch orchestration, attach flow, mkpipe wiring, and cleanup behavior
- `internal/agent/CodexAgent` and `internal/agent/ClaudeAgent` duplicate nearly the same tmux-backed runtime lifecycle, differing mainly in backend command, readiness matcher, default session name, and user-facing labels

That duplication was still manageable when the product surface was only “start Codex” and “start Claude.” It becomes a maintenance problem once the lifecycle grows richer:

- attach-only mkpipe prompt injection now exists in both launcher binaries
- lock acquisition and release policy matters for detached vs attached runs
- future entrypoints should be able to create a session and reuse the same lifecycle block instead of re-implementing tmux startup and cleanup again

The user goal is to turn the repeated tmux session + prompt ingress + backend readiness behavior into a reusable block that different entrypoints can consume directly.

## Goals

- Create a reusable public session API for tmux-backed coding-agent sessions.
- Centralize lifecycle orchestration in one shared runtime instead of duplicating it per backend.
- Preserve `tmux_codex` and `tmux_claude` as the stable operator-facing commands.
- Keep Codex-vs-Claude behavior data-driven and internal.
- Move session-specific helper packages under a coherent `internal/session/*` tree.
- Make mkpipe a session-owned optional feature instead of command-owned wiring.
- Keep the refactor focused on the current launcher use case rather than expanding into a broader workflow engine.

## Non-Goals

- No new multi-agent or workflow orchestration surface in this refactor.
- No generic replacement for `tmux_codex` and `tmux_claude`.
- No session reuse or reopen behavior when a requested tmux session name already exists.
- No startup/initial prompt in session configuration.
- No detached mkpipe listener or supervisor process.
- No public backend descriptor API that callers assemble manually.
- No automatic session shutdown when attach returns.

## Stable Product Contract

### CLI surface remains the same

The supported operator-facing commands remain:

- `tmux_codex`
- `tmux_claude`

This refactor does not introduce a new user-facing command.

### Existing operator behavior remains the contract

The commands continue to own:

- CLI flag parsing and usage errors
- stdout/stderr wording
- exit-code mapping
- backend-specific success labels in human-facing messages

The new session package is a reusable library surface. It does not replace the stable command contract; it powers that contract underneath.

For attach flows that need pre-attach operator output, the session API must provide a small pre-attach notification hook carrying attach metadata such as resolved session name and resolved mkpipe path. This keeps the commands in control of their current output wording while still letting the session layer own mkpipe startup timing.

### Mkpipe behavior remains attach-only

Mkpipe remains an attach-only feature:

- command validation still rejects `--mkpipe` without `--attach`
- the session library also rejects detached-start attempts that enable mkpipe through configuration

This preserves the current deliberate product scope and prevents the library from silently downgrading attach-only behavior into a detached no-op.

## Public Package Contract

### Package location

The new public entrypoint is:

```text
orchestrator/session
```

This package is the only reusable library surface that `cmd/*` should consume directly.

### Backend-specific constructors

The public package exposes explicit constructors:

```go
session.NewCodex(config)
session.NewClaude(config)
```

The constructors return the same session-handle type, but each constructor binds the handle to an internal backend descriptor.

This keeps backend choice explicit for callers without exposing backend internals as public API.

### Public config

The exported config is intentionally small and lifecycle-oriented. At minimum it includes:

```go
type Config struct {
    SessionName string
    Mkpipe      *MkpipeConfig
    LockPolicy  LockPolicy
}

type MkpipeConfig struct {
    Path string
}

type Lock interface {
    Acquire() error
    Release() error
}

type LockPolicy func() (Lock, error)
```

- `SessionName`
  - optional override
  - defaults come from the chosen backend constructor when omitted
- `Mkpipe`
  - optional attach-only mkpipe configuration
  - `Mkpipe.Path` is an optional custom path override
- `LockPolicy`
  - optional lock factory hook, invoked at the start of `Start()` or attached-launch `Attach()`
  - returns a fresh per-lifecycle lock instance with `Acquire()` and `Release()`
  - v1 exports `session.CurrentDirectoryLockPolicy()` for the current-working-directory behavior used today

The config does not include an initial prompt.

### Public session handle

The public handle exposes the current reusable session control surface:

- `SessionName() string`
- `Start() error`
- `Attach(opts AttachOptions) error`
- `SendPrompt(prompt string) error`
- `Capture() (string, error)`
- `Close() error`

`Capture()` is part of the public surface now rather than an internal-only diagnostic because the current code already treats pane capture as a first-class capability, and future orchestration and diagnostics will depend on it.

`Attach()` uses this minimal options shape:

```go
type AttachOptions struct {
    Stdin        io.Reader
    Stdout       io.Writer
    Stderr       io.Writer
    BeforeAttach func(AttachInfo)
}

type AttachInfo struct {
    SessionName string
    MkpipePath  string // empty when mkpipe is disabled for this attach call
}
```

Nil attach streams preserve the current tmux-layer fallback to `os.Stdin`, `os.Stdout`, and `os.Stderr`.

`BeforeAttach`, when present, fires immediately before the blocking `tmux attach-session` handoff. It receives the resolved session name and resolved absolute mkpipe path so the stable commands can preserve their current banner behavior without reclaiming lifecycle ownership from the session layer.

## Session Method Semantics

### `Start()`

`Start()` means:

- acquire the configured lock policy if one exists
- create a new tmux session
- launch the backend CLI through the shared launch-environment builder
- wait until the backend is ready using the chosen backend descriptor
- release transient startup resources, including the optional lock
- leave the tmux-backed backend session running

`Start()` is for detached launch behavior.

`Start()` rejects mkpipe-enabled config with a clear error, because mkpipe is attach-only by contract.

### `Attach(opts)`

`Attach()` has two valid modes:

1. **Attach on a brand-new session handle**
   - this is the attached-launch path
   - `Attach()` performs startup first, then optional mkpipe setup, then tmux attach handoff
   - immediately before handoff, `Attach()` can emit attach metadata through the optional pre-attach hook
   - if a lock policy is configured, it is acquired before startup and released when attach returns

2. **Attach on an already-started session handle**
   - this reattaches to the session started earlier by `Start()`
   - it does not re-run startup
   - it does not retroactively enable mkpipe

If `BeforeAttach` is provided, it runs in either mode immediately before handoff. `MkpipePath` is empty unless that specific `Attach()` call created a mkpipe listener.

This split keeps the public API small while still allowing the session layer to own the full attached lifecycle.

### `SendPrompt(prompt)`

`SendPrompt` sends one prompt into the live backend session through the shared tmux send path.

It is valid only after the session has started successfully.

### `Capture()`

`Capture()` returns the current pane capture from the live session.

It is valid only after the session has started successfully.

### `Close()`

`Close()` explicitly closes the tmux-backed backend session.

It is distinct from attach return. Attach return only ends the launcher-owned attach flow; it does not close the backend session automatically.

`Close()` is idempotent.

## Session State Contract

The session handle has a small logical state model:

- `new`
  - freshly constructed
  - no tmux session created yet
- `started`
  - tmux session exists
  - backend is ready
  - prompt send and capture are valid
- `closed`
  - no live session is owned by this handle anymore
  - reached after explicit close or after a failed startup attempt that cleaned up before the session became usable

The intended method behavior is:

- `SessionName()` from any state: valid
- `Start()` from `new`: valid
- `Start()` from `closed`: error
- `Attach()` from `new`: valid and performs attached startup flow
- `Attach()` from `started`: valid and reattaches without re-running startup
- `Start()` from `started`: error
- `Attach()` from `closed`: error
- `SendPrompt()` from `new`: error
- `SendPrompt()` from `started`: valid
- `SendPrompt()` from `closed`: error
- `Capture()` from `new`: error
- `Capture()` from `started`: valid
- `Capture()` from `closed`: error
- `Close()` from `new`: no-op
- `Close()` from `started`: valid
- `Close()` from `closed`: no-op

A failed startup or pre-handoff attach attempt on a `new` handle does not leave the handle reusable; after best-effort cleanup it transitions to `closed`, and callers construct a new handle to retry. An attach error that occurs after the session was already `started` leaves the handle in `started` so the caller can reattach or close explicitly.

This avoids ambiguous retry behavior while keeping the handle easy to reason about.

## Internal Package Layout

The refactor creates this internal structure:

```text
orchestrator/
  session/
    ...
  internal/session/
    backend/
    runtime/
    tmux/
    env/
    dirlock/
    mkpipe/
```

### `internal/session/runtime`

This is the shared lifecycle engine. It owns:

- session state tracking
- startup orchestration
- attach orchestration
- lock lifecycle management
- tmux session creation and opening
- launch command assembly through `env`
- readiness polling
- optional mkpipe listener startup, forwarding, and cleanup
- public-handle method behavior

This package is where the repeated orchestration from the current `cmd/*` and `internal/agent/*` code is consolidated.

### `internal/session/backend`

This package owns the backend descriptor model.

A backend descriptor contains only what differs between Codex and Claude:

- launch command
- readiness matcher
- default session name
- user-facing label data needed by higher layers

This package does not own lifecycle orchestration.

### `internal/session/tmux`

This package is the moved version of the current tmux adapter layer.

It continues to own:

- tmux session creation/opening
- pane creation
- attach behavior
- send-text behavior
- capture behavior
- tmux-specific error shaping

### `internal/session/env`

This package is the moved version of the current launch-environment builder.

It continues to own:

- sourcing `$HOME/.agentrc` if present
- prepending `~/.agent-bin` to `PATH`
- disabling terminal echo
- building the shell command used to start the backend CLI

### `internal/session/dirlock`

This package is the moved version of the current working-directory lock implementation.

It remains an internal implementation detail. The public `session` package exposes the usable lock-policy hook; callers do not import the internal package directly.

### `internal/session/mkpipe`

This package is the moved version of the current mkpipe listener implementation.

It continues to own:

- FIFO path resolution
- FIFO creation and validation
- EOF-delimited read behavior
- payload normalization
- listener lifecycle and cleanup

The difference is ownership: the session runtime, not the command package, now decides when mkpipe starts, forwards prompts, and stops.

### What happens to `internal/agent`

`internal/agent` is dissolved by this refactor.

Its responsibilities are split as follows:

- backend-specific launch/readiness details move to `internal/session/backend`
- shared lifecycle orchestration moves to `internal/session/runtime`
- public reusable control methods move onto the `orchestrator/session` handle

The refactor does not preserve `internal/agent` as a stable long-term package boundary.

## Runtime Lifecycle

### Detached start flow

The detached path is:

1. Construct `session.NewCodex(config)` or `session.NewClaude(config)`.
2. `Start()` acquires the optional lock policy.
3. Runtime asks the backend descriptor for defaults and readiness behavior.
4. Runtime creates a new tmux session.
5. Runtime creates/opens the startup pane.
6. Runtime builds the shared launch command through `internal/session/env`.
7. Runtime sends the launch command into the pane.
8. Runtime polls captures until the backend descriptor reports ready and the quiet-period condition is satisfied.
9. Runtime releases the lock.
10. Runtime leaves the session in `started`.

### Attached launch flow

The attached path on a new session handle is:

1. Construct `session.NewCodex(config)` or `session.NewClaude(config)`.
2. `Attach()` acquires the optional lock policy.
3. Runtime executes the same startup flow used by `Start()`.
4. If mkpipe is configured, runtime resolves path, creates the listener, and starts prompt-forwarding goroutines.
5. Runtime opens and validates the tmux attach target.
6. Runtime notifies the optional pre-attach hook with attach metadata such as session name and resolved mkpipe path.
7. Runtime hands control to `tmux attach-session`.
8. When attach returns, runtime cleans up transient resources such as mkpipe listener and held lock.
9. The backend tmux session remains alive.

### Reattach flow

The reattach path on an already-started session handle is:

1. Caller invokes `Attach()` after a previous successful `Start()`.
2. Runtime attaches to the existing tmux session.
3. No startup or readiness logic re-runs.
4. No mkpipe listener is created in this path.
5. No startup-scoped lock is reacquired in this path.

Mkpipe is intentionally not retrofitted after a detached start.

## Lock Policy Contract

### Lock policy role

Locking is optional and session-scoped.

The lock policy exists to preserve the current “one launcher command per working directory” behavior where desired, without forcing that policy on every future caller of the session library.

### Built-in lock policy

V1 provides one built-in policy equivalent to today’s current-working-directory lock.

Internally, that policy is implemented by `internal/session/dirlock`.

### Lock hold duration

The policy is held:

- from before startup begins until startup completes for detached launches
- from before startup begins until attach returns for attached launches

The policy is not held for the full lifetime of the tmux-backed session after the launcher process exits.

This preserves current operator intent while avoiding a lock that outlives the launcher call unnecessarily.

## Mkpipe Contract Inside Session

Mkpipe becomes a session feature rather than a command-owned orchestration branch.

### What the session layer owns

When mkpipe is configured on an attached run, the session runtime owns:

- listener creation after backend readiness
- prompt-forwarding goroutines
- error logging behavior during attach
- cleanup on attach return or interruption

### What commands still own

Commands still own:

- user-facing flag syntax and validation
- printing the pre-attach banner
- exit-code mapping

The session layer provides the lifecycle behavior; the CLI continues to decide how that behavior is surfaced to operators.

### Library-level mkpipe validation

Even though the CLI already validates `--mkpipe` with `--attach`, the library also enforces:

- mkpipe cannot be used with detached `Start()`
- mkpipe cannot be retrofitted on a session that was already started detached

This prevents future callers from accidentally violating the product contract when they use the library directly.

## Failure Semantics

### Existing tmux session name

If the requested tmux session name already exists, startup fails clearly and leaves the existing session untouched.

Automatic reopen, reuse, or replacement is explicitly out of scope for v1.

### Startup failures

For detached or attached startup, failures during these phases are treated as failed startup:

- lock acquisition
- tmux session creation
- pane acquisition
- launch-command assembly
- launch send
- readiness wait
- mkpipe setup before attach handoff
- attach-target open/validation before attach handoff

Best-effort cleanup closes the newly created tmux session when startup fails after session creation but before attach handoff.

### Attach-return behavior

When `Attach()` returns successfully:

- transient attach-owned resources are cleaned up
- the backend tmux session remains alive
- the session handle remains logically usable until explicitly closed

### Attach failure after handoff begins

If attach itself fails after the backend session is already ready and handoff has begun:

- transient attach-owned resources are still cleaned up
- the error is returned
- the backend session is not auto-closed

Failures opening or validating the attach target before the blocking handoff are startup failures, not attach-return failures.

This matches the design choice that attach is not the same thing as session ownership.

### Prompting and capture before startup

`SendPrompt()` and `Capture()` return clear errors when called before successful startup.

### Repeated lifecycle calls

Repeated or contradictory calls return clear errors instead of attempting clever recovery.

The design prefers explicit state and predictable failure over implicit restart or reuse behavior.

## Command Adapter Contract

After the refactor, the two stable commands become thin adapters.

### `tmux_codex`

Its shape becomes:

1. parse flags
2. build `session.Config`
3. create `session.NewCodex(config)`
4. if `--attach`, call `Attach(session.AttachOptions{...})` and use `BeforeAttach` to print the same banner contract
5. otherwise call `Start()`
6. print the same success banner and preserve the same exit-code behavior

### `tmux_claude`

The Claude command follows the same flow, differing only in:

- constructor choice
- backend defaults
- user-facing label strings already specific to Claude

This is the intended end state: command packages retain their stable operator contract but no longer own the session lifecycle implementation.

## Migration Outcome

At the end of this refactor:

- reusable session behavior lives behind `orchestrator/session`
- backend-specific differences are internal and data-driven
- lifecycle-specific helper packages live under `internal/session/*`
- `tmux_codex` and `tmux_claude` remain the stable binaries
- `internal/agent` is no longer the primary package model

That is the full product scope of this design.
