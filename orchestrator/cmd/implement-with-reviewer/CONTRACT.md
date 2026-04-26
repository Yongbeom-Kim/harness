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

V1 replaces one-shot backend subprocess calls with persistent backend sessions exposed through the `internal/cli.Session` API.

Each role owns its own tmux session for the lifetime of the run:

- one tmux session for the implementer
- one tmux session for the reviewer

This is an approved implementation deviation from the earlier reviewed design, which proposed one shared run-level tmux session with two panes.

Both role sessions are created and started before the first real implementer turn runs. Startup acknowledgements are required to complete successfully, but successful startup text is not printed into the normal command transcript.

### Completion contract

Each backend turn is instructed to finish with:

```text
<promise>done</promise>
```

Backend adapters append the exact instruction line:

```text
Finish your response with exactly <promise>done</promise>.
```

Approval is still detected by substring match on:

```text
<promise>APPROVED</promise>
```

V1 intentionally does not strip `<promise>done</promise>` from:

- raw captures
- reviewer inputs
- final printed implementation output

### Idle timeout

Each startup acknowledgement and each runtime turn uses a fixed 120-second idle timeout.

If no new output appears and the turn never produces a new exact `<promise>done</promise>` line before that timeout, the run fails.

## Prompt Contract

### Role prompts

Implementer role prompt:

```text
You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file.
```

Reviewer role prompt:

```text
You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with exactly: <promise>APPROVED</promise> - nothing else. Otherwise respond with specific, actionable feedback only. No praise, no filler.
```

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

Backend adapters own startup-only prompt decoration and the completion instruction line. The feature package owns the role prompts and turn prompt bodies.

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

## Failure Semantics

- tmux unavailability or backend launch/startup failure fails fast
- a timeout persists a best-effort timeout capture and writes a failed result
- artifact persistence failures are terminal even if the agent logic otherwise succeeded
- cleanup is attempted for both sessions on both success and failure
- if cleanup itself fails, that cleanup failure becomes the terminal failure

The second approved implementation deviation from the earlier reviewed design is that tmux runtime verification is manual in v1 rather than a new checked-in integration suite.

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure or non-convergence
- `2`: usage or validation error
