# Implementation Decisions

## Context

- Request: introduce a new tmux abstraction with `TmuxLike`, `TmuxSession`, and `TmuxPane`, then inject the tmux abstraction into the `claude` / `codex` session implementations.
- Existing relevant code:
  - `orchestrator/internal/cli/command.go`
  - `orchestrator/internal/cli/codex.go`
  - `orchestrator/internal/cli/claude.go`
  - `orchestrator/internal/cli/interface.go`
  - `orchestrator/internal/cli/factory.go`
  - `orchestrator/internal/implementwithreviewer/runner.go`
- Existing design context:
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/design-document.md`
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/implementation-spec.md`

## 2026-04-27 Question Round 1

1. Where should the new tmux abstraction live?
- Answer: `B`
- Interpretation: create a dedicated `orchestrator/internal/tmux/` package for tmux-facing types instead of extending `orchestrator/internal/cli`.

2. How should `codex` / `claude` receive the new tmux dependency?
- Answer: `B`
- Interpretation: change the public `cli.NewSession(...)` factory shape so callers provide a `TmuxLike` dependency explicitly.

3. What should this refactor do about run topology right now?
- Answer: `A`
- Interpretation: preserve today’s behavior where each backend session owns its own tmux session, but model `TmuxSession` as multi-pane-capable for future work.

4. Which API boundary should stay stable after this change?
- Answer: `A`
- Interpretation: keep the external `cli.Session` contract as `Start`, `RunTurn`, `SessionName`, and `Close`; only the internal implementation becomes tmux-abstraction based.

5. What test scope should this change target?
- Answer: `A`
- Interpretation: add focused unit tests around fake tmux abstractions and update existing adapter tests without adding a new real-`tmux` integration suite in this change.

## Follow-Up Needed

- The remaining design question is whether `TmuxLike` should represent:
  - a session-scoped facade that can expose and manage one or more panes, or
  - a pane-scoped transport interface injected into backend sessions while `TmuxSession` owns topology and lifecycle.

## 2026-04-27 Question Round 2

6. What should `TmuxLike` represent?
- Answer: `A`
- Interpretation: `TmuxLike` should be a pane-scoped transport interface. `TmuxSession` owns session lifecycle and topology, and `TmuxPane` values implement `TmuxLike`.

7. If `cli.NewSession(...)` now requires a tmux dependency, who should create the default tmux object in production code?
- Answer: `C`
- Interpretation: keep `implementwithreviewer.RunConfig.NewSession` as the seam, and have production wiring provide a fully wired closure that creates or injects the concrete tmux dependency before calling a tmux-aware `cli.NewSession(...)`.

8. What should `SessionName()` mean after the refactor?
- Answer: `A`
- Interpretation: keep `SessionName()` returning the owning tmux session name, even if the backend session works through a pane-scoped `TmuxLike`.

## Resolved Ownership Model

- Each backend session still owns its own tmux session in the runtime model.
- The concrete backend session instance should work through a pane-scoped `TmuxLike`.
- Session creation remains outside the runner by using the existing `RunConfig.NewSession` seam to inject tmux-aware production wiring.
- `SessionName()` continues to report the owning tmux session name so artifact metadata remains unchanged.
- `TmuxSession` should own lifecycle and identity concerns such as close and attach-target behavior; `TmuxLike` should stay focused on pane-local transport operations such as send, capture, and reset.
