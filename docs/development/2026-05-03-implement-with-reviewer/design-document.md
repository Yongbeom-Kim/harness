# Implement-With-Reviewer Design

**Date:** 2026-05-03
**Status:** Ready for implementation planning
**Feature Area:** `orchestrator/cmd/implement-with-reviewer`, `orchestrator/internal/agentruntime/*`, `orchestrator/internal/agentruntime/tmux`, `orchestrator/cmd/tmux_codex`, `orchestrator/cmd/tmux_claude`, `orchestrator/cmd/tmux_cursor`, `Makefile`

## Summary

Add a new operator-facing binary:

```sh
implement-with-reviewer --implementer <codex|claude|cursor> --reviewer <codex|claude|cursor> <prompt>
implement-with-reviewer -i <codex|claude|cursor> -r <codex|claude|cursor> <prompt>
```

The command is a tmux-backed bootstrapper, not a long-running orchestration engine.

Its job is:

1. create one shared tmux session
2. create two agent runtimes inside that tmux session on separate panes
3. resolve and start two attach-scoped mkpipe listeners
4. send one seeded prompt to the implementer and one seeded prompt to the reviewer, each containing the resolved absolute peer mkpipe path and the inter-agent protocol
5. print one concise pre-attach status line
6. auto-attach to the tmux session

After that point, the implementer and reviewer coordinate directly through the two mkpipes. The harness process does not supervise each turn, write iteration artifacts, or decide when to forward the next message.

This feature also refactors the current runtime boundary:

- commands own tmux session lifecycle
- each agent runtime owns one pane plus one backend lifecycle
- the current `internal/session` package family is renamed to `internal/agentruntime` to avoid collision with the tmux-session concept

## Problem

The current codebase supports only single-agent launchers:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`

Those launchers currently depend on a runtime type that still owns the tmux session itself. That makes the current boundary too coarse for the requested workflow because:

- `implement-with-reviewer` needs one tmux session with two panes, not two separate tmux sessions
- each coding agent should own only its pane/backend lifecycle, not the full tmux session lifecycle
- current mkpipe wiring is tied to the runtime's attach path, which fits one-session launchers but not a shared-session bootstrapper
- the current package name `session` is now ambiguous because this feature introduces both tmux sessions and agent-runtime "sessions" in the same design

Without a refactor, the new workflow would either duplicate too much session logic at the command layer or force one runtime instance to own infrastructure that must be shared across two agents.

## Goals

- Add `implement-with-reviewer` as a new operator-facing harness command.
- Support `codex`, `claude`, and `cursor` as either implementer or reviewer.
- Use one shared tmux session with two panes.
- Make the command auto-attach after bootstrap.
- Let the two agents coordinate through mkpipe paths included in their initial prompts.
- Keep the command as a bootstrapper rather than a supervising workflow engine.
- Refactor the current runtime boundary so commands own tmux session lifecycle and runtimes own pane/backend lifecycle.
- Rename `internal/session` to `internal/agentruntime`.
- Preserve the existing `tmux_codex`, `tmux_claude`, and `tmux_cursor` CLI contracts while refactoring them onto the new ownership model.

## Non-Goals

- No revival of the old artifact-writing iteration runner.
- No headless or detach-safe agent-to-agent messaging in v1.
- No automatic commit, MR creation, or downstream workflow on approval.
- No deadlock prevention or escalation policy beyond the prompt contract.
- No `stdin` task input for the new command.
- No user-configurable tmux session name in v1.
- No change to the operator-facing CLI contract of `tmux_codex`, `tmux_claude`, or `tmux_cursor`.

## Product Surface

After this feature, the supported operator-facing binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
- `implement-with-reviewer`

The first three remain launcher-only commands for one backend in one tmux session.

`implement-with-reviewer` is the first workflow command, but it is still intentionally narrow. It only bootstraps a shared tmux session and initial agent-to-agent coordination.

## CLI Contract

### Invocation

```sh
implement-with-reviewer --implementer <backend> --reviewer <backend> <prompt>
implement-with-reviewer -i <backend> -r <backend> <prompt>
```

Supported backend values:

- `codex`
- `claude`
- `cursor`

### Inputs

Required flags:

- `--implementer <backend>` or `-i <backend>`
- `--reviewer <backend>` or `-r <backend>`

Required positional input:

- exactly one `<prompt>` positional argument

`stdin` is not read for task input.

### Validation

The command exits with code `2` and prints an error to `stderr` when:

- `--implementer` / `-i` is missing
- `--reviewer` / `-r` is missing
- either backend is not one of `codex|claude|cursor`
- no positional prompt is provided
- more than one positional prompt is provided
- the positional prompt is empty or whitespace-only after shell parsing

`-h` exits with code `0`.

### Exit codes

- `0`: success, including `-h`
- `1`: runtime failure
- `2`: usage or validation error

### Pre-attach output

Before auto-attach begins, the command prints exactly one concise line to `stdout` containing:

- the generated tmux session name
- the implementer backend
- the reviewer backend

Example shape:

```text
Attaching implement-with-reviewer tmux session "<session-name>" (implementer=<backend>, reviewer=<backend>)
```

The exact session-name generation algorithm is internal. The product contract only requires a tmux-safe unique name per invocation.

## Runtime Model

### High-level behavior

`implement-with-reviewer` is a bootstrapper:

1. validate flags and the prompt
2. acquire the current-directory lock
3. create one tmux session with a generated unique name
4. obtain two panes from that session
5. construct one implementer runtime on pane 1
6. construct one reviewer runtime on pane 2
7. start both runtimes and wait until both are ready
8. resolve and start two mkpipe listeners owned by the command process
9. send the initial implementer and reviewer prompts, each containing the resolved absolute peer mkpipe path and protocol rules
10. print the pre-attach status line
11. attach to the shared tmux session

Once attached:

- mkpipe listeners continue forwarding messages into the two runtimes
- the harness process does not inspect message content or supervise turns
- the agents are expected to continue the loop autonomously through the seeded protocol

When attach returns:

- the command stops both mkpipe listeners
- the command releases the lock
- the tmux session remains alive

This matches the current launcher principle that attach return ends the launcher process, not the tmux-hosted backend session.

### Detach and lifetime guarantee

Autonomous agent-to-agent messaging is only guaranteed while the attached `implement-with-reviewer` process remains alive.

This is an explicit v1 limitation:

- if the operator detaches and the attach process exits, the command-owned mkpipe listeners also stop
- the tmux session and both backend CLIs remain alive
- further coordination becomes manual unless a future feature adds detach-safe listener ownership

This limitation is acceptable in v1.

## Architecture and Responsibility Changes

### Package rename

Rename:

```text
orchestrator/internal/session
```

to:

```text
orchestrator/internal/agentruntime
```

and move the current subpackages under that tree:

- `internal/agentruntime/backend`
- `internal/agentruntime/tmux`
- `internal/agentruntime/env`
- `internal/agentruntime/mkpipe`
- `internal/agentruntime/dirlock`

The rename should stay small. Most identifiers can remain stable unless they are directly ambiguous with tmux-session semantics.

### New ownership split

After this refactor, the ownership model is:

- command layer
  - acquires and releases lock
  - creates and optionally kills tmux sessions
  - attaches to tmux sessions
  - owns attach-scoped mkpipe listeners
  - prints operator-facing status lines
- agent runtime layer
  - owns one pane
  - launches one backend in that pane
  - waits for readiness
  - sends prompts into that pane
  - captures pane output
  - closes only that pane/backend

This split applies both to `implement-with-reviewer` and to the existing single-agent launchers.

### Runtime API direction

The current runtime API must stop owning the tmux session lifecycle.

The intended internal shape is:

- backend-specific constructors remain explicit:
  - `NewCodex(...)`
  - `NewClaude(...)`
  - `NewCursor(...)`
- constructors receive an existing pane and tmux-session context instead of creating a tmux session themselves
- runtime methods remain narrow:
  - `Start() error`
  - `SendPrompt(prompt string) error`
  - `Capture() (string, error)`
  - `Close() error`

`Attach()` should no longer live on the runtime because tmux attach is now a command-owned concern.

The old runtime-owned lock policy should also move up to the command layer because lock scope is about tmux-session creation, not pane/backend lifecycle.

### Runtime close semantics

Runtime `Close()` must never kill the whole tmux session.

Instead:

- add pane-close support to the tmux layer
- make runtime `Close()` kill only its own pane/backend
- keep whole-session cleanup as a command responsibility

For tmux, this means adding pane-level close support, for example through `kill-pane`.

The design intent is about ownership boundary: runtime close must not invoke session-kill behavior directly. If tmux itself removes a now-empty single-pane session as a side effect of closing the last pane, that is acceptable, but it is not treated as runtime-owned tmux-session lifecycle.

## Shared tmux Session Contract

`implement-with-reviewer` creates exactly one tmux session.

Inside that session:

- the implementer runtime gets the session's default pane
- the reviewer runtime gets the second pane created by `NewPane()`

The exact visual split direction is not part of the product contract. The only contract is that both agents run in the same tmux session on different panes.

If bootstrap fails before attach is handed over, the command should treat that as failed setup rather than partial success:

- stop any started mkpipe listeners
- best-effort close the tmux session
- release the lock
- return a runtime failure

## Mkpipe Contract

### Ownership

In v1, `implement-with-reviewer` owns both mkpipe listeners.

The command may reuse the existing mkpipe resolution logic with minimal change, but it is responsible for:

- resolving the two paths
- creating the FIFOs
- starting the two read loops
- forwarding each FIFO's messages into the correct runtime with `SendPrompt`
- stopping both listeners when attach returns or startup fails

This keeps the change small and avoids reworking mkpipe into a detached runtime-owned subsystem.

### Ordering requirement

The mkpipe correctness rule is strict:

- both FIFOs must already exist
- both listeners must already be running

before either initial agent prompt is sent.

Resolving the path alone is not sufficient. If the initial prompt tells an agent to write to the peer pipe before that peer listener exists, the first coordination message can fail.

### Default path naming

Because both runtimes share one tmux session name, their mkpipe basenames must be role-specific.

The default v1 shape should be:

- implementer: `.<sanitized-shared-session-name>-implementer.mkpipe`
- reviewer: `.<sanitized-shared-session-name>-reviewer.mkpipe`

Both paths live in the current working directory unless a future feature introduces explicit overrides.

This avoids collisions between the two roles while staying close to the current mkpipe naming model.

### Resolved path form

Before either listener starts, the command resolves both mkpipe paths to absolute filesystem paths.

The seeded prompts must include those resolved absolute peer paths, not the role-specific basename or a current-directory-relative form. This keeps the protocol stable even if either agent changes directories after startup.

### Runtime guarantee

While attached, the command forwards:

- implementer FIFO messages into the implementer pane
- reviewer FIFO messages into the reviewer pane

Listener errors remain runtime failures only during bootstrap. After attach begins, individual prompt-delivery failures are best-effort logged and dropped, matching the current mkpipe philosophy.

## Prompt and Protocol Contract

### Initial implementer prompt

The seeded implementer prompt must include:

- the operator's original task
- explicit role: implementer
- the resolved absolute reviewer mkpipe path
- the shared-session context: both agents are live in the same workspace and tmux session
- the exact protocol markers
- the rule that review handoff happens by writing to the reviewer mkpipe

The implementer should begin work immediately.

### Initial reviewer prompt

The seeded reviewer prompt must include:

- the operator's original task
- explicit role: reviewer
- the resolved absolute implementer mkpipe path
- the shared-session context
- the exact protocol markers
- the rule that review begins when the implementer notifies it through mkpipe

The reviewer does not need to produce an immediate review. It should wait for implementer handoff, inspect the actual workspace changes, and then respond through the implementer mkpipe.

### Exact protocol markers

The v1 protocol uses exact literal first-line markers:

- `[IWR_IMPLEMENTATION_READY]`
- `[IWR_CHANGES_REQUESTED]`
- `[IWR_APPROVED]`
- `[IWR_BLOCKED]`

Every inter-agent mkpipe message must start with exactly one of those markers on line 1.

The rest of the message body is free-form and role-specific.

### Marker semantics

`[IWR_IMPLEMENTATION_READY]`

- implementer -> reviewer
- means there is a reviewable code change
- body should summarize what changed and what the reviewer should inspect

`[IWR_CHANGES_REQUESTED]`

- reviewer -> implementer
- means approval is withheld
- body should contain specific actionable review feedback

`[IWR_APPROVED]`

- reviewer -> implementer
- terminal approval for the autonomous loop
- once sent, both agents should stop autonomous pipe messaging and remain idle in their panes for human follow-up

`[IWR_BLOCKED]`

- either direction
- means the sender wants help from the peer
- body should contain the concrete blocker or question

### Clarification behavior

The chosen v1 rule is peer-first and intentionally loose:

- an agent that is blocked should ask the peer first through mkpipe
- if the peer also cannot resolve it, the agents may continue messaging
- there is no automatic deadlock prevention, timeout-based escalation, or forced stop rule in v1

Human intervention remains available through the attached tmux session.

## Existing Single-Agent Commands

`tmux_codex`, `tmux_claude`, and `tmux_cursor` keep their current CLI contracts:

- same flags
- same output wording
- same exit-code behavior
- same attach-only mkpipe semantics

Their internals change to match the new ownership split:

1. command acquires lock
2. command creates tmux session
3. command obtains the first pane
4. command constructs one agent runtime on that pane
5. command starts the runtime
6. if `--mkpipe` is enabled, command starts the attach-scoped listener
7. command attaches to tmux session when requested

This refactor keeps the product surface stable while making the multi-pane workflow possible.

## Failure Semantics

### Usage failures

Invalid CLI usage returns exit code `2`.

### Bootstrap failures

If any of the following fail before attach handoff, the command exits with code `1` and performs best-effort cleanup:

- lock acquisition
- tmux session creation
- pane allocation
- backend startup
- readiness wait
- mkpipe path resolution
- FIFO creation
- listener startup
- initial prompt send

Best-effort cleanup includes:

- stop any listener already started
- close the tmux session
- release the lock

### Attached runtime failures

After attach has begun:

- listener delivery errors are logged and dropped
- detach or process exit stops autonomous pipe forwarding
- the tmux session stays alive

There is no fallback non-tmux mode.

## Documentation and Build Changes

Implementation must update:

- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`
- `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- `Makefile`

The three tmux launcher contract docs currently describe the supported command surface as exactly three binaries. They must be updated to include `implement-with-reviewer`.

`make build` must also produce `bin/implement-with-reviewer` alongside the three existing launcher binaries.

## Testing Strategy

The implementation should add focused automated coverage for:

- `implement-with-reviewer` argument parsing and validation
- generated pre-attach status line
- shared-session bootstrap ordering
- role-specific mkpipe default path generation and absolute-path resolution
- command-owned mkpipe listener forwarding into the correct runtime
- new pane-close behavior in tmux
- runtime `Close()` closing only its pane, not the whole session
- single-agent launcher refactor regressions

Manual smoke testing should verify at least:

- `codex` / `claude` / `cursor` all work as implementer or reviewer
- both panes start in one tmux session
- initial prompts include the resolved absolute peer pipe path and protocol markers
- implementer -> reviewer and reviewer -> implementer mkpipe messaging works while attached
- detaching stops autonomous messaging but leaves the tmux session alive

## Acceptance Criteria

- `implement-with-reviewer` exists with the required CLI shape.
- It starts one shared tmux session and two backend runtimes on separate panes.
- It resolves and starts both mkpipe listeners before seeding either agent.
- It sends one initial prompt to each agent containing the resolved absolute peer mkpipe path and exact markers.
- It auto-attaches after bootstrap and prints one concise pre-attach line.
- The autonomous loop works while attached.
- Approval leaves both agents idle rather than auto-closing them.
- `internal/session` has been renamed to `internal/agentruntime`.
- `make build` produces `bin/implement-with-reviewer`.
- `tmux_codex`, `tmux_claude`, and `tmux_cursor` still behave the same from the operator's perspective.
