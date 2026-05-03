# Implement-With-Reviewer Implementation Decisions Log

Date: 2026-05-03
Topic: implementation architecture for `implement-with-reviewer` and the `internal/agentruntime` refactor

## Context

- The reviewed product design lives in `design-document.md`; the product-design Q&A log lives in `decisions-raw.md`.
- The current codebase is still on the pre-feature shape:
  - single-agent launchers live under `orchestrator/cmd/tmux_codex`, `tmux_claude`, and `tmux_cursor`
  - the reusable runtime package is still `orchestrator/internal/session`
  - that package currently creates tmux sessions, allocates panes, owns attach-scoped mkpipe listeners, and closes whole tmux sessions from runtime `Close()`
- Existing tmux pane management already supports `NewPane()` returning the default pane first and `split-window` for later panes.
- The repo baseline is clean for this scope: `cd orchestrator && go test ./...` passed on 2026-05-03 before implementation-plan work started.

## Round 1

### 1. After `internal/session` is renamed and runtimes stop owning locks, where should current-directory lock acquisition live for all four commands?

Options presented:

- A: Have each command import `orchestrator/internal/agentruntime/dirlock` directly and wrap tmux-session bootstrap/attach with a command-owned lock helper; runtime constructors receive no lock policy. (Recommended)
- B: Keep a `CurrentDirectoryLockPolicy()` helper on `internal/agentruntime` even though the runtime no longer acquires locks itself.
- C: Move lock acquisition into `orchestrator/internal/agentruntime/tmux` because tmux-session creation is the protected resource.
- D: Add a brand-new `orchestrator/internal/commandutil` package just for lock wiring.

User answer:

> 1A, let's move dirlock down to internal/dirlock, since the agentruntime doesn't own it anymore.

Decision:

- Lock acquisition should move to the command layer.
- The reusable current-directory lock implementation should live in `orchestrator/internal/dirlock`, not under `internal/agentruntime`.

### 2. Where should the bootstrap flow for `implement-with-reviewer` itself live?

Options presented:

- A: Keep it in `orchestrator/cmd/implement-with-reviewer/`, with `main.go` handling CLI/bootstrap sequencing and one small support file for prompt/protocol/session-name helpers. (Recommended)
- B: Add `orchestrator/internal/workflow/implementwithreviewer` and keep the command as a thin shim.
- C: Expand `orchestrator/internal/agentruntime` to own multi-runtime bootstrap orchestration directly.
- D: Keep the workflow mostly in shell/Makefile and use Go only for the existing launchers.

User answer:

> 2A

Decision:

- `implement-with-reviewer` should keep its bootstrap flow in `orchestrator/cmd/implement-with-reviewer/`.
- The command package may add one small support file for prompt/protocol/session-name helpers, but does not need a new internal workflow package.

### 3. How aggressively should this refactor deduplicate `tmux_codex`, `tmux_claude`, and `tmux_cursor` while moving ownership up to the command layer?

Options presented:

- A: Refactor the three command packages in place and keep them separate; only extract a tiny helper if the post-refactor duplication is exact and low-risk. (Recommended)
- B: Introduce a shared `orchestrator/internal/launcher` package and rewrite all four commands onto it now.
- C: Replace the three launchers with one generated/template-driven command surface.
- D: Leave the single-agent launchers on the old ownership model and refactor only `implement-with-reviewer`.

User answer:

> 3A

Decision:

- The three single-agent launcher commands should be refactored in place.
- Avoid a broad launcher-framework extraction unless a later implementation step reveals exact low-risk shared code.

### 4. Once commands own attach-scoped mkpipe lifecycle, where should the listener-forwarding helper live?

Options presented:

- A: Add a small reusable helper under `orchestrator/internal/agentruntime/mkpipe` that starts forwarding to a provided `SendPrompt` callback, exposes the resolved path, and returns a cleanup handle; commands still decide when to start/stop it. (Recommended)
- B: Duplicate the forwarding goroutines in each command package and keep `mkpipe` as `Start(...)` plus raw channels only.
- C: Keep forwarding inside `orchestrator/internal/agentruntime` and have commands call back into runtime-managed attach helpers.
- D: Add a top-level exported forwarding package outside `internal/agentruntime`.

User answer:

> Asked follow-up: why does the command own the attach-scoped mkpipe lifecycle? I don't think it should?

Decision in progress:

- Superseded by Round 2 decisions.
- Because mkpipe lifecycle is runtime-owned, the raw `mkpipe` package should stay a listener primitive and the runtime layer should own the forwarding goroutines and listener cleanup.

### 5. What tmux API should runtime `Close()` use so it only shuts down its own pane/backend?

Options presented:

- A: Extend `tmux.TmuxPaneLike` with a pane-level close method implemented via `tmux kill-pane`; `TmuxSessionLike.Close()` remains whole-session cleanup for commands. (Recommended)
- B: Keep `TmuxPaneLike` read/write-only and add `ClosePane(pane)` on `TmuxSessionLike`.
- C: Leave pane close unsupported and let runtime `Close()` continue calling whole-session `Close()`.
- D: Drop runtime `Close()` from the plan and rely on operator/manual cleanup only.

User answer:

> 5A

Decision:

- Add pane-close support to the tmux layer.
- Runtime `Close()` should close only its own pane/backend via pane-level close support and must never kill the whole tmux session.

### 6. Where should the exact marker constants and seeded implementer/reviewer prompt builders live?

Options presented:

- A: In `orchestrator/cmd/implement-with-reviewer/prompt.go` (or `protocol.go`) as code constants and tested builder functions. (Recommended)
- B: Inline the full prompt strings inside `main.go`.
- C: Store prompt templates as external markdown/text assets and load them at runtime.
- D: Put prompt generation in `orchestrator/internal/agentruntime/backend` because the prompts mention backends.

User answer:

> 6A

Decision:

- Exact protocol markers and seeded implementer/reviewer prompt builders should live in `orchestrator/cmd/implement-with-reviewer/` as code constants and tested builder helpers.

### 7. Where should `codex|claude|cursor` parsing for `--implementer` and `--reviewer` live?

Options presented:

- A: Keep the string-to-constructor mapping local to `orchestrator/cmd/implement-with-reviewer`, returning constructor/label pairs without exporting a new enum. (Recommended)
- B: Add an exported `BackendKind` parser on `orchestrator/internal/agentruntime`.
- C: Add parsing helpers to `orchestrator/internal/agentruntime/backend`.
- D: Pass raw strings through the whole bootstrap path and branch at every callsite.

User answer:

> 7A

Decision:

- `codex|claude|cursor` parsing for `--implementer` and `--reviewer` should stay local to `orchestrator/cmd/implement-with-reviewer`.
- Do not introduce a new exported backend enum/parser surface for this workflow.

### 8. How should the new workflow command be structured for automated tests?

Options presented:

- A: Follow the current launcher pattern: keep narrow command-local interfaces/dependency hooks in `cmd/implement-with-reviewer` for tmux session, runtime construction, attach, and mkpipe forwarding so ordering and cleanup can be unit-tested deterministically. (Recommended)
- B: Use concrete `tmux` and `agentruntime` types directly and rely mostly on manual smoke tests.
- C: Export reusable fake/test interfaces from `internal/agentruntime`.
- D: Skip unit tests for bootstrap ordering and cover only CLI argument parsing.

User answer:

> 8A

Decision:

- `implement-with-reviewer` should follow the current launcher testing style.
- Keep command-local interfaces and dependency hooks so ordering, cleanup, and bootstrap failures are unit-testable without concrete tmux processes.

## Round 2

### 9. If mkpipe remains attach-scoped but should not be command-owned, what should own each listener lifecycle?

Options presented:

- A: Each `agentruntime.Runtime` owns its own mkpipe listener lifecycle through explicit start/stop methods (or equivalent), while commands decide only when to invoke those methods during attach/bootstrap sequencing. (Recommended)
- B: Add a separate `orchestrator/internal/mkpipebridge` helper that owns listeners as standalone objects so neither commands nor runtimes own them directly.
- C: Keep command ownership after all, because attach start/stop already brackets listener lifetime naturally.
- D: Revisit the attach-scoped constraint and make mkpipe detach-safe plus runtime-owned.

User answer:

> 9A

Decision:

- Mkpipe listener lifecycle should be owned by each `agentruntime.Runtime`, not by the command.
- Commands decide when to start or stop the runtime-owned attach-scoped listeners during bootstrap and attach teardown.

### 10. Once the package path becomes `internal/agentruntime`, should the top-level reusable type stay `Session` or become `Runtime`?

Options presented:

- A: Rename the top-level type to `Runtime` because commands will now manipulate both tmux sessions and agent runtimes in the same codepaths, so `Session` becomes directly ambiguous. (Recommended)
- B: Keep the type name `Session` and rely on the package path to disambiguate.
- C: Keep `Session` internally but alias it behind command-local interfaces only.
- D: Rename it to `Agent`.

User answer:

> 10A

Decision:

- The top-level reusable type under `internal/agentruntime` should be renamed from `Session` to `Runtime`.
- This avoids direct ambiguity once commands manipulate both tmux sessions and agent runtimes in the same flow.

### 11. How should the shared-session bootstrap sequence start the two runtimes before mkpipe and prompt seeding?

Options presented:

- A: Start implementer runtime to readiness, then reviewer runtime to readiness, then start both mkpipe listeners, then send both seeded prompts. This stays deterministic and simplest to test. (Recommended)
- B: Start both runtimes concurrently, wait for both ready, then start both mkpipe listeners and seed prompts.
- C: Start reviewer first, then implementer, then mkpipe, then prompts.
- D: Start implementer, create its mkpipe immediately, then bring up reviewer.

User answer:

> 11. Okay the agent runtime should own the mkpipe lifecycle, so the orchestrated will start both the implemented and review run times and then after starting, it should get the mkpipe paths, then seed the prompts.

Decision:

- The orchestrator should start both implementer and reviewer runtimes to readiness first.
- After both runtimes are started, the orchestrator obtains the mkpipe paths from the two runtimes and only then seeds the initial prompts.
- Prompt seeding must therefore happen after both runtimes are live and after both runtime-owned attach-scoped mkpipe listeners are started.

### 12. What tmux-session name generation shape should `implement-with-reviewer` use in v1?

Options presented:

- A: Use a dedicated helper that generates a tmux-safe unique name with a fixed `implement-with-reviewer-` prefix plus timestamp-derived uniqueness; tests assert prefix, non-empty value, and tmux-safe sanitization rather than an exact timestamp. (Recommended)
- B: Use a UUID-only name with no human-readable prefix.
- C: Include both backend names in the generated session name, such as `codex-claude-<ts>`.
- D: Use the current directory basename plus timestamp.

User answer:

> 12A

Decision:

- `implement-with-reviewer` should use a dedicated helper that generates a tmux-safe unique session name with an `implement-with-reviewer-` prefix plus timestamp-derived uniqueness.
- Tests should assert the prefix, non-empty uniqueness suffix, and tmux-safe sanitization rather than an exact timestamp string.
