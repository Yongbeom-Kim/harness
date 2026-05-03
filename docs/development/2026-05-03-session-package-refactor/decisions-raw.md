# Session Package Refactor Decision Log

Date: 2026-05-03
Topic: shared session abstraction for tmux-backed coding agents

## Round 1

### 1. What should be the scope of this refactor?

Options presented:

- A: Build a shared `internal/session/*` plus a coordinating `session` package that fully owns launch, readiness wait, optional mkpipe, and optional attach for `tmux_codex` and `tmux_claude`, while leaving broader workflow orchestration for later.
- B: Build `session` as the foundation for both current launchers and an immediate multi-agent/workflow layer in the same refactor.
- C: Limit this change to moving files under `internal/session/*` and keep most orchestration in `cmd/*`.
- D: Skip the new `session` package and only deduplicate the two `main.go` files.

User answer:

> 1A

Decision:

- The refactor should introduce shared `internal/session/*` packages plus a coordinating `session` package.
- The new session layer should own launcher lifecycle concerns for the two current binaries.
- Broader multi-agent or workflow orchestration remains out of scope for this design.

### 2. How should backend-specific behavior be modeled inside the new design?

Options presented:

- A: Use one shared session runtime with an injected backend descriptor for Codex vs Claude: launch command, ready matcher, default session name, labels.
- B: Keep separate `CodexSession` and `ClaudeSession` concrete types with duplicated orchestration.
- C: Keep current `agent.CodexAgent` and `agent.ClaudeAgent` mostly intact and just wrap them from a thin `session` package.
- D: Collapse everything into one hardcoded generic agent type now and ignore backend-specific tuning.

User answer:

> 2A

Decision:

- Use one shared session runtime with backend-specific behavior injected through a backend descriptor.
- Backend-specific data should include launch command, readiness matcher, default session naming, and user-facing labels.

### 3. How much lifecycle orchestration should the new `session` layer own?

Options presented:

- A: It should own the whole session lifecycle boundary: backend startup, readiness wait, tmux handling, optional mkpipe wiring, and attach handoff, while letting callers choose policy knobs.
- B: It should own startup only; `cmd/*` should still handle mkpipe, signals, and attach.
- C: It should own only tmux primitives and leave all orchestration above it.
- D: It should be mostly a data model package with little behavior.

User answer:

> 3A

Decision:

- The `session` layer should own the full launcher lifecycle boundary.
- CLI entrypoints should become thin adapters that configure and invoke this lifecycle rather than reproducing it.

### 4. What should happen to the current working-directory lock?

Options presented:

- A: Treat locking as an optional policy that CLI entrypoints apply, not something every `session` call enforces internally.
- B: Move it under `internal/session/dirlock` and make every new session creation acquire it automatically.
- C: Replace it with a session-name-scoped lock so multiple sessions per repo are allowed immediately.
- D: Remove locking and rely only on tmux session-name collisions.

User answer:

> 4move it to internal/session/dirlock, and treat it as an optional locking policy that is set when a new session struct instance object is created.

Decision:

- Move the lock package under `internal/session/dirlock`.
- Locking remains optional rather than universally enforced.
- The lock policy should be configured as part of session construction/configuration instead of being hardcoded inside every command path.
- Follow-up required: decide the exact session-construction API for optional policies such as locking.

### 5. What should happen to the operator-facing CLI surface?

Options presented:

- A: Keep `tmux_codex` and `tmux_claude` as the stable commands and make them thin adapters over the new `session` package.
- B: Add a new shared `tmux_session` or `tmux_agent` command and keep the existing two as wrappers for now.
- C: Replace both commands with one shared command immediately.
- D: Do only the library refactor now and postpone CLI shape changes.

User answer:

> 5A

Decision:

- Keep `tmux_codex` and `tmux_claude` as the stable operator-facing binaries.
- Refactor them into thin adapters over the new shared session package.

### 6. Where should the new coordinating package live?

Options presented:

- A: Create `orchestrator/session` as the package consumed by `cmd/*`, backed by `orchestrator/internal/session/*`.
- B: Keep everything under `orchestrator/internal/session` and let `cmd/*` call that directly.
- C: Rename `internal/agent` to `internal/session` and do not add a separate top-level `session` package.
- D: Keep the current package names and add only a small helper package near `cmd/*`.

User answer:

> 6A

Decision:

- Create a top-level `orchestrator/session` package for command consumption.
- Back it with lower-level implementation packages under `orchestrator/internal/session/*`.

## Round 2

### 7. What should the public `orchestrator/session` API look like for the CLI entrypoints?

Options presented:

- A: Expose a typed config plus a session object, such as `session.New(config)` returning a handle with methods like `Start`, `Attach`, `SendPrompt`, `Capture`, and `Close`. This matches the “new session struct instance” direction and keeps the commands thin.
- B: Expose only one-shot helpers like `session.RunCodexAttached(...)` and `session.RunClaudeAttached(...)`.
- C: Expose only package-level functions around opaque session IDs, with no exported session object.
- D: Expose a builder-style fluent API with chained options and no simple config struct.

User answer:

> 7A

Decision:

- The public package should expose a typed config and a session object.
- CLI entrypoints should construct a session instance and invoke lifecycle methods on that object.
- The public handle should support operations such as start, attach, prompt sending, capture, and close.

### 8. If the requested tmux session name already exists when creating a new session, what should v1 do?

Options presented:

- A: Fail clearly and leave the existing session untouched; reopening or reusing existing sessions can be an explicit later feature.
- B: Automatically attach to the existing session.
- C: Automatically reuse the existing session and skip startup/readiness flow when possible.
- D: Kill the existing session and recreate it.

User answer:

> 8A

Decision:

- New-session creation should fail clearly if the requested tmux session name already exists.
- Reopen/reuse behavior is not part of v1 of this refactor.

### 9. Should the new session layer support an optional startup prompt as part of session creation?

Options presented:

- A: Yes. Allow an optional `InitialPrompt` that is sent only after backend readiness and before optional attach; the CLI can choose whether to expose it now.
- B: No. Session creation should stop at readiness, and callers should always send prompts in a separate step.
- C: Only support startup prompts through mkpipe, not through session config.
- D: Require a startup prompt for every created session.

User answer:

> 9B

Decision:

- Session creation ends at backend readiness.
- Prompt sending remains a separate explicit operation after session creation.
- The design should avoid baking an initial prompt into the constructor/startup path.

### 10. What should happen when `Attach` returns successfully?

Options presented:

- A: Clean up only transient resources owned by the launcher process, such as mkpipe listeners and lock handles, but leave the tmux-backed backend session running until someone explicitly closes it.
- B: Automatically close the backend session when attach returns.
- C: Make close-on-attach-return configurable, with auto-close as the default.
- D: Keep even the mkpipe listener alive after the CLI exits by introducing a detached helper.

User answer:

> 10A

Decision:

- Attach return should clean up only transient launcher-owned resources.
- The tmux-backed backend session remains alive after attach returns.
- Explicit close remains a separate action.

### 11. How should the optional lock policy be expressed on the session object?

Options presented:

- A: As a pluggable policy in session config, with the current working-directory lock provided as one built-in implementation.
- B: As a simple boolean like `UseWorkingDirLock`.
- C: As an implicit backend default rather than explicit config.
- D: Outside the session package entirely; callers must acquire locks before session construction.

User answer:

> 11A

Decision:

- Represent locking as a pluggable policy in session configuration.
- Provide the current working-directory lock as a built-in implementation.
- Keep lock policy explicit and session-scoped rather than implicit.

## Round 3

### 12. Which current lower-level packages should be absorbed under `internal/session/*` as part of this refactor?

Options presented:

- A: Move the session-lifecycle-specific packages under `internal/session/*`: `agent/tmux` -> `session/tmux`, `agent/env` -> `session/env`, `dirlock` -> `session/dirlock`, and `mkpipe` -> `session/mkpipe`; dissolve the old `internal/agent` layer into the new runtime/backend split.
- B: Move `tmux`, `env`, and `dirlock` under `internal/session/*`, but keep `mkpipe` as a separate top-level `internal/mkpipe` package.
- C: Move only `tmux` and `env`; leave `dirlock` and `mkpipe` where they are.
- D: Keep the current lower-level package locations and add only a new wrapper package.

User answer:

> 12A

Decision:

- Consolidate session-lifecycle packages under `internal/session/*`.
- Move tmux, environment bootstrap, lock, and mkpipe under the new session-oriented internal package tree.
- Retire the old `internal/agent` package shape in favor of the new session-oriented structure.

### 13. How should backend choice be exposed from the public `orchestrator/session` package?

Options presented:

- A: Provide public constructors like `session.NewCodex(config)` and `session.NewClaude(config)`, backed internally by shared runtime plus private backend descriptors.
- B: Expose a public backend descriptor type so callers assemble Codex/Claude behavior themselves.
- C: Use one public `session.New(config)` with a string field like `Backend: "codex" | "claude"`.
- D: Hide backend choice entirely and infer it from the session name.

User answer:

> 13A

Decision:

- Expose backend choice through explicit public constructors such as `session.NewCodex(config)` and `session.NewClaude(config)`.
- Keep backend descriptors internal rather than making callers assemble backend behavior manually.

### 14. Should the public session handle expose `Capture()`?

Options presented:

- A: Yes. Keep `Capture()` on the public session handle now because it already exists conceptually in the current code and will matter for later orchestration and diagnostics.
- B: No. Keep capture internal until a user-facing command needs it.
- C: Only expose capture through a separate debug-only helper.
- D: Expose the raw tmux pane instead of a capture method.

User answer:

> 14A

Decision:

- Keep `Capture()` on the public session handle.
- Treat capture as part of the stable session control surface rather than an internal-only diagnostic.

### 15. How should mkpipe fit into the new lifecycle?

Options presented:

- A: Make mkpipe an optional session feature configured on the session object, so the session layer owns listener startup, prompt forwarding, and cleanup; `cmd/*` only maps flags into config.
- B: Keep mkpipe wiring outside the session layer; commands still start the listener manually after `Start`.
- C: Keep mkpipe completely separate from session and let future callers compose it themselves.
- D: Replace mkpipe immediately with a generic pluggable input-source framework.

User answer:

> 15A

Decision:

- Mkpipe becomes an optional session feature configured through session construction/configuration.
- The session layer owns mkpipe listener startup, prompt forwarding, and cleanup.
- CLI entrypoints should only translate flags into session config.

### 16. How should the internal runtime/backend split be organized?

Options presented:

- A: Use a shared `internal/session/runtime` for lifecycle orchestration and a small `internal/session/backend` package for backend descriptors/readiness matchers/defaults, alongside `tmux`, `env`, `dirlock`, and `mkpipe`.
- B: Put everything into one flat `internal/session` package.
- C: Keep separate `codex.go` and `claude.go` orchestration files under `internal/session`, even if most logic duplicates.
- D: Put backend descriptor logic in the public `session` package instead of internal packages.

User answer:

> 16 what is this runtime/backend split?

Status:

- Clarification requested before the decision is finalized.

## Round 4

### 16. How should the internal runtime/backend split be organized?

Clarification provided:

- `internal/session/runtime`: the generic session engine that owns shared lifecycle orchestration such as lock acquisition, tmux session creation/opening, launch command assembly, readiness polling, optional mkpipe startup/cleanup, attach handoff, capture, and close behavior.
- `internal/session/backend`: the backend-specific layer that defines what differs between Codex and Claude, such as launch command, readiness matcher, default session name, and user-facing labels.

Options presented:

- A: Use `internal/session/runtime` for shared lifecycle orchestration and `internal/session/backend` for backend descriptors/readiness matchers/defaults, alongside `tmux`, `env`, `dirlock`, and `mkpipe`.
- B: Put everything into one flat `internal/session` package.
- C: Keep separate `codex.go` and `claude.go` orchestration files under `internal/session`, even if most logic duplicates.
- D: Put backend descriptor logic in the public `session` package instead of internal packages.

User answer:

> 16A

Decision:

- Use a shared `internal/session/runtime` package for lifecycle orchestration.
- Use a separate `internal/session/backend` package for backend descriptors, readiness matchers, default names, and labels.
- Keep the public `orchestrator/session` package focused on exported constructors and the session handle rather than backend internals.

### 17. When should the optional lock policy be held?

Options presented:

- A: Acquire it before `Start`, and release it after startup completes for detached launches or after `Attach` returns for attached runs; do not hold the lock for the full tmux session lifetime.
- B: Hold it until explicit `Session.Close()`.
- C: Hold it only around tmux `new-session`.
- D: Make callers release it manually through a separate API.

User answer:

> 17A

Decision:

- Acquire the optional lock policy before session startup begins.
- For detached launches, release the lock after startup completes successfully or fails.
- For attached launches, hold the lock through attach and release it when attach returns.
- Do not hold the lock for the full tmux session lifetime after the launcher process exits.

### 18. If mkpipe is enabled in config but no attach occurs, what should the session API do?

Options presented:

- A: Return a clear error; mkpipe remains attach-only even at the library API.
- B: Silently ignore mkpipe unless `Attach()` is called.
- C: Start a headless listener anyway.
- D: Auto-attach when mkpipe is enabled.

User answer:

> 18A

Decision:

- Mkpipe remains attach-only at the session API level.
- If config requests mkpipe without an attached run path, the session layer should return a clear error rather than silently downgrade behavior.
