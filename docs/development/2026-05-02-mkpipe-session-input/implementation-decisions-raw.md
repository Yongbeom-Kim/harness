# mkpipe Session Input Implementation Decisions

Date: 2026-05-02
Topic: implementation structure for mkpipe-backed prompt injection in `tmux_codex` and `tmux_claude`
Source design doc: `docs/development/2026-05-02-mkpipe-session-input/design-document.md`
Source decision log: `docs/development/2026-05-02-mkpipe-session-input/decisions-raw.md`

## Exploration Summary

- `orchestrator/cmd/tmux_codex/main.go` and `orchestrator/cmd/tmux_claude/main.go` currently duplicate the same launcher flow: parse args, acquire `dirlock`, start agent, wait for readiness, optionally attach, otherwise print a success banner.
- Both concrete agents already expose `SendPrompt(prompt string) error`, but the shared `agent.Agent` interface in `orchestrator/internal/agent/agent.go` does not currently include that capability.
- The repository currently has no shared launcher-argument package. Both command binaries use independent `flag.FlagSet` parsing with the same `--session` and `--attach` behavior.
- The design already places FIFO mechanics in a new `orchestrator/internal/mkpipe` package and keeps launch lifecycle ownership outside that package.
- The only existing launcher product contract doc is `orchestrator/cmd/tmux_codex/CONTRACT.md`, but it already documents behavior shared by both launcher binaries.

## Round 1

1. How should launcher code obtain prompt-send capability for mkpipe delivery after startup?
A: Expand `agent.Agent` to include `SendPrompt(prompt string) error` so the launcher can stay agent-oriented and avoid direct tmux coupling. (Recommended)
B: Keep `agent.Agent` unchanged and add a second narrow interface in the command packages, then type-assert the constructed agent to that interface.
C: Re-open the tmux session from the launcher and send FIFO payloads directly through tmux APIs, bypassing the agent layer after startup.
D: Make `internal/mkpipe` depend on concrete `*agent.CodexAgent` and `*agent.ClaudeAgent` types for prompt injection.

User answer:

> 1 A

Decision:

- Expand `agent.Agent` to include `SendPrompt(prompt string) error`.
- Launcher code will keep using the agent abstraction instead of coupling directly to tmux after startup.

2. Where should the custom `--mkpipe` optional-value parsing live?
A: Add a small shared launcher-argument package so both binaries tokenize `--session`, `--attach`, and `--mkpipe [path]` through one implementation. (Recommended)
B: Keep `parseArgs` local to each `main.go` and duplicate the `--mkpipe` token scan in both command packages.
C: Put argument parsing into `internal/mkpipe`, even though most parsed fields are launcher concerns rather than FIFO concerns.
D: Introduce a third-party CLI parser just to support optional flag values.

User answer:

> 2 B

Decision:

- Keep `parseArgs` local to each `main.go`.
- Duplicate the `--mkpipe` token scan in `tmux_codex` and `tmux_claude` rather than extracting a shared parser package.

3. How much of the launch and attach flow should be shared between `tmux_codex` and `tmux_claude` in this change?
A: Extract a small shared launcher runner for the common lock/start/ready/mkpipe/attach flow, while each binary keeps only wiring such as labels, defaults, and agent construction. (Recommended)
B: Keep orchestration duplicated in both `main.go` files and only share the new `internal/mkpipe` helpers.
C: Move the full attach and mkpipe lifecycle into `internal/agent`, so agents own launcher orchestration.
D: Implement the flow only for `tmux_codex` first and copy it to `tmux_claude` later.

User answer:

> 3 B

Decision:

- Keep launcher orchestration duplicated in both `main.go` files.
- Share only the new `internal/mkpipe` helpers and the widened agent interface.

4. Which component should own signal handling and FIFO cleanup for `--attach --mkpipe` runs?
A: The launcher runner owns context cancellation, interrupt handling, and deferred cleanup; `internal/mkpipe` only exposes idempotent create/read/close/remove operations. (Recommended)
B: `internal/mkpipe` installs OS signal handlers internally and cleans up the FIFO on its own.
C: The agent layer owns signal handling because it already owns session startup and shutdown.
D: Skip explicit interrupt handling and rely only on normal process exit cleanup.

User answer:

> 4 A

Decision:

- The launcher runner in each command package owns context cancellation, interrupt handling, and deferred cleanup.
- `internal/mkpipe` should expose idempotent setup and teardown operations without installing its own signal handlers.

## Round 2

5. What API boundary should `orchestrator/internal/mkpipe` own?
A: Give `internal/mkpipe` a small listener type that resolves/validates the path, creates/removes the FIFO, performs normalization, and runs the blocking FIFO read loop; each launcher only wires channel consumption, logging, and attach lifecycle. (Recommended)
B: Keep `internal/mkpipe` as stateless helpers only (`ResolvePath`, `ValidatePath`, `Create`, `Remove`, `ReadOnce`), and implement the loop plus normalization separately in both launcher binaries.
C: Let `internal/mkpipe` own only FIFO creation/removal while the launcher packages use raw `os.OpenFile`/`io` code for reading and normalization.
D: Put the read loop into `internal/agent` because prompt forwarding eventually targets an agent.

User answer:

> 5 A, but can we make it a Go channel? Then every message is sent via the channel.

Decision:

- `internal/mkpipe` remains the primary owner of path resolution, validation, FIFO creation/removal, normalization, and the background read loop.
- Replace the callback-oriented sketch with a channel-oriented contract so normalized prompt payloads are emitted to the launcher through Go channels.
- Follow-up required: lock the exact channel and shutdown contract.

6. If mkpipe setup fails after the backend session is already ready but before attach begins, how should the launcher remove that partially started success path?
A: In each launcher, treat mkpipe setup as part of startup and call `agent.Close()` on any pre-attach mkpipe failure so the tmux session is not left behind. (Recommended)
B: Have `internal/mkpipe` accept a closer callback and own session cleanup on startup failure.
C: Leave the tmux session running because the backend itself started successfully.
D: Re-open the tmux session and kill it through tmux APIs instead of closing through the agent.

User answer:

> 6 A

Decision:

- In each launcher, mkpipe setup remains part of startup.
- Any pre-attach mkpipe setup failure triggers `agent.Close()` so the tmux session is not left behind.

7. Who should own message normalization for EOF-delimited FIFO payloads?
A: `internal/mkpipe` owns normalization: trim exactly one trailing newline sequence, ignore whitespace-only payloads, and only invoke the launcher callback for deliverable prompts. (Recommended)
B: The launcher packages receive raw FIFO payload bytes and normalize them before calling `agent.SendPrompt`.
C: `agent.SendPrompt` should normalize inbound mkpipe payloads so manual prompts and FIFO prompts share one path.
D: The tmux pane layer should normalize text before `SendText`.

User answer:

> 7 A

Decision:

- `internal/mkpipe` owns normalization.
- It trims exactly one trailing newline sequence, ignores whitespace-only payloads, and emits only deliverable prompts to the launcher.

8. Where should the product-contract documentation change live for this feature?
A: Update `orchestrator/cmd/tmux_codex/CONTRACT.md` as the shared launcher contract document for both `tmux_codex` and `tmux_claude`, extending invocation, validation, output, and failure sections for mkpipe. (Recommended)
B: Add a second `orchestrator/cmd/tmux_claude/CONTRACT.md` and document each binary separately.
C: Skip product-contract updates and rely only on the design + implementation docs for mkpipe behavior.
D: Add a new top-level README or docs page and leave the existing contract doc unchanged.

User answer:

> 8 B

Decision:

- Add a separate `orchestrator/cmd/tmux_claude/CONTRACT.md`.
- Both launcher contract docs should describe mkpipe behavior explicitly rather than relying on one shared file.

9. What test split should this implementation plan require?
A: Add per-binary command tests for `--mkpipe` parsing and attach-validation in both `main_test.go` files, plus focused unit tests under `internal/mkpipe` for path resolution, FIFO lifecycle, normalization, and listener behavior, plus a small `internal/agent` interface regression where needed. (Recommended)
B: Test only `internal/mkpipe` heavily and rely on minimal smoke coverage in one launcher binary.
C: Favor only end-to-end/manual verification because FIFO behavior is too OS-specific for useful unit tests.
D: Add parser tests only and leave FIFO runtime behavior to manual validation.

User answer:

> 9 A

Decision:

- Add per-binary command tests in both launcher packages for mkpipe parsing and attach validation.
- Add focused `internal/mkpipe` unit tests for path resolution, FIFO lifecycle, normalization, and listener behavior.
- Add any small `internal/agent` interface regression coverage required by the widened agent contract.

## Round 3

10. What exact channel contract should `internal/mkpipe` expose to the launchers?
A: Expose a `Listener` handle with resolved `Path`, `Messages() <-chan string`, `Errors() <-chan error`, and `Close() error`; `internal/mkpipe` owns the goroutine and channel lifecycle. (Recommended)
B: Expose a single typed event channel carrying either prompt payloads or listener errors.
C: Require the launcher to pass in its own `chan string`, and `internal/mkpipe` only writes normalized messages to that caller-owned channel.
D: Avoid a background goroutine and expose a blocking `ReadNext()` method instead of channels.

User answer:

> 10 A

Decision:

- Expose a `Listener` handle that owns the goroutine and channel lifecycle.
- The launcher-facing surface should include the resolved FIFO path plus `Messages() <-chan string`, `Errors() <-chan error`, and `Close() error`.

11. How should the listener shut down cleanly when `tmux attach` returns or the process is interrupted while the FIFO loop may be blocked waiting for input?
A: `Listener.Close()` actively wakes or unblocks the FIFO read loop, waits for the goroutine to exit, then returns so the launcher can remove the FIFO path deterministically. (Recommended)
B: The launcher removes the FIFO path first and assumes blocked FIFO operations will terminate on their own.
C: Skip graceful listener shutdown and rely on process exit to clean up blocked operations.
D: Replace blocking FIFO reads with a polling loop so shutdown can be checked on a timer.

User answer:

> 11 A

Decision:

- `Listener.Close()` must actively wake or unblock the FIFO read loop and wait for the goroutine to exit before returning.
- Cleanup should be deterministic instead of relying on process exit or polling.

12. For the new `orchestrator/cmd/tmux_claude/CONTRACT.md`, how explicit should the file be?
A: Write a full standalone contract doc mirroring the launcher sections so implementers can update explicit files without cross-file indirection. (Recommended)
B: Make it a thin wrapper that points to `tmux_codex/CONTRACT.md` for shared launcher behavior.
C: Introduce a new shared launcher contract doc and reduce both binary-specific files to short wrappers.
D: Add only mkpipe-specific notes and leave the rest of Claude launcher behavior undocumented there.

User answer:

> 12 A

Decision:

- Add a full standalone Claude contract document instead of a thin wrapper or shared indirection file.
