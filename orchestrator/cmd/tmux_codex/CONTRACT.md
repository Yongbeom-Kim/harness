# codex Product Contract

## Purpose

`tmux_codex` launches a persistent Codex instance inside a tmux session.

The command:

- creates a tmux session
- gets the default pane from that session
- sends a launcher command that sources `$HOME/.agentrc` before starting `codex`
- waits until Codex is ready for input
- optionally attaches the current command IO streams to the tmux session

This command is a lightweight operator entrypoint for a single Codex-backed tmux session.

## Invocation

```sh
tmux_codex [--session <name>] [--attach]
```

## Inputs

### Optional flags

- `--session <name>`
  - tmux session name
  - default: `codex`
- `--attach`
  - after launching Codex in the pane, attach the provided command IO streams to the tmux session

### Positional arguments

- positional arguments are not allowed

### Standard input

- `stdin` is not read for launch configuration
- when `--attach` is used, the configured `stdin` stream is passed through to `tmux attach-session`

## Validation Contract

The command exits with code `2` and prints an error to `stderr` when:

- positional arguments are provided
- `--session` is empty or whitespace-only

`-h` exits with code `0`.

## Runtime Model

### Session creation

The command creates a tmux session using the requested session name.

It then requests the session's first pane and sends a sourced launcher command equivalent to:

```sh
bash -lc 'if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo; '"'"'codex'"'"''
```

The launcher contract is:

- source `$HOME/.agentrc` if present
- disable local terminal echo with `stty -echo`
- start `codex`

### Attach behavior

If `--attach` is provided, the command calls tmux attach against the created session after Codex has started and become ready for input.

The attach path uses the configured command IO streams:

- provided `stdin`
- provided `stdout`
- provided `stderr`

If any of those streams are nil in-process, the tmux layer falls back to `os.Stdin`, `os.Stdout`, and `os.Stderr`.

## Output Contract

### Success output without attach

On successful launch without `--attach`, after Codex is ready for input, the command prints:

```text
Launched Codex in tmux session "<session-name>"
```

Exit code: `0`

### Success output with attach

On successful launch with `--attach`:

- the command does not print the launch banner first
- control is handed to tmux attach using the configured IO streams

Exit code: `0` when attach returns successfully.

### Runtime failure output

If tmux session creation, pane creation, launcher send, readiness wait, or attach fails:

- the command exits with code `1`
- the failure is printed to `stderr`

If pane creation or launcher send fails after the tmux session is opened, the command attempts to close the session before returning the failure.

## Failure Semantics

- session creation failure is terminal
- pane creation failure is terminal and triggers best-effort session cleanup
- launcher send failure is terminal and triggers best-effort session cleanup
- readiness failure is terminal and triggers best-effort session cleanup
- attach failure is terminal
- there is no fallback non-tmux execution mode

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure
- `2`: usage or validation error
