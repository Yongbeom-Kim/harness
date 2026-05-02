# Thin Concrete Agent Interface for tmux-Backed Backends

**Goal:** Simplify the current harness runtime by removing the shared session/runtime abstraction stack and replacing it with concrete tmux-backed `codex` and `claude` agent types, while moving `implement-with-reviewer` orchestration back into its command package.

**Status:** Reviewed design input complete; ready for implementation planning.

## Summary

The current runtime splits backend behavior across `internal/agent`, `internal/agent/session`, `internal/agent/session/driver`, `internal/agent/session/launcher`, and `internal/reviewloop`. This makes the main abstraction harder to understand because the thing workflows actually care about is not "a session runtime" but "a concrete backend running in tmux that can be started, waited on, prompted, and captured."

This refactor changes the center of gravity:

- `internal/agent` becomes the primary runtime package
- `CodexAgent` and `ClaudeAgent` remain separate concrete types
- reusable tmux mechanics remain in `internal/agent/tmux`
- `implement-with-reviewer` owns its own loop, prompt shaping, done-marker polling, capture slicing, artifacts, session naming, and side-channel orchestration

The design is intentionally not DRY-first. The first goal is to make the runtime legible. Shared code is allowed only where it remains obviously infrastructural.

## Goals

- Remove the shared runtime/session abstraction as the primary control surface for workflows.
- Keep separate concrete `CodexAgent` and `ClaudeAgent` implementations.
- Make the concrete backend method set correspond to direct tmux-backed CLI actions.
- Move `<promise>done</promise>` completion detection, turn slicing, role prompt shaping, and implementer/reviewer loop logic into `cmd/implement-with-reviewer`.
- Keep reusable infrastructure narrow: tmux helpers, file channels, and directory locking.
- Preserve the existing command-line UX for `implement-with-reviewer`, `tmux_codex`, and `tmux_claude`.
- Remove `tmux_agent`.

## Non-Goals

- No new generic backend registry or shared runtime interface.
- No attempt to fully deduplicate Codex and Claude behavior in this refactor.
- No redesign of the external `implement-with-reviewer` flag surface or transcript format beyond what is required by the internal ownership change.
- No new startup prompt phase.
- No new operator-facing tmux flags.

## Concrete Backend Contract

Each concrete backend type exposes a thin action-level API:

- `Start() error`
- `WaitUntilReady() error`
- `SendPrompt(prompt string) error`
- `Capture() (string, error)`
- `SessionName() string`
- `Close() error`

### Behavioral meaning

- `Start()` creates the tmux session for the backend, creates or claims the expected pane, and launches the backend CLI in that pane.
- `Start()` fails immediately if the target tmux session already exists.
- `WaitUntilReady()` uses backend-specific heuristics to detect when the CLI is ready for input.
- `SendPrompt()` is raw text submission to the pane. It does not add role instructions, markers, or done-marker instructions on its own.
- `Capture()` returns the full raw tmux pane capture at the moment it is called.
- `Close()` tears down the owning tmux session.

There is no dedicated startup prompt. The command layer owns all semantic prompt construction.

## Command Ownership

`cmd/implement-with-reviewer` becomes the owner of:

- backend selection from CLI flags
- per-run per-role tmux session naming
- role prompt text
- reviewer approval detection
- `<promise>done</promise>` instruction injection
- unique turn markers
- full-pane capture slicing for the active turn
- idle-time polling for turn completion
- side-channel wrapping and routing
- runtime artifact schema and writing
- implementer/reviewer iteration control

The old `internal/reviewloop` package is no longer the runtime control center.

## Prompt Model

There is no startup prompt.

Instead, every workflow turn prompt is built by the command package and includes:

- the role contract for implementer or reviewer
- the turn-specific task/rewrite/review body
- the done-marker instruction for normal turn execution

Side-channel messages remain supported, but they are not decorated with role instructions, markers, or done-marker instructions.

## Completion Model

`Capture()` returns the full pane history, so the command must isolate the current turn itself.

The command does that by:

- generating a unique marker per turn
- prefixing the decorated prompt with that marker
- polling `Capture()` until the current turn slice ends with `<promise>done</promise>`

Side-channel traffic may appear in the pane during a turn. The main turn isolation logic must be marker-based so those messages do not corrupt completion detection.

## Session Naming

The command generates explicit per-run per-role session names, for example:

- `iwr-<run-id>-implementer`
- `iwr-<run-id>-reviewer`

The concrete backend constructors receive those names explicitly.

This avoids collisions by construction and supports the `Start()` fast-fail-on-existing-session rule.

## Launcher Commands

`tmux_codex` and `tmux_claude` remain supported as thin wrappers over the same concrete backend types used by the workflow.

`tmux_agent` is removed.

Launcher success should mean the backend was started successfully in tmux and is ready for input.

## Reusable Infrastructure Boundary

Reusable infrastructure that remains outside the command package:

- `internal/agent/tmux`
- `internal/filechannel`
- `internal/dirlock`

The tmux helper package remains responsible for running the `tmux` binary, creating sessions and panes, attaching, killing sessions, sending text, and capturing pane scrollback.

## Error Model

Even though the workflow now talks to concrete backend types instead of a shared runtime interface, the agent layer keeps one small shared typed error model for:

- launch failures
- readiness failures
- capture failures
- close failures

This preserves structured failure handling without restoring a shared runtime interface.
