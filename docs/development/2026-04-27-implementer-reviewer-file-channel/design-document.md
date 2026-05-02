# File-Channel Communication for `implement-with-reviewer`

**Goal:** Add a narrow internal side-channel between implementer and reviewer sessions using named pipes, so agents can send explicit out-of-band messages without relying on ordinary stdout transport.

**Status:** Drafted from reviewed Q&A decisions.

## Summary

`implement-with-reviewer` already runs a persistent tmux-backed implementer/reviewer loop, but communication between roles is still effectively whole-turn and orchestrator-shaped: the runner captures a complete turn result, then embeds that text into the next prompt. This design adds a second path for communication.

V1 introduces two fixed FIFO paths in the command's current working directory:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

The orchestrator creates those FIFOs before either agent session starts, tells both agents about them in startup prompts, and runs dedicated background readers for both directions for the lifetime of the run.

When an agent wants to send an explicit side-channel message, it writes the full body to the appropriate FIFO and closes the writer. The orchestrator reads until EOF, treats that byte sequence as one message, wraps it in a neutral `<side_channel_message>...</side_channel_message>` envelope, and injects it into the destination role's tmux pane immediately. This side-channel does not change orchestration control flow directly; it only adds more prompt text into the live session.

The feature is intentionally narrow:

- keep the current CLI surface unchanged
- keep the current implementer/reviewer loop unchanged
- use the file channel only for explicit agent-to-agent messages
- log channel events as run artifacts
- prefer simple lifecycle rules over guaranteed delivery

## Goals

- Add a simple internal transport for explicit implementer-to-reviewer and reviewer-to-implementer messages.
- Keep the existing `implement-with-reviewer` command-line UX unchanged.
- Preserve the current full-turn orchestration model for task, review, approval, and rewrite flow.
- Let agents use a side-channel without requiring stdout parsing or redesign of the main prompt loop.
- Reuse the existing tmux-backed persistent-session model.
- Persist channel activity as first-class runtime artifacts.

## Non-Goals

- No replacement of the current review/rewrite loop with channel-native orchestration.
- No multi-agent message bus beyond implementer and reviewer.
- No guaranteed delivery for messages that arrive before the destination is **ready to receive** (per [Early-arrival behavior](#early-arrival-behavior)).
- No control-message protocol that can directly alter loop control, end the run, or force extra iterations.
- No structured XML parsing, escaping, or schema validation for side-channel payloads.
- No support for multiple concurrent writers per FIFO.
- No requirement that side-channel text be excluded from normal tmux pane captures.

## Current State

Today:

- `implement-with-reviewer` reads one task from `stdin`
- it starts persistent tmux-backed sessions for implementer and reviewer
- the runner executes a serial loop of implementer turn, reviewer turn, and optional implementer rewrite
- per-turn completion is still driven by `<promise>done</promise>`
- artifacts are written under `log/runs/<run-id>/`
- the command acquires a per-working-directory `.harness_lock`, so only one harness command runs at a time in the same working directory

That last point matters for this design because V1 intentionally uses fixed relative FIFO paths in the current working directory instead of per-run FIFO paths.

## User-Facing Contract

### CLI surface

The external command surface does not change.

`implement-with-reviewer` still runs as:

```sh
cat task.txt | implement-with-reviewer --implementer <backend> --reviewer <backend> [--max-iterations N]
```

No new flags are added for side-channel messaging in V1.

### Agent-facing capability

The side-channel is an internal capability exposed to the implementer and reviewer through their startup prompts.

Each startup prompt must tell the agent:

- the available FIFO path it can write to in order to message the other role
- that the FIFO paths are:
  - `./to_reviewer.pipe`
  - `./to_implementer.pipe`
- that one message is one writer open-write-close cycle
- that the agent should write the full message body in one shot and close the writer

The prompt does not need extra anti-leakage rules beyond that. V1 relies on the normal role instructions and the agent's ability to follow the channel-writing contract.

## Channel Contract

### Topology

There are exactly two channel files:

- implementer writes to `./to_reviewer.pipe`
- reviewer writes to `./to_implementer.pipe`

V1 assumes exactly one logical writer per direction in normal operation.

### Message boundary

V1 does not use newline, sentinel, timeout, or JSON framing.

One message is defined as:

1. a writer opens the FIFO
2. the writer emits a complete message body
3. the writer closes the FIFO
4. the Go reader reads until EOF and treats the collected bytes as exactly one message

This means EOF is the message boundary. EOF is not part of the message body.

### Payload shape

The raw payload is arbitrary text.

V1 does not:

- require newline termination
- escape XML-sensitive text
- reject bodies containing `</side_channel_message>`
- convert the payload to structured JSON

The payload is treated as plain prompt text.

### Empty messages

If a FIFO write resolves to an empty or whitespace-only body after EOF:

- the orchestrator does not deliver it
- it records a non-fatal dropped-empty event in the channel log

## Runtime Model

### Startup order

At run startup, before either agent session starts:

1. acquire the existing working-directory lock
2. delete any existing `./to_reviewer.pipe` and `./to_implementer.pipe`
3. recreate both paths as FIFOs
4. start dedicated reader goroutines for both FIFOs
5. start implementer and reviewer sessions
6. deliver normal startup prompts, including channel-path instructions

The decision to delete existing FIFO paths is justified by the lock: once the run owns the working directory lock, it owns those fixed path names for the lifetime of the run.

### Reader behavior

Each FIFO has a dedicated background reader for the lifetime of the run.

When a writer completes an open–write–close cycle, the reader collects one message. For each such message:

1. read bytes until EOF
2. treat the collected bytes as the message body; apply the same empty/whitespace handling as in [Empty messages](#empty-messages) (no separate “classification” step beyond that)
3. if empty/whitespace-only, log and drop; then continue waiting for the next message on the same FIFO
4. otherwise map the FIFO direction to a source role and destination role
5. wrap the raw body in the delivery envelope
6. inject the wrapped text into the destination tmux pane
7. log the resulting delivery outcome
8. continue waiting for the next open–write–close cycle on the same FIFO until the run ends

A dedicated reader per FIFO therefore loops: block until a writer opens, read until EOF, handle delivery or drop, repeat. (V1 still assumes at most one writer at a time per direction; concurrent writers to the same FIFO are out of scope and behavior is undefined.)

### Delivery envelope

The orchestrator injects the following wrapper into the destination pane:

```text
<side_channel_message>
<raw body>
</side_channel_message>
```

There are no attributes such as `from=` in the wrapper.

The wrapper is only prompt text intended for the agent. It is not a machine-parseable XML protocol with strict escaping guarantees.

### Timing relative to active turns

If a side-channel message arrives while the destination role is already in the middle of an active turn:

- the orchestrator injects it immediately
- there is no special buffering, turn restart, or capture filtering
- if the injected text appears in the current turn capture, that is expected behavior

### Early-arrival behavior

If a side-channel message is read and would be delivered before the destination role is **ready to receive** side-channel text:

- drop the message
- log it as `dropped_not_started` (include the raw body in the log when it was read successfully, for debugging)

**Ready to receive (V1):** the destination’s tmux session exists, and that role’s startup prompt (including FIFO path instructions) has been fully written to its pane. Until then, the destination is not started for side-channel purposes.

**Prompt interleaving:** the orchestrator may deliver the two role startup prompts in any order. As soon as a role has *its* prompt, it can write to the peer’s FIFO; if the peer’s pane does not yet contain *its* startup instructions, the correct outcome is `dropped_not_started` for deliveries to that peer. V1 does not require global ordering of prompt delivery to prevent this.

V1 intentionally chooses drop-over-buffering because it is simpler and avoids preserving extra delivery state.

### Delivery success

A side-channel delivery is considered successful when the orchestrator successfully injects the wrapped message into the destination tmux pane.

V1 does not require:

- an acknowledgement marker from the receiving agent
- proof that the message later appeared in a captured turn artifact

### Delivery failure

If a message is successfully read from a FIFO but injection into the destination tmux pane fails:

- record the failed delivery in the channel log
- continue the run

This is intentionally softer than FIFO setup or reader failure. V1 treats transport setup as required for the feature, but treats individual delivery failures as non-fatal degraded behavior.

## Orchestration Semantics

The side-channel does not replace or rewire the main loop.

The existing loop remains:

1. implementer produces an implementation
2. reviewer reviews it
3. if not approved, implementer rewrites

Side-channel messages do not directly:

- approve a run
- fail a run
- trigger extra review rounds
- skip an iteration
- stop the command early

They only add more text into the live destination session.

## Paths And Cleanup

### FIFO paths

V1 uses fixed current-working-directory-relative paths:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

The startup prompts show those literal relative paths rather than resolved absolute paths.

### Stale paths

If either path already exists before FIFO creation:

- remove it unconditionally
- recreate the FIFO

This includes stale files left by previous failed runs.

### Cleanup

On both success and failure, normal cleanup removes:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

The FIFO files do not persist after the run ends.

## Runtime Artifacts

V1 keeps the existing artifact directory model under:

```text
log/runs/<run-id>/
```

In addition to existing metadata, captures, transitions, and result files, the run should persist a dedicated channel event log as JSONL. A simple path such as `log/runs/<run-id>/channel-events.jsonl` is sufficient for V1.

Each channel event record should include at least:

- timestamp
- source role
- destination role
- channel path
- status
- raw body when available

Suggested statuses include:

- `delivered`
- `delivery_failed`
- `dropped_empty`
- `dropped_not_started`
- `reader_error`

The raw body should be preserved for successful and failed message deliveries, and for `dropped_not_started` when the read succeeded, when available.

## Failure Semantics

### Fatal failures

The run fails if:

- FIFO setup fails
- FIFO reader startup fails
- channel runtime infrastructure fails in a way that breaks the side-channel itself

This follows the earlier decision that file-channel setup and read-path failures are part of the feature contract.

### Non-fatal failures

The run continues if:

- an individual message is empty
- an individual message would be delivered before the destination is ready to receive (per Early-arrival)
- an individual message cannot be injected into the destination pane

These cases are logged as artifacts, not terminal run failures.

## Accepted Risks In V1

- Messages that would be delivered before the destination is ready to receive are dropped.
- Immediate injection into an active destination pane may interleave with in-flight agent output.
- Because the wrapper is plain prompt text, payloads containing `</side_channel_message>` are not sanitized.
- Because V1 uses fixed relative FIFO paths, the design depends on the existing working-directory lock for path ownership discipline.
- Because prompt instructions expose the FIFO paths to agents, the system relies on normal instruction-following rather than a stricter side-channel protocol or permission model.
- Because delivery failures are non-fatal, some side-channel messages may be lost without terminating the run.

## Validation Strategy

The design should be considered successful when the implemented feature can demonstrate:

- FIFO creation before agent startup
- startup prompts that disclose the fixed FIFO paths and usage contract
- EOF-delimited message reading from both directions
- immediate tmux injection using the `<side_channel_message>` wrapper
- dropped-empty and dropped-not-started events recorded in artifacts
- non-fatal handling of individual injection failures
- FIFO cleanup on both success and failure

## Open Questions Deferred From V1

- Whether side-channel delivery should eventually require acknowledgements
- Whether side-channel messages should later support structured metadata
- Whether fixed relative FIFO paths should be replaced by per-run paths
- Whether side-channel messages should later influence loop control
- Whether side-channel traffic should eventually be isolated from normal turn captures
