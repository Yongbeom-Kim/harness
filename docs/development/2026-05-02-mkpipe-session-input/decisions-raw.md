# mkpipe Session Input Decision Log

Date: 2026-05-02
Topic: mkpipe-backed prompt injection for `tmux_codex` and `tmux_claude`

## Round 1

### 1. When `--mkpipe` is enabled, who should own the long-lived FIFO listener, given `tmux_codex` and `tmux_claude` currently exit right after readiness unless `--attach` is used?

User answer:

> I think let's have it sold that MKPIPE flat can only be supplied when we are using attachment. We could also have it sold that if you do not attach and we supply the MKPIP flag then we keep a process alive either in the fall of the background that will listen to the pipe and then forward whatever messages but I think that is a little overcomplicated and I am happy to keep this as a pure goroutine in the background.

Interpreted decision:

- Provisional: `--mkpipe` is only supported together with `--attach`.
- Follow-up required: clarify whether the FIFO listener lives in a goroutine inside the attached launcher process and therefore stops when attach returns.

### 2. How do you want the CLI to expose the path override?

User answer:

> so the MKPIPE flag will enable the feature, and if you do not provide a path, it will generate the default path

Decision:

- Use a boolean-style enable flag plus an optional explicit path override.
- Current preferred shape: `--mkpipe` enables the feature and a separate path-bearing flag provides overrides.
- A follow-up round can still rename the override flag if needed.

### 3. What should the default FIFO location be when the user enables mkpipe without a path?

User answer:

> A

Decision:

- Default FIFO path lives in the current working directory and is derived from backend and/or session identity.

### 4. What should terminate one inbound message read from the FIFO?

User answer:

> read until EOF = one message

Decision:

- One writer open/write/close cycle maps to one injected prompt.

### 5. When should the listener start accepting prompts relative to backend readiness?

User answer:

> A

Decision:

- The listener does not begin consuming prompt input until the target backend session is confirmed ready.

### 6. What should happen if the target FIFO path already exists when the launcher starts?

User answer:

> A

Decision:

- Startup fails unless the target path is absent.
- Normal shutdown removes the FIFO created by the launcher.

### 7. If prompt injection fails after startup because the tmux session is gone or `SendText` fails, what should the mkpipe supervisor do?

User answer:

> log error, do not exit

Decision:

- Runtime prompt-forwarding failures are logged.
- The listener remains alive instead of exiting on the first delivery error.

## Round 2

### 8. If `--mkpipe` is only valid with `--attach`, what should happen to the FIFO listener when `tmux attach` returns?

User answer:

> 8 A

Decision:

- When `tmux attach` returns, the launcher exits.
- The in-process FIFO listener stops and the FIFO is removed during shutdown.

### 9. What exact CLI shape do you want for the path override?

User answer:

> 9 --mkpip and --mkpipe <path>?

Interpreted decision:

- Provisional: prefer the original operator shape where `--mkpipe` alone enables the default path and `--mkpipe <path>` provides an explicit override.
- Follow-up required: confirm the exact accepted syntax because Go's standard flag parser does not support optional values without custom parsing.

### 10. What should the default FIFO filename look like in the current working directory?

User answer:

> 10 A

Decision:

- Use a hidden, session-specific FIFO filename derived from the session name to avoid collisions across multiple sessions in one working directory.

### 11. How should we handle concurrent writers to the same FIFO, given EOF-delimited messages can interleave if multiple processes write at once?

User answer:

> 11 A

Decision:

- The product contract only supports one writer at a time.
- Reads are serialized and concurrent-writer behavior is explicitly unsupported.

### 12. What should happen if a writer opens and closes the FIFO without sending any bytes?

User answer:

> 12 A

Decision:

- Empty writes are ignored.

### 13. Where should the runtime listener report delivery errors or lifecycle messages while `--attach` is active?

User answer:

> 13 A, and let's leave a comment to write to a file if this becomes problematic.

Decision:

- Runtime listener messages go to the launcher's standard streams while attached.
- Note in the design: if stream interleaving becomes problematic in practice, the feature can later move to a dedicated log file.

## Round 3

### 14. Confirm the CLI syntax for the path override.

User answer:

> 14 A

Decision:

- Support custom parsing so `--mkpipe` alone uses the default path and `--mkpipe <path>` uses an explicit override.
- `--mkpipe` remains valid only when `--attach` is also present.

### 15. If the operator is manually typing in the attached tmux session while an external process writes to the FIFO, what product contract should we claim?

User answer:

> 15 A

Decision:

- mkpipe prompt forwarding is best effort only.
- Operators should avoid manual typing while pipe-driven sends are active because terminal input and forwarded prompts may interleave.

### 16. When a FIFO payload ends with a trailing newline, which is common for shell redirection, how should the forwarder treat it before injection?

User answer:

> 16 A, I think the trim Hill works perfectly fine and the trim logic can be used again to check for if the user send a bunch of bites. That is nothing but white space.

Decision:

- Trim exactly one final trailing newline sequence before injecting the payload.
- Reuse the trim/normalization step to detect whitespace-only payloads and ignore them instead of forwarding them.

### 17. How should custom mkpipe paths be interpreted?

User answer:

> 17 A

Decision:

- Custom paths may be absolute or relative.
- Relative paths resolve against the launch working directory.

### 18. What should the command print before attaching when mkpipe is enabled, given the current attach path skips the normal launch banner?

User answer:

> 18 A

Decision:

- Before attach, print one concise status line containing both the session name and the FIFO path.

### 19. What validation behavior should `--mkpipe` have if `--attach` is not provided?

User answer:

> 19 A

Decision:

- `--mkpipe` without `--attach` is a usage error.
- The command prints a clear validation message to `stderr` and exits with code `2`.

### 20. Where should the new reusable code live?

User answer:

> 20 A

Decision:

- Add `orchestrator/internal/mkpipe` to own FIFO path resolution plus create/open/read/remove behavior.
- Command packages continue to own process lifecycle and prompt forwarding orchestration.

### 21. On process interruption while attached with mkpipe enabled, what cleanup behavior do you want?

User answer:

> 21 A

Decision:

- Remove the FIFO on normal shutdown and via best-effort interrupt handling.
- If a hard crash leaves the FIFO behind, the next launch fails until the operator removes it.

## Round 4

### 22. When a complete FIFO message arrives while Codex or Claude is still generating the previous response, what should the forwarder do?

User answer:

> 22 B

Decision:

- Inject each completed FIFO message immediately after it is read.
- There is no readiness wait or delivery queue between completed FIFO reads and tmux injection.

### 23. If the operator passes a custom path whose parent directory does not exist, what should happen?

User answer:

> 23 A

Decision:

- Fail validation clearly.
- Do not create missing parent directories.

### 24. What path should the launcher print in the pre-attach status line and in mkpipe-related errors?

User answer:

> 24 A

Decision:

- Use the resolved absolute FIFO path in operator-facing output.

### 25. How should the default FIFO filename handle unusual tmux session names such as names with spaces or path separators?

User answer:

> 25 A

Decision:

- Sanitize the session name into a filesystem-safe basename before composing the default FIFO filename.

### 26. Should mkpipe impose an explicit maximum message size?

User answer:

> 26 A

Decision:

- Do not add a product-level message size limit.
- Read the full FIFO payload until EOF and rely on normal process memory limits.
