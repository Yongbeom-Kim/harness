# mkpipe Session Input Design

**Date:** 2026-05-02
**Status:** Ready for implementation planning
**Feature Area:** `tmux_codex`, `tmux_claude`, `orchestrator/internal/mkpipe`

## Summary

Add an optional `--mkpipe` launcher feature to `tmux_codex` and `tmux_claude` so an attached session can accept prompts from a Unix named pipe. The launcher creates a FIFO, starts an in-process listener goroutine, and forwards each EOF-delimited write into the live tmux-backed agent session as one prompt.

This feature is intentionally narrow. It is only supported together with `--attach`, it does not introduce a detached supervisor or orchestration daemon, and it relies on standard Unix FIFO semantics rather than a richer socket protocol.

## Problem

The current launcher surface can start a persistent Codex or Claude process inside tmux and optionally attach the operator to it, but it has no simple machine-oriented input channel after startup. External automation currently has to drive tmux directly or rely on manual input.

The desired feature is a small Unix-native ingress path that can be enabled per launch without changing the broader launcher-only product direction.

## Goals

- Let operators opt into FIFO-backed prompt injection when launching `tmux_codex` or `tmux_claude`.
- Keep the feature compatible with the current launcher model instead of introducing a long-lived background service.
- Reuse the existing tmux prompt-send path so FIFO messages behave like normal typed prompts.
- Make message boundaries simple: one writer open/write/close cycle equals one forwarded prompt.
- Fail clearly on invalid flag combinations and invalid FIFO paths.

## Non-Goals

- No support for `--mkpipe` without `--attach`.
- No detached helper process, daemon, or second tmux pane just to keep FIFO listening alive.
- No message acknowledgement, persistence, retry queue, or delivery receipt.
- No attempt to support multiple concurrent writers safely.
- No automatic session recreation if prompt delivery starts failing.
- No automatic cleanup of stale FIFOs left behind by hard crashes.
- No non-Unix portability target; this feature is defined in terms of Unix named-pipe behavior.

## User-Facing Contract

### Supported commands

The feature is available on both launcher binaries:

```sh
tmux_codex --attach --mkpipe
tmux_codex --attach --mkpipe ./custom.pipe
tmux_claude --attach --mkpipe
tmux_claude --attach --mkpipe ./custom.pipe
```

`--mkpipe` has two operator-visible forms:

- `--mkpipe` by itself enables FIFO input at a default path.
- `--mkpipe <path>` enables FIFO input at an explicit path.

Because standard Go flag parsing does not support optional values on a single flag, the implementation may use custom argument parsing. The product contract is the operator syntax above, not the parser mechanism.

The argument-disambiguation contract is:

- `--mkpipe` may appear at most once.
- `--mkpipe` may appear before or after `--session` and `--attach`.
- if the token after `--mkpipe` exists and does not begin with `-`, it is consumed as the explicit FIFO path
- otherwise `--mkpipe` is treated as the bare enable form and the next token is parsed normally as another flag or argument
- a custom path that would begin with `-` is unsupported in raw form and must be spelled with disambiguating path syntax such as `./-pipe` or an absolute path
- a duplicate `--mkpipe` is a usage error and exits with code `2`

### Attach requirement

`--mkpipe` is only valid when `--attach` is also present.

If `--mkpipe` is supplied without `--attach`, the launcher:

- prints a clear validation error to `stderr`
- exits with code `2`
- does not start or leave behind a session

### Pre-attach output

When `--mkpipe` is enabled and launch succeeds, the launcher prints one concise status line before attaching. That line must include:

- the resolved tmux session name
- the resolved absolute FIFO path

The exact wording can follow the existing launcher style, but there should be exactly one concise pre-attach line rather than a verbose multi-line preamble.

## FIFO Path Contract

### Default path

If the operator uses bare `--mkpipe`, the launcher creates the FIFO in the current working directory.

The default filename format is:

```text
.<sanitized-session-name>.mkpipe
```

Examples:

- session `codex` -> `./.codex.mkpipe`
- session `reviewer 1` -> `./.reviewer-1.mkpipe`

The session name must be sanitized into a filesystem-safe basename before composing the filename. For planning purposes, the sanitization contract is:

- preserve ASCII letters, digits, `.`, `_`, and `-`
- replace other characters, including spaces and path separators, with `-`
- collapse repeated `-` runs when practical
- if sanitization would produce an empty basename, fall back to the backend default name

### Custom path

If the operator uses `--mkpipe <path>`:

- absolute paths are allowed
- relative paths resolve against the launcher's current working directory
- the launcher prints and logs the resolved absolute path

### Path validation

Before attach begins, the launcher validates the target FIFO path:

- the parent directory must already exist
- the target path itself must not already exist
- parent directories are never created automatically

If validation fails, the launch is treated as a startup failure. The command exits without attaching, and any partially created FIFO is removed.

These path and FIFO-setup failures are runtime startup failures, not usage errors, so they should follow the existing launcher failure model and exit with code `1`.

If a previous hard crash left a FIFO at the target path, the next launch fails until the operator removes that path manually.

## Runtime Model

### Startup sequence

With `--attach --mkpipe`, the high-level flow is:

1. Parse launcher arguments, including the `--mkpipe` optional path form.
2. Acquire the existing working-directory lock.
3. Start the backend tmux session and wait until Codex or Claude is ready using the existing readiness checks.
4. Resolve and validate the FIFO path.
5. Create the FIFO.
6. Start an in-process goroutine that owns the FIFO read loop.
7. Print the one-line pre-attach status message with session name and absolute FIFO path.
8. Call `tmux attach-session`.

The FIFO listener must not accept or process prompt input before backend readiness has been confirmed.

### FIFO listener lifecycle

The FIFO listener lives inside the launcher process as a goroutine. It is not a detached child process.

This means:

- the listener starts only for the lifetime of the attached launcher invocation
- when `tmux attach` returns, the launcher exits and the listener stops
- on normal shutdown, the launcher removes the FIFO it created
- on interrupt, the launcher performs best-effort FIFO cleanup before exiting

This is a deliberate scope constraint. The feature is for attached sessions only, not for headless long-lived session control.

### Message read loop

The FIFO protocol is EOF-delimited:

- the listener opens the FIFO for reading
- a writer opens the FIFO, writes bytes, and closes it
- the listener reads until EOF
- that complete byte stream is treated as one message
- the listener closes or reopens as needed and waits for the next writer

One open/write/close cycle equals one forwarded prompt.

## Message Normalization and Delivery

### Normalization

After a complete FIFO message is read:

- trim exactly one final trailing newline sequence if present
- if the remaining content is whitespace-only, ignore it
- otherwise forward the resulting payload as one prompt

This normalization is intentionally minimal. The launcher does not rewrite internal newlines or attempt to interpret structured message formats.

### Delivery timing

Completed FIFO messages are forwarded immediately after they are read.

The launcher does not:

- wait for the backend to become idle again
- queue messages until a future prompt boundary
- reject a message just because the backend is currently generating output

This is a best-effort design chosen to stay small. A FIFO message may therefore interleave with ongoing model output or with the operator's manual typing in the attached tmux session.

### Delivery failure behavior

If forwarding a message into tmux fails after startup:

- log the error to the launcher's standard streams
- drop that message
- keep the listener alive for later messages

The launcher does not retry the failed message, recreate the tmux session, or terminate the FIFO listener because of one delivery failure.

## Concurrency and Operator Expectations

### Concurrent writers

The product contract supports one writer at a time.

Concurrent writers are explicitly unsupported because EOF-delimited FIFO traffic can interleave in ways the launcher will not attempt to reconstruct. The implementation may serialize reads naturally through the FIFO loop, but the contract does not promise correct behavior for overlapping writers.

### Manual typing while attached

The operator may still type directly in the attached tmux session, but mkpipe delivery is only best effort in that situation. The product contract should tell operators to avoid manual typing while pipe-driven sends are active because the two input sources can interleave.

## Logging and Observability

Listener lifecycle messages and delivery errors are written to the launcher's standard streams while attached. This keeps the first version simple and avoids introducing a sidecar log file.

If stream interleaving becomes too noisy in practice, a future revision may move mkpipe runtime logs to a dedicated file, but that is not part of this design.

## Failure Semantics

### Validation and startup failures

The following conditions are terminal startup failures for mkpipe mode:

- `--mkpipe` used without `--attach`
- custom path parent directory does not exist
- target path already exists
- FIFO creation fails
- listener setup fails before attach begins

For startup failures that occur after the tmux session has been opened but before attach is handed over, the launcher should treat the run as failed setup rather than a partial success. Best-effort cleanup should remove any FIFO created for this launch, and the session should not be left behind as a hidden success path for a failed mkpipe launch request.

Only the `--mkpipe` without `--attach` combination is a usage error with exit code `2`. The other mkpipe setup failures above should exit with code `1`, consistent with the launcher's existing runtime-failure contract.

### Runtime failures after attach

Once attach has started, mkpipe runtime errors are non-fatal to the launcher unless the overall process is already exiting. A failed prompt delivery is logged and dropped, and the launcher remains attached until tmux attach returns or the process is interrupted.

## Proposed Package Boundary

The new reusable package should be:

```text
orchestrator/internal/mkpipe
```

Its responsibility is the named-pipe concern itself:

- resolve default and custom paths
- validate path preconditions
- create the FIFO
- read EOF-delimited messages
- remove the FIFO during cleanup

The command packages remain responsible for:

- CLI parsing
- attaching lifecycle rules
- signal handling
- logging
- forwarding payloads into the active backend session

This keeps `internal/mkpipe` focused on Unix FIFO behavior instead of making it own tmux or agent orchestration.

## Acceptance Criteria

- `tmux_codex --attach --mkpipe` and `tmux_claude --attach --mkpipe` create a default FIFO in the current working directory and print its absolute path before attaching.
- `tmux_* --attach --mkpipe <path>` uses the resolved custom path.
- `--mkpipe` without `--attach` fails as a usage error with exit code `2`.
- The FIFO listener starts only after backend readiness is confirmed.
- One writer open/write/close cycle becomes one forwarded prompt.
- A final newline-only terminator is trimmed once, and whitespace-only messages are ignored.
- Existing paths and missing parent directories fail clearly.
- Existing paths, missing parent directories, and FIFO creation failures exit with code `1`.
- FIFO cleanup happens on normal exit and best-effort interrupt handling.
- Delivery failures are logged and do not stop the listener.
- The feature does not claim safe concurrent-writer support or headless background persistence.
