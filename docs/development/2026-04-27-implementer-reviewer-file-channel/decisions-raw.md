# Raw Decisions

## 2026-04-27 Seed Direction

- Decision: design a new implementer-reviewer communication feature around internal file channels backed by named pipes (`mkfifo`) instead of piping one agent's stdout directly into the other.
- Proposed shape:
  - create channel paths such as `./to_reviewer.pipe` and `./to_implementer.pipe`
  - listen to those FIFOs from Go and surface received records through Go channels
  - let an agent emit an out-of-band message by writing text into the relevant pipe path
  - when the orchestrator receives a message, forward it into the target agent's tmux session
- Motivation:
  - decouple agent-to-agent communication from raw stdout transport
  - preserve the tmux-backed persistent-session model
  - open the door to more explicit agent communication semantics than whole-turn prompt synthesis alone

## Current Codebase Context

- `orchestrator/internal/implementwithreviewer/runner.go` currently runs a strict serial loop: implementer turn, reviewer turn, then implementer rewrite.
- `orchestrator/internal/cli/command.go` currently treats each turn as prompt submission plus pane capture polling until `<promise>done</promise>` is observed.
- The current tmux model already supports persistent pane-local processes, pane capture, and prompt injection, but it does not expose a separate inter-agent message transport.
- The existing command contract still treats reviewer feedback and rewritten implementation as full turn outputs that the orchestrator re-embeds into later prompts.

## 2026-04-27 Question Round 1

1. What should the first scope of the file-channel feature be?
- Answer: `A`
- Interpretation: keep the current full-turn implementer/reviewer loop and add the file channel as a narrow internal side-channel for explicit agent-to-agent messages.

2. What message model should v1 use on the FIFO?
- Answer: `D`, clarified by follow-up `8A`
- Interpretation: avoid explicit delimiters in v1. A message boundary is one writer open-write-close cycle; the Go reader reads until EOF, delivers the collected bytes as one message, then reopens the FIFO for the next message.

3. When should the orchestrator read from these channels?
- Answer: `A`
- Interpretation: start dedicated background readers for both FIFOs as soon as the run starts so side-channel messages can arrive asynchronously.

4. How should received channel messages be injected into the target agent session?
- Answer: `A`
- Interpretation: wrap each incoming message in a stable orchestrator-authored envelope before sending it into the destination tmux session.

5. What should happen if a destination agent is currently producing output when a channel message arrives?
- Answer: `B`
- Interpretation: allow immediate injection into the live tmux pane even while the destination agent is mid-response.

6. What failure behavior should v1 have if FIFO setup or reading fails?
- Answer: `A`
- Interpretation: treat file-channel setup or runtime failure as a hard runtime failure for the run.

## 2026-04-27 Question Round 1 Follow-up

8. What exact boundary rule should v1 use for FIFO messages?
- Answer: `A`
- Interpretation: one message equals one writer open-write-close cycle; EOF is the message boundary rather than newline, sentinel, timeout, or one `read()` call.

## 2026-04-27 Question Round 2

9. Where should the FIFO paths live for each run?
- Answer: `D`
- Interpretation: use current-working-directory-relative channel paths such as `./to_reviewer.pipe` and `./to_implementer.pipe` rather than placing the FIFOs under the run artifact tree.

10. How should agents learn the channel paths and usage rules?
- Answer: `A`
- Interpretation: provide the channel paths and exact write contract in each role's startup prompt so the capability is available throughout the persistent session.

11. What should the orchestrator-authored envelope look like when delivering a side-channel message into tmux?
- Answer: `B`
- Interpretation: inject a neutral XML-like wrapper with no attributes, using `<side_channel_message>...</side_channel_message>`.

12. If a side-channel message arrives during an active turn and we inject it immediately, how should it relate to that turn's capture/output?
- Answer: `A`
- Interpretation: treat the injected side-channel text as part of the live pane transcript for the in-flight turn; no special exclusion or restart behavior is required.

13. Should the file-channel messages also be persisted as first-class artifacts separate from pane captures?
- Answer: `A`
- Interpretation: persist a dedicated channel event log with timestamps, direction metadata, and raw message bodies in addition to ordinary pane captures.

14. What writer concurrency model should v1 assume for each FIFO?
- Answer: `A`
- Interpretation: assume exactly one logical writer per direction in normal operation: implementer writes only to `to_reviewer` and reviewer writes only to `to_implementer`.

## Additional Codebase Context

- `orchestrator/cmd/implement-with-reviewer/main.go` acquires a per-working-directory lock via `.harness_lock`, so only one harness command runs at a time in a given current working directory.
- That existing lock reduces collision risk for fixed `./to_reviewer.pipe` and `./to_implementer.pipe` paths, though stale FIFO cleanup and path disclosure rules still need design decisions.

## 2026-04-27 Question Round 3

15. Since `./to_reviewer.pipe` and `./to_implementer.pipe` are fixed relative paths, what exact path should the startup prompt show agents?
- Answer: `B`
- Interpretation: show only the literal relative paths `./to_reviewer.pipe` and `./to_implementer.pipe`.

16. What should startup do if one of those FIFO paths already exists before the run begins?
- Answer: `B`
- Interpretation: once the directory lock is acquired, the run owns those path names and should delete any existing path unconditionally, then recreate the FIFO.

17. How should cleanup treat the FIFO files after the run ends?
- Answer: `A`
- Interpretation: remove both FIFO paths during normal cleanup on both success and failure.

18. What should the dedicated channel artifact log format be?
- Answer: `A`
- Interpretation: persist channel events as JSONL with timestamp, direction metadata, delivery status, and raw message body.

19. If a side-channel message body is empty or whitespace-only after reading EOF, what should v1 do?
- Answer: `A`
- Interpretation: ignore the message and record a non-fatal dropped-empty event in the channel log.

20. Should the startup prompt explicitly tell agents not to mention FIFO paths or side-channel tags in ordinary stdout responses unless intentionally sending a side-channel message?
- Answer: `B`
- Interpretation: do not add a special anti-leakage rule; rely on the normal role prompts and side-channel instructions without extra guard text.

## 2026-04-27 Question Round 4

21. When should the orchestrator create the FIFOs and start the reader goroutines?
- Answer: `A`
- Interpretation: create both FIFOs and start their readers before either agent session starts, so the startup prompts can safely reference already-existing paths.

22. If a side-channel message is read before the destination role session has finished startup, what should v1 do?
- Answer: `C`
- Interpretation: if a message arrives before the destination role session is marked started, drop it and record a `dropped_not_started` event rather than buffering or failing the run.

23. What counts as a successful side-channel delivery?
- Answer: `A`
- Interpretation: delivery succeeds once the orchestrator successfully injects the wrapped message into the destination tmux pane; no agent acknowledgement is required.

24. If tmux injection of a received side-channel message fails, what should happen?
- Answer: `B`
- Interpretation: log the failed delivery and continue the run rather than treating injection failure as terminal.

25. Should side-channel messages ever alter loop control directly, such as ending the run early or forcing an extra reviewer pass?
- Answer: `A`
- Interpretation: no; in v1 they are prompt text injections only and do not directly change orchestration control flow.

26. If the raw message body itself contains `</side_channel_message>`, how should v1 handle it?
- Answer: `A`
- Interpretation: do no escaping or validation in v1; the wrapper is prompt text for the agent, not machine-parseable XML with strict body constraints.
