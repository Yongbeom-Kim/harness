# Queued Session Prompts Implementation Decisions Log

Date: 2026-05-03
Topic: implementation architecture for explicit now-vs-queued prompt sending

## Explored Context

- `orchestrator/internal/agentruntime/runtime.go` currently owns `Start()`, `SendPrompt()`, mkpipe startup via `Config.Mkpipe`, mkpipe forwarding, capture, and pane-only close.
- `orchestrator/internal/agentruntime/backend/*.go` currently owns backend launch/readiness/prompt behavior, but prompt sending still relies on `tmux.TmuxPaneLike.SendText()` implicitly pressing `Enter`.
- `orchestrator/internal/agentruntime/tmux/interfaces.go` exposes only `SendText`, `Capture`, and `Close`; `tmux.go` currently makes `SendText` paste text and then send `Enter`.
- `orchestrator/cmd/tmux_codex`, `tmux_claude`, and `tmux_cursor` currently preserve nearly identical command-local interfaces and depend on `StartInfo.Mkpipe` from `Runtime.Start()`.
- `orchestrator/cmd/implement-with-reviewer/main.go` currently seeds both bootstrap prompts through a single `workflowRuntime.SendPrompt(string)` method after both runtimes and both runtime-owned mkpipes are ready.
- `orchestrator/internal/agentruntime/backend/backend_test.go` is the natural place to verify backend-specific text-plus-key sequences once the pane interface becomes explicit; `runtime_test.go` already owns mkpipe forwarding/state behavior.

## Round 1

1. Should this feature keep the current runtime-owned mkpipe lifecycle exactly where it is, or refactor it while introducing `SendPromptNow` and `SendPromptQueued`?
A: Keep the existing `Config.Mkpipe` plus `StartInfo.Mkpipe` flow and let `Runtime.Start()` continue to start mkpipe when configured; change only the send semantics and mkpipe forwarding target in this feature. (Recommended)
B: Split mkpipe startup into a new explicit `Runtime.StartMkpipe()` call now and make commands own the lifecycle transition.
C: Move mkpipe startup completely out of `Runtime` and into command packages while keeping the current runtime API otherwise.
D: Defer all mkpipe-touching work and only add the new direct-send methods for bootstrap paths.

User answer:

> 1A

Decision:

- Keep the current runtime-owned mkpipe lifecycle for this feature.
- Preserve `Config.Mkpipe`, `Runtime.Start()`, and `StartInfo.Mkpipe` as the mkpipe startup/reporting path.
- Limit the runtime change to explicit now-vs-queued send semantics and to switching mkpipe forwarding onto queued delivery.

2. Once `TmuxPaneLike.SendText()` becomes paste-only, which layer should own the explicit launch-time `Enter` that starts each backend CLI process?
A: Keep launch ownership in `backend.Launch(...)` and make the shared backend launch helper call `SendText(launchText)` followed by `PressKey("Enter")`, parallel to the new prompt-send helpers. (Recommended)
B: Return launch text from the backend and make `Runtime.Start()` append the launch `Enter` centrally.
C: Teach the tmux layer a second high-level helper just for launcher submission and keep backend launch logic text-only.
D: Let each command package press `Enter` after runtime construction and before readiness waits.

User answer:

> 2A

Decision:

- Keep backend launch ownership in `backend.Launch(...)`.
- Make the shared backend launch helper send text and then press `Enter` explicitly.
- Do not move launch-submission logic into `Runtime` or command packages.

3. How should command-local runtime interfaces absorb the API split away from the ambiguous `SendPrompt` name?
A: Keep command-local interfaces in each command package, but replace `SendPrompt` with only the methods each caller actually needs: `SendPromptNow` for `implement-with-reviewer`, and no send methods at all for the single-agent launchers. (Recommended)
B: Add both `SendPromptNow` and `SendPromptQueued` to every command-local runtime interface for consistency, even where one command never calls them.
C: Introduce one shared launcher/runtime interface package so all commands consume the same larger surface.
D: Keep a deprecated `SendPrompt` method on command-local interfaces only, even though the reusable runtime surface drops it.

User answer:

> 3A

Decision:

- Keep the command-local interfaces where they are.
- Replace `SendPrompt` only where needed with explicit `SendPromptNow`.
- Do not widen the single-agent command interfaces with unused send methods.

4. Where should the main behavioral verification for backend-specific now-vs-queued gestures live?
A: Put the sequence assertions primarily in `orchestrator/internal/agentruntime/backend/backend_test.go` using a recording pane that tracks `SendText` and `PressKey` calls, while `runtime_test.go` verifies only that direct calls and mkpipe forwarding choose `Now` versus `Queued` correctly. (Recommended)
B: Put the full gesture assertions only in `runtime_test.go` and keep backend tests focused on readiness matchers.
C: Cover the gestures only through command-package tests because they exercise real call sites.
D: Rely on manual tmux smoke tests for key-sequence behavior and keep automated tests at the API-shape level.

User answer:

> 4A

Decision:

- Put backend-specific text-plus-key sequence verification primarily in `orchestrator/internal/agentruntime/backend/backend_test.go`.
- Keep `runtime_test.go` focused on semantic call selection and mkpipe forwarding behavior rather than low-level key choreography.

## Round 2

5. How should the existing mkpipe-path reporting surface evolve now that this feature is intentionally not refactoring mkpipe lifecycle?
A: Keep `StartInfo` and `StartInfo.Mkpipe.Path` exactly as the command-facing source of resolved mkpipe paths; do not add new mkpipe accessors in this feature. (Recommended)
B: Keep `StartInfo` for compatibility but also add a new `Runtime.MkpipePath()` accessor now.
C: Remove `StartInfo` and make commands query a new runtime accessor after `Start()`.
D: Hide the mkpipe path from the reusable runtime and make commands resolve it independently.

User answer:

> 5A

Decision:

- Keep `StartInfo` and `StartInfo.Mkpipe.Path` unchanged as the command-facing mkpipe-path surface.
- Do not add any new runtime mkpipe-path accessor in this feature.

6. What should happen to the runtime error envelope for prompt sending when `SendPrompt` splits into `SendPromptNow` and `SendPromptQueued`?
A: Keep the existing error-envelope behavior and reuse the current prompt-send error kind/state shape so this feature changes semantics, not error taxonomy. (Recommended)
B: Introduce a new `ErrorKindPrompt` and migrate both new send methods to it immediately.
C: Return raw backend/tmux errors from the new send methods and only keep wrapped errors for startup/capture/close.
D: Make `SendPromptNow` wrapped and `SendPromptQueued` raw because queueing is backend-specific.

User answer:

> 6A

Decision:

- Keep the current runtime error-envelope behavior for prompt sending.
- Reuse the existing wrapped error kind/state shape instead of inventing a new error taxonomy in this feature.

7. Where should the standardized Claude queued-send wrapper live in code?
A: Keep it private to `orchestrator/internal/agentruntime/backend/claude.go` as a local constant/helper next to the explanatory comment and `SendPromptQueued` implementation. (Recommended)
B: Put it in a shared constant/helper in `backend/backend.go` so every backend can reference it.
C: Put it in `runtime.go` because mkpipe forwarding chooses queued semantics there.
D: Put it in `implement-with-reviewer/protocol.go` because the workflow uses queued peer sends most heavily.

User answer:

> 7A

Decision:

- Keep the Claude queued wrapper private to `orchestrator/internal/agentruntime/backend/claude.go`.
- Place the explanatory limitation comment next to that helper and `SendPromptQueued` implementation.

8. How generic should `TmuxPaneLike.PressKey(key string)` be in the implementation?
A: Make `PressKey` a thin generic passthrough to tmux `send-keys` with no local allowlist; tests only need to assert `Enter` and `Tab` for this feature. (Recommended)
B: Validate keys in tmux code and reject anything except `Enter` or `Tab`.
C: Replace the string with a small enum type so only known keys compile.
D: Expose only `PressEnter()` and `PressTab()` in implementation even if the interface text says generic.

User answer:

> 8A

Decision:

- Implement `PressKey(key string)` as a thin generic tmux `send-keys` passthrough.
- Do not add a local key allowlist or enum in this feature.
- Test only the required `Enter` and `Tab` behaviors.
