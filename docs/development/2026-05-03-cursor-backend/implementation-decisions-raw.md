# Cursor Backend Implementation Decision Log

Date: 2026-05-03
Topic: implementation architecture for the Cursor tmux backend and launcher

Inputs reviewed:

- `docs/development/2026-05-03-cursor-backend/design-document.md`
- `docs/development/2026-05-03-cursor-backend/decisions-raw.md`
- `docs/development/2026-05-03-session-package-refactor/implementation-spec.md`
- current `orchestrator/session/*`, `orchestrator/internal/session/backend/*`, `orchestrator/cmd/tmux_codex/*`, `orchestrator/cmd/tmux_claude/*`, and `Makefile`

Repository baseline:

- `cd orchestrator && go test ./...` passes as of 2026-05-03

## Round 1

Note: the question numbers in this round are recorded as `15` through `18` to preserve the user's compact reply mapping (`15A16A17A18A`).

### 15. How should the new launcher be added at the command layer?

Options presented:

- A: Add a standalone `orchestrator/cmd/tmux_cursor/{main.go,main_test.go}` that mirrors the current `tmux_codex` / `tmux_claude` thin-adapter pattern, and do not refactor a shared CLI helper now.
- B: Add `tmux_cursor`, but also extract a shared internal helper and refactor all three launcher packages to use it.
- C: Keep three separate binaries, but share only argument parsing across the command packages.
- D: Avoid a new command package and have `tmux_cursor` be a thin wrapper around one of the existing launchers.

User answer:

> 15A

Decision:

- Add a standalone `orchestrator/cmd/tmux_cursor` package.
- Mirror the current thin-adapter pattern used by `tmux_codex` and `tmux_claude`.
- Do not introduce a shared CLI helper package in this feature.

### 16. Where should the Cursor-specific backend behavior live?

Options presented:

- A: Create `orchestrator/internal/session/backend/cursor.go` that owns `DefaultSessionName`, `Launch`, `WaitUntilReady`, `SendPrompt`, and a local `cursorReady` matcher; keep the shared quiet-period loop in `backend.go` and avoid new cross-backend helper extraction in this feature.
- B: Put `cursor.go` in place, but also extract shared interstitial-rejection helpers into `backend.go` and update Codex/Claude to use them now.
- C: Keep launch command in `backend/cursor.go`, but move readiness matching into `orchestrator/session/session.go`.
- D: Reuse `Claude`’s matcher and only change the launch command plus default session name.

User answer:

> 16A

Decision:

- Create `orchestrator/internal/session/backend/cursor.go`.
- Keep Cursor-specific launch and readiness behavior local to that file.
- Reuse the existing shared quiet-period loop in `backend.go` without refactoring Codex or Claude in this feature.

### 17. How broad should the existing contract-doc updates be?

Options presented:

- A: Add `orchestrator/cmd/tmux_cursor/CONTRACT.md`, and update `tmux_codex/CONTRACT.md` plus `tmux_claude/CONTRACT.md` anywhere their shared-surface wording would become false after Cursor lands, not just the top launcher list.
- B: Add the new Cursor contract doc, but only update the “supported binaries” bullet list in the existing Codex/Claude docs.
- C: Replace the three per-launcher docs with one new shared launcher contract doc now.
- D: Leave the existing Codex/Claude docs unchanged and document Cursor only in its new contract doc.

User answer:

> 17A

Decision:

- Add a dedicated `orchestrator/cmd/tmux_cursor/CONTRACT.md`.
- Update the existing Codex and Claude contract docs anywhere they would otherwise still imply a two-launcher surface.
- Keep the per-launcher contract-doc structure rather than replacing it with a shared contract file.

### 18. How should Cursor test ownership be split?

Options presented:

- A: Keep the current layered pattern: `backend/backend_test.go` owns Cursor launch/prompt/readiness cases, `session/session_test.go` adds `NewCursor` constructor/default-name coverage, and `cmd/tmux_cursor/main_test.go` owns CLI parsing, banners, exit codes, and nil-constructor guard behavior.
- B: Put most Cursor behavior in `cmd/tmux_cursor/main_test.go` and keep backend/session tests minimal.
- C: Add real-CLI integration tests that invoke `agent` as the main verification path.
- D: Rely on manual verification for readiness and keep only a couple of smoke tests.

User answer:

> 18a

Decision:

- Keep the existing layered test ownership split.
- Add Cursor backend behavior to `orchestrator/internal/session/backend/backend_test.go`.
- Add only constructor/default-name coverage to `orchestrator/session/session_test.go`.
- Put CLI parsing, output, exit-code, and constructor-guard coverage in `orchestrator/cmd/tmux_cursor/main_test.go`.
