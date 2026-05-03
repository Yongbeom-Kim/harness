# Cursor Backend Design

**Date:** 2026-05-03
**Status:** Ready for implementation planning
**Feature Area:** `orchestrator/session`, `orchestrator/internal/session/backend`, `orchestrator/cmd/tmux_cursor`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, `orchestrator/cmd/tmux_claude/CONTRACT.md`, `Makefile`

## Summary

Add Cursor as the third supported tmux-backed coding-agent backend in the harness.

This design extends the current launcher/session architecture without reshaping it:

- add a new public constructor `session.NewCursor(config)`
- add a private `internal/session/backend.Cursor` descriptor
- add a new operator-facing launcher binary `tmux_cursor`
- extend the launcher product surface from two supported binaries to three
- keep the shared session lifecycle, lock policy, attach flow, mkpipe behavior, and launch-environment model unchanged

The Cursor backend launches the interactive CLI by sending bare `agent` through the existing shared launch shell. Readiness in v1 is intentionally minimal: a stable capture containing `Cursor Agent` is treated as ready unless the screen is a known login, trust, or setup interstitial.

## Problem

The current harness product surface supports only Codex and Claude launchers even though the session architecture is already built around backend-specific descriptors and thin command adapters.

The user wants the same persistent tmux-backed workflow for Cursor:

- start Cursor in a named tmux session
- optionally attach immediately
- optionally enable attach-time mkpipe prompt injection
- preserve the same command ergonomics and lifecycle model used by `tmux_codex` and `tmux_claude`

Without a designed Cursor contract, implementation would have to guess:

- whether Cursor should be a third dedicated launcher or part of a new generic launcher
- whether the public session package should grow a Cursor constructor
- which exact command should be launched inside tmux
- how Cursor readiness should be recognized
- whether build/setup/docs should change

This design answers those questions while keeping the feature bounded.

## Goals

- Add Cursor support within the existing session/launcher architecture.
- Keep dedicated operator-facing launcher binaries rather than introducing a generic launcher.
- Add a public `session.NewCursor(config)` constructor that matches the current Codex/Claude pattern.
- Launch Cursor via bare `agent` through the shared launch-environment builder.
- Keep `tmux_cursor` CLI behavior parallel to `tmux_codex` and `tmux_claude`.
- Reuse the existing session lock, attach, mkpipe, and cleanup semantics unchanged.
- Update build output and product-contract docs so Cursor is part of the supported launcher surface.

## Non-Goals

- No new generic `tmux_agent` or `tmux_session` command.
- No replacement of `session.NewCodex` / `session.NewClaude` with a generic public backend selector.
- No Cursor-specific startup flags in v1.
- No repo-managed Cursor wrapper binary or separate Cursor setup flow.
- No new runtime preflight that checks `command -v agent` before launch.
- No broader workflow-engine or multi-agent design work in this feature.
- No more sophisticated Cursor readiness heuristic than the minimal marker chosen for v1, unless implementation proves it necessary.

## Current Architecture Fit

The existing repo already has the right abstraction boundary for this feature:

- `orchestrator/session` exposes explicit constructors and a shared session handle
- `orchestrator/internal/session/backend` owns backend-specific launch and readiness differences
- `orchestrator/cmd/tmux_codex` and `orchestrator/cmd/tmux_claude` are thin command adapters over the shared session package

Cursor should fit this exact pattern rather than reopening the design:

- one new backend descriptor
- one new public constructor
- one new launcher binary

This keeps the architecture consistent and avoids turning a bounded backend-addition task into another command-surface refactor.

## User-Facing Product Contract

### Supported launcher binaries

After this feature, the supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`

No generic launcher is introduced or revived.

### Purpose of `tmux_cursor`

`tmux_cursor` launches a persistent Cursor Agent instance inside a tmux session.

The command:

- creates a tmux session
- gets the default pane from that session
- sends a shared launch command that sources `$HOME/.agentrc`, prepends `~/.agent-bin` to `PATH`, disables terminal echo, and starts `agent`
- waits until Cursor is ready for input
- optionally attaches the current command IO streams to the tmux session

### Invocation

```sh
tmux_cursor [--session <name>] [--attach] [--mkpipe [<path>]]
```

### Flags and validation

`tmux_cursor` uses the same CLI and validation contract as the existing launchers:

- `--session <name>`
  - optional tmux session name override
  - default: `cursor`
- `--attach`
  - attach to the created tmux session after readiness succeeds
- `--mkpipe [<path>]`
  - attach-only FIFO prompt injection
  - same optional-value syntax, path rules, and duplicate-flag validation as the current launchers

Validation behavior remains parallel:

- positional arguments are rejected
- empty or whitespace-only `--session` is rejected
- `--mkpipe` without `--attach` is rejected
- duplicate `--mkpipe` is rejected
- `-h` exits successfully

Exit codes remain:

- `0`: success, including `-h`
- `1`: runtime failure
- `2`: usage or validation error

### Success output

On successful launch without `--attach`, `tmux_cursor` prints:

```text
Launched Cursor in tmux session "<session-name>"
```

On successful launch with `--attach` and without `--mkpipe`, the command does not print a launch banner before handoff to tmux attach.

On successful launch with `--attach --mkpipe`, before tmux attach begins, the command prints exactly one line:

```text
Attaching Cursor tmux session "<session-name>" with mkpipe "<absolute-fifo-path>"
```

This mirrors the current Codex/Claude launcher style.

### Failure behavior

`tmux_cursor` uses the same runtime failure model as the existing launchers:

- tmux session creation failure is terminal
- pane creation failure is terminal and triggers best-effort cleanup
- launch-environment validation failure is terminal and triggers best-effort cleanup
- launch send failure is terminal and triggers best-effort cleanup
- readiness failure is terminal and triggers best-effort cleanup
- mkpipe setup failures are terminal before attach handoff
- attach failure is terminal

There is no non-tmux fallback mode.

## Public Session Package Contract

### New constructor

Add:

```go
session.NewCursor(config)
```

This constructor returns the same public `*session.Session` handle type currently used by `NewCodex` and `NewClaude`.

### No other public API changes

The public session API does not otherwise change for this feature:

- `Config`
- `MkpipeConfig`
- `LockPolicy`
- `AttachOptions`
- `AttachInfo`
- `Start`
- `Attach`
- `SendPrompt`
- `Capture`
- `Close`

All current session semantics apply to Cursor sessions unchanged.

### Default session naming

When `Config.SessionName` is empty, `session.NewCursor(config)` resolves the default session name to:

```text
cursor
```

This matches the existing per-backend default naming convention used by Codex and Claude.

## Internal Backend Contract

### New backend descriptor

Add a new internal descriptor:

```text
orchestrator/internal/session/backend.Cursor
```

Its responsibilities match the existing backend types:

- provide the default session name
- launch the backend command through the shared environment builder
- decide when the backend capture is ready
- send prompts through the shared tmux send path

### Launch command

The Cursor backend launches:

```text
agent
```

No extra startup flags are injected in v1.

The design intentionally relies on the interactive default behavior of the installed Cursor CLI rather than spelling a subcommand such as `agent agent` or introducing a wrapper.

### Prompt sending

Cursor prompt sending reuses the same tmux-based text injection path as the other backends. There is no Cursor-specific prompt protocol in v1.

### Readiness contract

Cursor readiness should use the shared `waitUntilReady` loop with a Cursor-specific matcher.

The v1 ready-state rule is intentionally minimal:

- readiness requires a stable capture containing the literal text `Cursor Agent`
- readiness must still reject known login, trust, authentication, or setup interstitials

The shared quiet-period logic in `waitUntilReady` remains the stability mechanism. The backend matcher only answers whether a given capture looks ready.

If startup lands on a login, trust, authentication, or setup interstitial, the matcher continues returning not ready and the existing readiness wait expires through the standard timeout/runtime-failure path. v1 does not add Cursor-specific remediation messaging or an attach-through-setup mode.

### Rejected interstitials

For planning purposes, the Cursor matcher should explicitly treat login/setup-style screens as not ready when the captured text indicates flows such as:

- log in / login
- sign in
- authenticate / authentication
- trust prompts
- setup prompts that block entry into the interactive agent UI
- similar “press enter to continue” or bootstrap-interstitial states if observed during implementation

The important product rule is class-based, not tied to one brittle exact screen dump: setup interstitials are not ready even if they produce non-empty capture.

### Why the matcher is minimal

The user explicitly wants to avoid coupling readiness to volatile UI lines such as model/account text. The chosen tradeoff is:

- prefer a small stable marker now (`Cursor Agent`)
- explicitly reject known non-ready interstitial classes
- revise later only if real false positives or false negatives appear

## Launch Environment Contract

The shared launch-environment contract remains unchanged.

The shell command sent into tmux still:

- sources `$HOME/.agentrc` if present
- prepends the resolved absolute `~/.agent-bin` path to `PATH`
- disables local terminal echo with `stty -echo`
- starts the chosen backend command

For Cursor, that final backend command is `agent`.

### Operator setup expectation

Cursor availability remains an operator-managed environment concern.

This feature does not require:

- a repo-managed `scripts/bin/agent` wrapper
- a new setup target
- a hard requirement that the Cursor CLI binary live inside `~/.agent-bin`

The only runtime assumption is that `agent` resolves inside the launched shell after the normal `.agentrc` sourcing and `PATH` setup have run.

The current repo-managed `scripts/.agentrc` already fits this model by exposing `agent()` as a passthrough to the installed Cursor CLI, so the existing environment contract is sufficient for v1.

## Documentation and Build Contract

### New contract document

Add:

```text
orchestrator/cmd/tmux_cursor/CONTRACT.md
```

It should mirror the current launcher-contract style and document:

- product surface membership
- purpose
- invocation
- flag and validation rules
- runtime model
- output contract
- failure semantics
- exit codes

### Existing contract document updates

Update the shared “supported operator-facing harness binaries” wording in:

- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`

Those documents should list all three supported launchers after this feature lands.

No other product-contract rewrite is needed in this feature.

### Build behavior

`make build` should produce:

- `bin/tmux_codex`
- `bin/tmux_claude`
- `bin/tmux_cursor`

### Setup behavior

`make setup` remains unchanged.

The feature does not add any new setup command, shell integration step, or repo-managed Cursor wrapper.

## Testing Scope

Implementation planning should cover tests in three layers.

### Backend tests

Extend backend coverage with Cursor-specific tests analogous to the current Codex/Claude tests:

- default session name is `cursor`
- launch uses command `agent`
- `SendPrompt` forwards prompt text through the shared path
- readiness succeeds for stable capture containing `Cursor Agent`
- readiness rejects representative login/trust/setup interstitials

### Session package tests

Add at least constructor/default-name coverage for `session.NewCursor(config)`.

No new session-state behavior is expected beyond existing shared semantics.

### Command tests

Add `orchestrator/cmd/tmux_cursor/main_test.go` mirroring the current launcher coverage:

- parse/validation behavior
- detached success message
- attach behavior
- attach-time mkpipe banner
- constructor wiring and nil-constructor guard behavior

## Acceptance Criteria

The design is satisfied when all of the following are true:

- the repo exposes `session.NewCursor(config)` without changing the rest of the public session API
- the repo contains a private `backend.Cursor` implementation that launches bare `agent`
- the repo builds a `tmux_cursor` binary
- `tmux_cursor` follows the same CLI, attach, mkpipe, exit-code, and cleanup contract as the current launchers
- the supported launcher surface is documented as exactly three binaries
- `make setup` remains unchanged
- Cursor readiness uses the minimal stable `Cursor Agent` marker with explicit non-ready handling for login/trust/setup interstitials

## Risks and Follow-Up

The main deliberate risk in this design is readiness permissiveness.

Using only `Cursor Agent` as the positive marker keeps the matcher resilient to cosmetic UI changes, but it may need revision if:

- a non-ready Cursor screen also includes that text
- the ready screen removes or renames that text
- attach timing proves unreliable in practice

That risk is acceptable for v1 because:

- the user explicitly prefers the less brittle matcher
- the backend architecture isolates readiness logic to one backend-specific matcher
- tightening the matcher later is a local backend change rather than a session-architecture change
