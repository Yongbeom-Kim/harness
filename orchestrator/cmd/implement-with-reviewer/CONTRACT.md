# implement-with-reviewer Product Contract

## Product Surface

The supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
- `implement-with-reviewer`

This file documents the `implement-with-reviewer` shared-session workflow bootstrapper.

## Purpose

`implement-with-reviewer` bootstraps one shared tmux session with two agent runtimes:

- one implementer
- one reviewer

The command validates the requested backends, acquires the current-directory lock, creates one tmux session with two panes, starts both runtimes, starts both runtime-owned attach-scoped mkpipes, sends the seeded protocol prompts, prints one concise pre-attach line, and then attaches to tmux.

After bootstrap, the agents coordinate directly through the seeded mkpipe paths while the attached launcher process remains alive.

## Invocation

```sh
implement-with-reviewer --implementer <codex|claude|cursor> --reviewer <codex|claude|cursor> <prompt>
implement-with-reviewer -i <codex|claude|cursor> -r <codex|claude|cursor> <prompt>
```

## Inputs

### Required flags

- `--implementer <backend>` or `-i <backend>`
- `--reviewer <backend>` or `-r <backend>`

### Required positional input

- exactly one `<prompt>`

### Standard input

- `stdin` is not read for task input
- when attach begins, the configured IO streams are passed through to `tmux attach-session`

## Validation Contract

The command exits with code `2` and prints an error to `stderr` when:

- `--implementer` / `-i` is missing
- `--reviewer` / `-r` is missing
- either backend is not one of `codex|claude|cursor`
- no prompt is provided
- more than one prompt is provided
- the prompt is empty or whitespace-only after shell parsing

`-h` exits with code `0`.

## Runtime Model

### Shared session bootstrap

The command creates exactly one tmux session with a generated `implement-with-reviewer-...` name.

Inside that session:

- the implementer runtime uses the default pane
- the reviewer runtime uses the second pane returned by `NewPane()`

The command starts the two runtimes sequentially. Each runtime starts its own runtime-owned mkpipe as part of `Start()`. No seeded prompt is sent until both runtimes have started successfully and both resolved mkpipe paths are known.

### Seeded protocol

The implementer prompt includes:

- the original task
- role `implementer`
- the resolved absolute reviewer mkpipe path
- shared-session context
- the exact protocol markers
- the instruction to begin immediately and hand off review through the reviewer mkpipe

The reviewer prompt includes:

- the original task
- role `reviewer`
- the resolved absolute implementer mkpipe path
- shared-session context
- the exact protocol markers
- the instruction to wait for implementer handoff before reviewing

The command sends both seeded prompts with immediate backend semantics only after both runtimes have started and both resolved peer mkpipe paths are known.

The exact protocol markers are:

- `[IWR_IMPLEMENTATION_READY]`
- `[IWR_CHANGES_REQUESTED]`
- `[IWR_APPROVED]`
- `[IWR_BLOCKED]`

`[IWR_APPROVED]` is terminal for the autonomous loop. After approval, both agents remain idle in their panes for human follow-up.

### Mkpipe lifetime

Each runtime owns one attach-scoped mkpipe listener.

- the implementer default mkpipe basename is `.<sanitized-session-name>-implementer.mkpipe`
- the reviewer default mkpipe basename is `.<sanitized-session-name>-reviewer.mkpipe`
- seeded prompts receive the resolved absolute peer paths
- seeded bootstrap sends are immediate; later mkpipe traffic is queued and backend-specific
- the runtime forwards each mkpipe message directly into the queued-send path with no harness-owned retry, reorder, buffering, or idle-wait queue
- Claude has no native queued CLI gesture, so queued delivery is cooperative emulation via `Do this after all your pending tasks:\n\n<prompt>`

Before attach begins, runtime mkpipe errors are treated as bootstrap-fatal. After attach begins, mkpipe delivery failures are logged and dropped. When attach returns, both runtime mkpipes stop, the lock is released, and the tmux session remains alive.

## Output Contract

Before auto-attach begins, the command prints exactly one line:

```text
Attaching implement-with-reviewer tmux session "<session-name>" (implementer=<backend>, reviewer=<backend>)
```

Exit code `0` indicates successful bootstrap and a successful return from attach.

## Failure Semantics

Bootstrap failures are terminal and return exit code `1`, including:

- lock acquisition failure
- tmux session or pane creation failure
- runtime startup or readiness failure
- runtime mkpipe startup failure
- initial prompt send failure
- pre-attach runtime mkpipe delivery failure

Bootstrap cleanup is best effort and happens in this order:

1. stop any started runtime mkpipes
2. close the tmux session
3. release the lock

There is no detached workflow supervisor in v1.

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure
- `2`: usage or validation error
