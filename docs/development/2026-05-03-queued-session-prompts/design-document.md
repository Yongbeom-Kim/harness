# Queued Session Prompts Design

**Date:** 2026-05-03
**Status:** Ready for implementation planning
**Feature Area:** `orchestrator/internal/agentruntime/runtime.go`, `orchestrator/internal/agentruntime/backend/*`, `orchestrator/internal/agentruntime/tmux/interfaces.go`, `orchestrator/internal/agentruntime/tmux/tmux.go`, `orchestrator/cmd/implement-with-reviewer`, `orchestrator/cmd/tmux_codex/CONTRACT.md`, `orchestrator/cmd/tmux_claude/CONTRACT.md`, `orchestrator/cmd/tmux_cursor/CONTRACT.md`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`

## Summary

Replace the current ambiguous runtime prompt-send capability with two explicit semantics:

- `SendPromptNow`
- `SendPromptQueued`

The harness will use those semantics as follows:

- all `mkpipe`-delivered messages use `SendPromptQueued`
- direct orchestration/bootstrap sends that must take effect immediately use `SendPromptNow`

The harness does not add its own semantic prompt queue. It forwards each message to the backend immediately and relies on backend-specific send handling: native CLI queue gestures where available, and Claude's documented cooperative wrapper where they are not.

To support backend-specific queue gestures, the tmux pane abstraction becomes more granular:

- `SendText(text string)` pastes text only
- `PressKey(key string)` sends one explicit tmux keypress such as `Enter` or `Tab`

Backend adapters own the mapping from `Now` versus `Queued` semantics to concrete text-plus-key sequences:

- Codex: `Enter` sends now, `Tab` queues
- Cursor: one `Enter` queues, a second `Enter` dispatches immediately
- Claude: no true queue support, so queued behavior is emulated by wrapping the prompt in a cooperative "do this later" instruction and sending it immediately

This feature targets only the current `orchestrator/internal/agentruntime` architecture. It also establishes the naming that any future public `orchestrator/session` package should reuse later, but no `orchestrator/session` code changes are part of this plan.

## Problem

The current harness has one prompt-send concept:

- `SendPrompt`

That name is no longer precise enough because the supported backends do not share the same terminal interaction:

- Codex distinguishes between immediate send and queued send
- Cursor also distinguishes between immediate send and queued send, but with different key behavior
- Claude Code does not provide a real queued-send gesture at all

Today the ambiguity leaks into runtime behavior:

- `mkpipe` forwarding always calls the one immediate send path
- `implement-with-reviewer` initial seeded prompts also call that same path
- the tmux abstraction hides the post-text keypress inside `SendText`, so callers cannot choose `Enter` versus `Tab`

That creates two product issues:

1. the harness cannot express "queue this follow-up after the current work" as a first-class capability
2. the current API suggests a backend-neutral behavior that does not actually exist

The user requirement is not to build a harness-owned scheduler. It is to expose explicit send semantics and let each backend CLI do its own native queueing when possible.

## Goals

- Replace the ambiguous `SendPrompt` contract with explicit `SendPromptNow` and `SendPromptQueued`.
- Make all `mkpipe`-delivered messages use queued semantics.
- Keep direct bootstrap/orchestration sends explicit and immediate where current workflow timing depends on it.
- Avoid any harness-owned semantic queue, retry buffer, or idle-state wait logic.
- Move the backend-specific `Now` versus `Queued` mapping into backend adapters.
- Make the tmux pane abstraction granular enough to express text injection separately from keypress choice.
- Preserve the current mkpipe failure contract:
  - before attach/bootstrap handoff, delivery failures are fatal
  - after attach begins, failures are logged, the failed message is dropped, and listening continues
- Document the Claude queued-send limitation in both code comments and contract/design docs.

## Non-Goals

- No new CLI flags or user-facing commands.
- No in-memory queue of prompts waiting for the agent to become idle.
- No delivery acknowledgement, retry, persistence, or exactly-once semantics.
- No attempt to inspect backend output and decide when the backend is "done."
- No change to mkpipe path parsing, FIFO lifecycle, or current attach-only scope.
- No change to startup readiness heuristics in this feature.
- No requirement to land the older `session` package refactor first.
- No code changes to any future or reintroduced public `orchestrator/session` package as part of this feature.
- No promise of true queued behavior for Claude Code, because the Claude CLI does not expose it.

## Current Architecture Context

The current runtime shape already contains the two call sites that matter:

- `agentruntime.Runtime.startMkpipeForwarders()` forwards FIFO messages into the backend
- `implement-with-reviewer` sends two direct seeded prompts during bootstrap

Those call sites need different semantics:

- mkpipe messages should be queued
- the two seeded bootstrap prompts should remain immediate

At the same time, the current tmux abstraction is too coarse:

- `TmuxPane.SendText(text)` pastes text and also presses `Enter`

That implicit `Enter` is currently used by both backend launch and prompt sending. Once the harness needs `Tab` for Codex queueing and different `Enter` sequences for Cursor, the hidden keypress becomes the wrong abstraction.

## Stable Product Contract

### Operator-facing surface does not change

The supported commands remain:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
- `implement-with-reviewer`

There are no new flags and no new operator workflow steps.

### mkpipe always uses queued semantics, with backend-specific execution

For all commands that use mkpipe:

- each EOF-delimited FIFO write is still one logical prompt
- normalization rules stay unchanged
- the runtime forwards that message immediately when it is read
- the runtime now uses `SendPromptQueued` instead of the old immediate send path

This means the runtime always calls `SendPromptQueued`, while the backend adapter decides how that semantic is carried out:

- Codex and Cursor use their native queue gestures
- Claude receives an immediate wrapped instruction that asks it to defer the work

The harness itself does not wait for an idle boundary and does not retain a separate pending-message list.

### Direct bootstrap sends remain immediate

`implement-with-reviewer` keeps its current bootstrap behavior:

- the implementer seeded prompt is sent with `SendPromptNow`
- the reviewer seeded prompt is sent with `SendPromptNow`

This keeps the bootstrap timing deterministic and avoids changing the current shared-session startup contract. Only later peer-to-peer mkpipe traffic becomes queued.

## Runtime and Backend API Contract

### Runtime surface

The current reusable runtime surface should expose:

- `SendPromptNow(prompt string) error`
- `SendPromptQueued(prompt string) error`

The old `SendPrompt(prompt string) error` method should be removed from the runtime API rather than preserved as an alias.

For this feature, that API change applies to:

- `agentruntime.Runtime`
- any command-local runtime interfaces used by `implement-with-reviewer`

Any future session abstraction built on top of this runtime behavior should reuse the same names, but that follow-on work is out of scope for this implementation plan.

### Backend surface

The backend adapter contract should also become explicit:

- `SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error`
- `SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error`

The old backend-level `SendPrompt` method should be removed.

Backends own how those semantic methods translate into tmux gestures. The runtime stays backend-agnostic and only chooses which semantic method to call.

### Runtime state and locking behavior stay the same

This feature does not change:

- when a runtime may send prompts
- the started/new/closed state model
- mkpipe startup and shutdown timing
- the existing send mutex as the serialization boundary

Both `SendPromptNow` and `SendPromptQueued` are valid only after successful runtime start and should return the same state error shape that the old prompt-send path returned when called too early or after close.

The existing runtime send mutex remains important and must guard the full multi-step send sequence. That means a send is atomic at the runtime level across:

- `SendText`
- one or more `PressKey` calls

Without that serialization, queued and immediate send gestures could interleave and corrupt the backend input stream.

## tmux Pane Contract

### New pane interface

`TmuxPaneLike` should expose:

- `SendText(text string) error`
- `PressKey(key string) error`
- `Capture() (string, error)`
- `Close() error`

### `SendText` becomes paste-only

`SendText` should:

- load the text into a tmux buffer
- paste that buffer into the pane
- not send `Enter`
- not send `Tab`

This is the most important low-level contract change in the feature.

### `PressKey` sends exactly one explicit tmux keypress

`PressKey(key string)` should use tmux `send-keys` for a single explicit key. In this feature the required keys are:

- `Enter`
- `Tab`

The method is generic rather than hardcoded to those two keys so later backends or workflows can reuse it without another interface break.

### Existing launch flow must become explicit too

Because `SendText` no longer presses `Enter`, backend launch helpers must change from:

- `SendText(launchText)`

to:

- `SendText(launchText)`
- `PressKey("Enter")`

This is not a new behavior at the product level. It is the necessary consequence of making the pane abstraction honest about which keypress actually happens.

## Backend-Specific Send Semantics

### Codex

Codex uses:

- `SendPromptNow`
  - `SendText(prompt)`
  - `PressKey("Enter")`
- `SendPromptQueued`
  - `SendText(prompt)`
  - `PressKey("Tab")`

This matches the current Codex CLI behavior where `Enter` dispatches immediately and `Tab` queues the follow-up.

### Cursor

Cursor uses:

- `SendPromptQueued`
  - `SendText(prompt)`
  - `PressKey("Enter")`
- `SendPromptNow`
  - `SendText(prompt)`
  - `PressKey("Enter")`
  - `PressKey("Enter")`

This intentionally models the real Cursor interaction:

- one `Enter` queues the prompt
- pressing `Enter` again sends it immediately

The design does not collapse `SendPromptNow` into `SendPromptQueued` for Cursor. The immediate semantic remains real and explicit.

### Claude

Claude Code uses:

- `SendPromptNow`
  - `SendText(prompt)`
  - `PressKey("Enter")`
- `SendPromptQueued`
  - construct:

```text
wrappedPrompt := "Do this after all your pending tasks:\n\n" + prompt
```

  - `SendText(wrappedPrompt)`
  - `PressKey("Enter")`

This is a deliberate emulation, not true queueing.

In that construction, `prompt` means the already-normalized prompt body inserted verbatim after the blank line. The implementation must not add quoting, code fences, XML-like placeholder markers, or any extra instruction text beyond the standardized prefix above.

The design must document that limitation clearly:

- Claude Code CLI has no native queued-send gesture that the harness can invoke
- `SendPromptQueued` for Claude is therefore only a cooperative instruction to the model
- the harness does not add a synthetic queue to compensate
- the product contract must not promise that Claude will wait for its current task to finish before observing the wrapped follow-up

This limitation should be noted in both code comments and the relevant contract/design docs so future callers do not assume parity with Codex or Cursor.

## mkpipe Delivery Contract

### All mkpipe traffic uses queued semantics

For every runtime with mkpipe enabled:

- `startMkpipeForwarders()` should forward `listener.Messages()` into `SendPromptQueued`
- it should no longer call the removed `SendPrompt`

That applies to:

- single-agent launcher mkpipe traffic
- `implement-with-reviewer` peer-to-peer mkpipe traffic

### No harness-owned pending queue

The harness behavior for a received mkpipe message is:

1. normalize the FIFO payload exactly as it does today
2. immediately invoke `SendPromptQueued`
3. return to listening

The runtime does not:

- wait for the backend to finish its current response
- retain a semantic pending list
- retry on failure
- reorder messages

If the backend CLI itself supports queued follow-ups, that native queue is the queue of record.

### Failure semantics remain unchanged

The current mkpipe failure policy stays in place:

- before attach/bootstrap handoff, a delivery failure is fatal
- after attach begins, a delivery failure is logged, the failed message is dropped, and listening continues

This applies equally to queued delivery. The feature changes the injection gesture, not the bootstrap-versus-runtime failure boundary.

## `implement-with-reviewer` Contract Changes

### Bootstrap sends are explicit immediate sends

`implement-with-reviewer` should send:

- the implementer seeded prompt with `SendPromptNow`
- the reviewer seeded prompt with `SendPromptNow`

This preserves the current contract that both seeded prompts are delivered during bootstrap after both runtimes and both mkpipes are ready.

### Later peer coordination uses queued sends

After bootstrap:

- implementer-to-reviewer mkpipe messages use queued delivery
- reviewer-to-implementer mkpipe messages use queued delivery

That means:

- Codex-backed peers use native queued follow-ups
- Cursor-backed peers use native queued follow-ups
- Claude-backed peers rely on the documented wrapper-based emulation

The workflow contract should describe that distinction explicitly, especially because Claude cannot guarantee true post-completion scheduling.

## Documentation Contract

The design requires contract-doc updates in the current codebase, even though the command-line surface itself does not change.

### Single-agent launcher contracts

`tmux_codex`, `tmux_claude`, and `tmux_cursor` contract docs should say that:

- mkpipe messages are queued to the backend rather than sent immediately
- the exact backend behavior differs by CLI
- Claude queued delivery is emulated through the standardized wrapper because the CLI lacks native queue support

### `implement-with-reviewer` contract

Its contract doc should say that:

- the two seeded bootstrap prompts are sent immediately
- later mkpipe traffic is queued
- Claude-backed queued traffic is cooperative emulation rather than true CLI queueing

### Code comments

The backend implementation for Claude should include a succinct code comment near `SendPromptQueued` explaining:

- there is no native queued-send gesture in Claude Code CLI
- the harness therefore wraps the prompt in the exact standardized instruction string
- this is a fundamental CLI limitation, not a harness bug

## Future `session` Package Note

This feature is implemented against `orchestrator/internal/agentruntime`, because that is the current runtime boundary in the repo.

If a future public `orchestrator/session` package is reintroduced, it should adopt the same explicit prompt-send contract:

- `SendPromptNow`
- `SendPromptQueued`

It should not revive the ambiguous `SendPrompt` name.

This section is design guidance for a later refactor, not implementation work for this feature. It keeps the semantic distinction stable across refactors and prevents the same backend-behavior ambiguity from reappearing under a different package name.

## Design Outcome

After this feature:

- the harness has an honest prompt-send API instead of one ambiguous method
- mkpipe traffic is always treated as queued follow-up work
- direct bootstrap/orchestration sends can still steer the backend immediately
- tmux no longer hides a mandatory `Enter` behind `SendText`
- backend-specific CLI differences live in backend adapters where they belong
- Claude's lack of real queue support is explicit and documented instead of being silently papered over
