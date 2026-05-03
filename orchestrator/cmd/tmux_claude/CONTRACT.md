# tmux_claude Product Contract

## Product Surface

The supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
- `implement-with-reviewer`

This file documents the `tmux_claude` member of the single-agent launcher surface and the shared launch-environment behavior used by all three launchers.

## Purpose

`tmux_claude` launches a persistent Claude instance inside a tmux session.

The command creates a tmux session, starts `claude` through the shared launcher environment, waits until Claude is ready for input, and optionally attaches the current command IO streams to the tmux session.

## Invocation

```sh
tmux_claude [--session <name>] [--attach] [--mkpipe [<path>]]
```

## Inputs

### Optional flags

- `--session <name>`
  - tmux session name
  - default: `claude`
- `--attach`
  - after launching Claude in the pane, attach the provided command IO streams to the tmux session
- `--mkpipe [<path>]`
  - attach-only FIFO prompt injection
  - bare `--mkpipe` creates `.<sanitized-session-name>.mkpipe` in the launch working directory
  - relative paths are resolved against the launch working directory
  - absolute paths are used unchanged

### Positional arguments

- positional arguments are not allowed

### Standard input

- `stdin` is not read for launch configuration
- when `--attach` is used, the configured `stdin` stream is passed through to `tmux attach-session`

## Validation Contract

The command exits with code `2` and prints an error to `stderr` when:

- positional arguments are provided
- `--session` is empty or whitespace-only
- `--mkpipe` is provided without `--attach`
- `--mkpipe` is provided more than once

`-h` exits with code `0`.

Raw `--mkpipe -pipe` is unsupported because a token beginning with `-` remains available for normal flag parsing; use `--mkpipe ./-pipe` or an absolute path when the FIFO basename starts with a dash.

The shared launcher environment validates `~/.agent-bin` before sending the backend command into tmux. Startup fails as a runtime launch error if `~/.agent-bin` is missing or is not a directory.

## Runtime Model

### Session creation

The command creates a tmux session using the requested session name.

It then requests the session's first pane and sends a sourced launcher command equivalent to:

```sh
bash -lc 'if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; export PATH='"'"'/Users/example/.agent-bin'"'"':"$PATH"; stty -echo; '"'"'claude'"'"''
```

The shared launcher contract for `tmux_codex`, `tmux_claude`, and `tmux_cursor` is:

- source `$HOME/.agentrc` if present
- prepend `~/.agent-bin` to `PATH`
- disable local terminal echo with `stty -echo`
- start the backend command

### Attach behavior

If `--attach` is provided, the command calls tmux attach against the created session after Claude has started and become ready for input.

If `--mkpipe` is provided, the launcher waits for backend readiness before creating the FIFO listener, opens the tmux attach handle before printing the mkpipe banner, and keeps the listener alive only for the attached launcher process. There is no detached helper, headless persistence, or supervisor process.

Each normalized mkpipe message is forwarded directly into the backend's queued-send path rather than sent immediately. The harness does not add its own retry, reorder, buffering, or idle-wait queue. Queue behavior is backend-specific: Codex uses its queued gesture, Cursor uses its queued Enter path, and Claude has no native queue so the runtime pastes `Do this after all your pending tasks:\n\n<prompt>` and presses `Enter` as cooperative emulation.

The attach path uses the configured command IO streams:

- provided `stdin`
- provided `stdout`
- provided `stderr`

## Output Contract

### Success output without attach

On successful launch without `--attach`, after Claude is ready for input, the command prints:

```text
Launched Claude in tmux session "<session-name>"
```

Exit code: `0`

### Success output with attach

On successful launch with `--attach` without `--mkpipe`:

- the command does not print the launch banner first
- control is handed to tmux attach using the configured IO streams

Exit code: `0` when attach returns successfully.

On successful launch with `--attach --mkpipe`, before tmux attach begins, the command prints exactly one line:

```text
Attaching Claude tmux session "<session-name>" with mkpipe "<absolute-fifo-path>"
```

mkpipe traffic is queued and backend-specific rather than immediate. Runtime listener errors and queued delivery failures are logged to standard streams, the failed prompt is dropped, and the attached session continues.

### Runtime failure output

If tmux session creation, pane creation, launch-environment validation, launcher send, readiness wait, mkpipe startup, or attach fails:

- the command exits with code `1`
- the failure is printed to `stderr`

## Failure Semantics

- session creation failure is terminal
- pane creation failure is terminal and triggers best-effort session cleanup
- launch-environment validation failure is terminal and triggers best-effort session cleanup before backend startup
- launcher send failure is terminal and triggers best-effort session cleanup
- readiness failure is terminal and triggers best-effort session cleanup
- mkpipe startup failures, including missing parent directories, existing target paths, FIFO creation failures, and listener setup failures, are terminal
- before attach begins, mkpipe delivery failures are terminal; after attach begins, runtime listener errors and queued delivery failures are logged and dropped
- stale FIFOs from hard crashes are not cleaned up automatically and must be removed manually before the next mkpipe launch
- attach failure is terminal
- there is no fallback non-tmux execution mode

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure
- `2`: usage or validation error

## Concurrency and Operator Expectations

- concurrent mkpipe writers are unsupported
- one writer open/write/close cycle is one prompt
- manual typing while pipe-driven sends are active is best effort only
- mkpipe is attach-only; it does not keep running after the attached launcher exits
