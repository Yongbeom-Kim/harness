# Queued Session Prompts Decision Log

Date: 2026-05-03
Topic: explicit now-vs-queued prompt sending for tmux-backed coding-agent sessions

## Round 1

### 1. Which current call sites should switch to queued semantics in v1?

Options presented:

- A: Only messages delivered from runtime-owned `mkpipe` listeners use queued semantics; seeded bootstrap prompts and other direct orchestration sends stay explicit and may use `SendPromptNow`.
- B: Both `mkpipe` messages and the initial seeded `implement-with-reviewer` prompts use queued semantics.
- C: Every programmatic send in the harness defaults to queued semantics, even outside `mkpipe`.
- D: Expose both methods now but do not switch any current caller yet.

User answer:

> 1A

Decision:

- In v1, only runtime-owned `mkpipe` deliveries switch to queued semantics.
- Seeded bootstrap prompts and other direct orchestration sends remain explicit and may use `SendPromptNow`.

### 2. Should the ambiguous `SendPrompt` name be retired from the reusable runtime/session surface?

Options presented:

- A: Yes. Replace it with only `SendPromptNow` and `SendPromptQueued` so every caller chooses explicitly.
- B: Keep `SendPrompt` as an alias to `SendPromptQueued` for compatibility.
- C: Keep `SendPrompt` as an alias to `SendPromptNow` for compatibility.
- D: Keep all three methods on the surface indefinitely.

User answer:

> 2A

Decision:

- Retire the ambiguous `SendPrompt` name from the reusable runtime/session surface.
- Replace it with explicit `SendPromptNow` and `SendPromptQueued` methods.

### 3. How should the tmux pane abstraction express the post-text keypress?

Options presented:

- A: Split it into `SendText(text string)` plus one generic key-trigger method such as `PressKey(key string)` that backends use for `Enter` or `Tab`.
- B: Split it into `SendText(text string)` plus dedicated `PressEnter()` and `PressTab()` methods only.
- C: Keep one `SendText(text string)` API and add an argument that controls whether it appends `Enter` or `Tab`.
- D: Let each backend bypass the pane abstraction and shell out to `tmux` directly.

User answer:

> 3A

Decision:

- Split the tmux pane abstraction into `SendText(text string)` plus a generic key-trigger method such as `PressKey(key string)`.
- Backends remain responsible for choosing the correct follow-up key sequence.

### 4. What should `SendPromptNow` mean for Cursor, where one `Enter` queues and a second `Enter` dispatches immediately?

Options presented:

- A: Model the real Cursor interaction exactly: send text, press `Enter` once to queue, then press `Enter` again to dispatch immediately.
- B: Treat Cursor `SendPromptNow` the same as queued in v1 and document that it is not distinct yet.
- C: Return an unsupported-operation error for Cursor `SendPromptNow`.
- D: Omit `SendPromptNow` from the Cursor backend even if other backends implement it.

User answer:

> 4A

Decision:

- `SendPromptNow` for Cursor should model the real CLI interaction exactly.
- The Cursor backend should send text, press `Enter` once to queue, and press `Enter` again to dispatch immediately.

### 5. Besides code comments on the Claude limitation, how visible should that limitation be in the docs?

Options presented:

- A: Document it in both code comments and the relevant design/contract docs so queued delivery semantics are explicit per backend.
- B: Keep it in code comments only; product docs can stay generic.
- C: Put it only in developer-facing design docs, not operator-facing contracts.
- D: Do not document it beyond the implementation.

User answer:

> 5A

Decision:

- The Claude queued-send limitation should be documented in both code comments and the relevant design/contract docs.
- Backend-specific queued delivery semantics should be explicit in the design.

## Round 2

### 6. Once `mkpipe` switches to queued delivery, what should happen on delivery failures?

Options presented:

- A: Keep the current failure contract: before attach/bootstrap, queued delivery failures are fatal; after attach begins, log the failure, drop that message, and keep listening with no retries or in-memory queue.
- B: Any queued delivery failure should become fatal even after attach begins.
- C: Retry failed queued deliveries once in memory before logging failure.
- D: Pause the listener after a queued delivery failure and require human intervention.

User answer:

> 6A

Decision:

- Keep the current failure contract for queued delivery.
- Before attach/bootstrap, queued delivery failures are fatal.
- After attach begins, the runtime logs the failure, drops that message, and keeps listening with no retries or in-memory queue.

### 7. What exact Claude queued-emulation wrapper should the harness standardize on?

Options presented:

- A: Use an exact standardized wrapper like `Do this after all your pending tasks:\n\n<prompt>` and document it as cooperative emulation rather than true CLI queueing.
- B: Use a single-line wrapper `Do this after all your pending tasks: <prompt>`.
- C: Use a stronger prefixed wrapper like `[Queued follow-up] Do this after all your pending tasks:\n\n<prompt>`.
- D: Do not standardize the wording; leave it implementation-defined.

User answer:

> 7A

Decision:

- Standardize the Claude queued-emulation wrapper as:

  `Do this after all your pending tasks:\n\n<prompt>`

- Document it as cooperative emulation rather than true CLI queueing.

### 8. Which layer should own the backend-specific mapping from `Now`/`Queued` to key sequences or emulation?

Options presented:

- A: Each backend adapter owns how `SendPromptNow` and `SendPromptQueued` turn into `SendText` plus `PressKey` sequences; the runtime just chooses which semantic method to call.
- B: The runtime owns all key-sequence logic centrally based on backend name.
- C: The tmux package should expose high-level `SendPromptNow` and `SendPromptQueued` directly.
- D: Command packages should decide the backend-specific key behavior.

User answer:

> 8A

Decision:

- Each backend adapter owns how `SendPromptNow` and `SendPromptQueued` map to `SendText` plus `PressKey` sequences or emulation.
- The runtime remains backend-agnostic and chooses only the semantic method to invoke.

### 9. How should `implement-with-reviewer` seed its two initial bootstrap prompts in v1?

Options presented:

- A: Send both seeded bootstrap prompts with `SendPromptNow` explicitly so their current bootstrap timing remains immediate, while later peer-to-peer `mkpipe` traffic uses queued semantics.
- B: Send both seeded bootstrap prompts with `SendPromptQueued` for consistency.
- C: Keep a private deprecated `SendPrompt` path just for bootstrap.
- D: Bypass runtime methods and inject the bootstrap prompts directly through tmux.

User answer:

> 9A

Decision:

- `implement-with-reviewer` should send both seeded bootstrap prompts with `SendPromptNow`.
- Later peer-to-peer `mkpipe` traffic should use queued semantics.

### 10. What codebase layer should this design target first, given the repo currently implements `agentruntime` rather than the earlier planned public `session` package?

Options presented:

- A: Target the current `orchestrator/internal/agentruntime`, backend adapters, tmux pane abstraction, and affected command contracts now, while defining semantics that a future `session` package can adopt later.
- B: Write the design only for the future `orchestrator/session` package and ignore the current runtime layout.
- C: Split the design evenly between current `agentruntime` and future `session` package contracts as co-equal implementation targets.
- D: Defer this feature until the session-package refactor lands.

User answer:

> 10A

Decision:

- Target the current `orchestrator/internal/agentruntime`, backend adapters, tmux pane abstraction, and affected command contracts now.
- Define the semantics so a future `session` package can adopt them later.
