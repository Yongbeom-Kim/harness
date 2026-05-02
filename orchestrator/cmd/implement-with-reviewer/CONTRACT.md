# implement-with-reviewer Product Contract

## Purpose

`implement-with-reviewer` runs a two-role agent loop:

- an `implementer` agent produces an implementation for the task read from `stdin`
- a `reviewer` agent reviews that implementation and either approves it or returns actionable feedback
- if not approved, the implementer rewrites the implementation using that feedback
- the loop repeats until approval or the maximum iteration limit is reached

The command preserves the existing CLI surface while using persistent tmux-backed backend sessions under the hood.

## Invocation

```sh
cat task.txt | implement-with-reviewer --implementer <backend> --reviewer <backend> [--max-iterations N]
```

Supported backends:

- `codex`
- `claude`

## Inputs

### Required flags

- `--implementer <backend>`
- `--reviewer <backend>`

### Optional flags

- `--max-iterations <N>`

### Environment variables

- `MAX_ITERATIONS`
  - used only when `--max-iterations` is not provided
  - default value when neither is provided: `10`

### Standard input

- the full task is read from `stdin`
- trailing newline characters are removed before execution
- a task that is empty or whitespace-only is rejected

## Validation Contract

The command exits with code `2` and prints an error to `stderr` when:

- `--implementer` is missing
- `--reviewer` is missing
- an unknown backend is provided for either role
- positional arguments are provided
- `--max-iterations` is not a positive integer
- `MAX_ITERATIONS` is set but not a positive integer, and `--max-iterations` is not provided
- the task from `stdin` is empty or whitespace-only

`-h` exits with code `0`.

## Runtime Model

### Persistent sessions

The command owns the implementer/reviewer loop directly and drives concrete tmux-backed `CodexAgent` / `ClaudeAgent` instances from `internal/agent`.

Each role owns its own tmux session for the lifetime of the run:

- one tmux session for the implementer
- one tmux session for the reviewer

This is an approved implementation deviation from the earlier reviewed design, which proposed one shared run-level tmux session with two panes.

Both role sessions are created, launched, and waited until ready before the first real implementer turn runs. There is no separate startup prompt phase.

### Side-channel runtime

Before either role session starts, the command creates two fixed FIFOs in the current working directory:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

Dedicated background readers stay attached to those FIFOs for the lifetime of the run. One FIFO open-write-close cycle is treated as one side-channel message. Non-empty message bodies are wrapped as:

```text
<side_channel_message>
<raw body>
</side_channel_message>
```

and injected immediately into the destination role's live tmux session.

This side-channel is additive only:

- it does not change the CLI surface
- it does not change approval detection
- it does not change iteration control or termination rules
- it may appear inside ordinary pane captures if delivery happens during an in-flight turn

If a message arrives before the destination session has launched and become ready, the command drops it and records `dropped_not_started`.

### Completion contract

Each backend turn is instructed to finish with:

```text
<promise>done</promise>
```

The command appends the exact instruction line to every main workflow turn:

```text
Finish your response with exactly <promise>done</promise>.
```

Approval is still detected by substring match on:

```text
<promise>APPROVED</promise>
```

Raw captures preserve the full pane text, including the done marker. Printed turn output, reviewer inputs, and final implementation text strip the terminal done marker.

### Idle timeout

Backend readiness uses the concrete agent startup readiness timeout. Runtime turns use the command idle timeout, defaulting to 30 minutes.

If no new output appears and the turn never produces a new exact `<promise>done</promise>` line before that timeout, the run fails.

## Prompt Contract

### Role prompts

Implementer role prompt:

```text
You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file.
```

Reviewer role prompt:

```text
You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with <promise>APPROVED</promise> and then the required completion marker on the final line. Otherwise respond with specific, actionable feedback only, followed by the required completion marker. No praise, no filler.
```

### Side-channel capability

Each role receives instructions for a fixed FIFO-based side channel as part of every main turn role contract:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

The role contract tells the agent which literal path it can write to in order to message the other role from its current session. One side-channel message is one writer open-write-close cycle. The agent should write the full message body and then close the writer.

### Turn prompt shapes

Initial implementer turn:

```text
<task>
```

Reviewer turn:

```text
Task given to implementer:
<task>

Implementation:
<current implementation>
```

Rewrite turn:

```text
Original task:
<task>

Your previous implementation:
<previous implementation>

Reviewer feedback:
<reviewer feedback>

Rewrite addressing all feedback.
```

The command package owns role contracts, fixed side-channel instructions, marker decoration, capture slicing, and the completion instruction line. Backend agents only provide raw `SendPrompt` and `Capture` transport.

## Output Contract

### Transcript format

The command prints the run header to `stdout`:

```text
Implementer : <backend>
Reviewer    : <backend>
Task        : <task>
```

Each completed turn is preceded by a banner:

```text
--- iter <n> - <ROLE> (<backend>) ---
```

Where:

- the initial implementer turn uses `iter 0 - IMPLEMENTER (<backend>)`
- reviewer turns use `iter <n> - REVIEWER (<backend>)`
- implementer rewrites use `iter <n> - IMPLEMENTER (<backend>)`

Completed turn text is printed to `stdout` only.

Runtime failures are printed to `stderr`.

The command does not add run-ID or tmux-session headers to the transcript.

### Success output

On approval:

```text
Approved after <N> review round(s).

Final implementation
<latest implementation>
```

Exit code: `0`

### Non-convergence output

If approval never arrives by `maxIterations`:

```text
Did not converge after <N> iterations.
```

Exit code: `1`

### Runtime failure output

If startup fails, a turn times out, tmux is unavailable, capture fails, cleanup fails, or artifact persistence fails:

- the command exits with code `1`
- the runtime error is printed to `stderr`
- whatever runtime artifacts were already written are preserved
- there is no fallback to the previous one-shot subprocess model

## Runtime Artifacts

Each run gets a UUIDv7 run ID and writes artifacts under:

```text
log/runs/<run-id>/
```

The artifact directory contains:

- `metadata.json`
- `state-transitions.jsonl`
- `channel-events.jsonl`
- `captures/*`
- `result.json`

### Metadata

`metadata.json` records:

- `run_id`
- the task and backend names
- `max_iterations`
- `idle_timeout_seconds`
- per-role session metadata under `sessions`

Each role session entry records:

- `backend`
- `tmux_session_name`

### Captures

Successful turn captures use stable iteration-addressed filenames:

- `captures/iter-0-implementer.txt`
- `captures/iter-1-reviewer.txt`
- `captures/iter-1-implementer.txt`
- `captures/iter-2-reviewer.txt`

Failure captures use explicit suffixes when capture text is available, for example:

- `captures/iter-0-implementer-startup.txt`
- `captures/iter-1-reviewer-timeout.txt`

### Result

`result.json` records the terminal run state:

- `approved`
- `non_converged`
- `failed`

It also records the final implementation when available and the terminal error string on failure.

### Channel events

`channel-events.jsonl` records side-channel activity with at least:

- timestamp
- source role
- destination role
- channel path
- status
- raw body when available

Supported status values are:

- `delivered`
- `delivery_failed`
- `dropped_empty`
- `dropped_not_started`
- `reader_error`

## Failure Semantics

- tmux unavailability or backend launch/startup failure fails fast
- FIFO setup failure or background FIFO reader failure fails fast
- a timeout persists a best-effort timeout capture and writes a failed result
- an individual side-channel delivery failure is non-fatal and is recorded as `delivery_failed`
- an empty side-channel message is non-fatal and is recorded as `dropped_empty`
- a side-channel message for a not-yet-started destination is non-fatal and is recorded as `dropped_not_started`
- artifact persistence failures are terminal even if the agent logic otherwise succeeded
- cleanup stops the FIFO manager before closing sessions, then removes the FIFO paths
- if cleanup itself fails, that cleanup failure becomes the terminal failure

The second approved implementation deviation from the earlier reviewed design is that tmux runtime verification is manual in v1 rather than a new checked-in integration suite.

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure or non-convergence
- `2`: usage or validation error
