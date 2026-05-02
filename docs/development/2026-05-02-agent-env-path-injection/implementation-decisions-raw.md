# Raw Implementation Decisions - Agent Env PATH Injection

## 2026-05-02 Project Context

- Design inputs reviewed:
  - `docs/development/2026-05-02-agent-env-path-injection/design-document.md`
  - `docs/development/2026-05-02-agent-env-path-injection/decisions-raw.md`
- Current implementation surface reviewed:
  - `orchestrator/internal/agent/shell/shell.go`
  - `orchestrator/internal/agent/shell/shell_test.go`
  - `orchestrator/internal/agent/codex.go`
  - `orchestrator/internal/agent/claude.go`
  - `orchestrator/internal/agent/agent_test.go`
  - `orchestrator/internal/agent/errors.go`
  - `Makefile`
  - `scripts/.agentrc`
  - `scripts/.agentrc.example`
  - `orchestrator/cmd/tmux_codex/CONTRACT.md`
  - `orchestrator/cmd/tmux_codex/main.go`
  - `orchestrator/cmd/tmux_codex/main_test.go`
  - `orchestrator/cmd/tmux_claude/main.go`
  - `orchestrator/cmd/tmux_claude/main_test.go`
- Current repository shape relevant to implementation:
  - The shared launch command builder is isolated today in `orchestrator/internal/agent/shell/shell.go`.
  - Only `orchestrator/internal/agent/codex.go` and `orchestrator/internal/agent/claude.go` import that package.
  - `make setup` currently manages only `$HOME/.agentrc`; there is no checked-in `scripts/bin/` directory yet.
  - The active product contract is anchored in `orchestrator/cmd/tmux_codex/CONTRACT.md`; there is no separate checked-in `tmux_claude` contract doc.
  - Existing launcher CLI tests already cover `tmux_codex` and `tmux_claude` flags and success banners, so this feature should fit under the current launcher-only surface rather than widening it.

## 2026-05-02 Question Round 1

1. How should `orchestrator/internal/agent/env` own the new preflight + launch-string contract?
A: Replace the current helper with one entrypoint such as `BuildLaunchCommand(command string, args ...string) (string, error)` that resolves and validates `~/.agent-bin`, assembles `source + PATH export + stty + backend`, and returns the final tmux send text. `(Recommended)`
B: Keep `BuildLaunchCommand(...) string` and add a separate `ValidateAgentBin() error` helper that callers must remember to invoke first.
C: Keep `env` as quoting-only and push directory validation into `CodexAgent.Start()` / `ClaudeAgent.Start()`.
D: Do the directory check inside the emitted shell script after tmux has already started.

2. After Go-side validation, what exact PATH form should the emitted shell command prepend?
A: Inject the resolved absolute home path, so the tmux launch string uses the same directory that was validated even if `.agentrc` mutates `HOME`. `(Recommended)`
B: Inject `export PATH="$HOME/.agent-bin:$PATH"` after validating with the resolved home dir, so the command text mirrors the user-facing contract.
C: Inject both the absolute path and `$HOME/.agent-bin`.
D: Keep using a literal `~/.agent-bin` in the shell string.

3. How should the recommended `~/.agent-bin` setup live in the repo?
A: Extend the existing `setup` target inline in `Makefile` so it links `.agentrc`, creates `~/.agent-bin` when absent, refreshes it when it is already a symlink, and fails with guidance when a non-symlink exists. `(Recommended)`
B: Keep `setup` for `.agentrc` only and add a second target like `setup-agent-bin`.
C: Move the symlink logic into a new script under `scripts/` and have `make setup` call it.
D: Document `~/.agent-bin` manually and keep Makefile out of it.

4. Where should automated coverage for the feature primarily live?
A: Add focused unit tests in `orchestrator/internal/agent/env/env_test.go` for launch-string assembly and preflight validation, keep `orchestrator/internal/agent/agent_test.go` focused on error propagation/start flow, and treat `make setup` verification as an explicit manual step in the plan. `(Recommended)`
B: Put all coverage into `agent_test.go` and skip a package-local `env_test.go`.
C: Add a shell integration test for `make setup` and keep Go tests minimal.
D: Rely on manual verification only.

5. What should the committed `scripts/bin/test_echo` smoke-test command do?
A: Be a tiny executable shell script that echoes its argv, so the smoke test proves both PATH discovery and argument passing. `(Recommended)`
B: Be a fixed-string script with no arg handling.
C: Ship `test_echo` plus one or two other helper scripts in the same change.
D: Commit an empty `scripts/bin/` and prove the feature only through Go tests.

6. How should the active documentation boundary be implemented for this feature?
A: Update only the canonical shared launcher doc `orchestrator/cmd/tmux_codex/CONTRACT.md` plus this feature’s development docs; do not add a separate `tmux_claude` contract while behavior remains shared. `(Recommended)`
B: Add a new `orchestrator/cmd/tmux_claude/CONTRACT.md` mirroring Codex.
C: Put PATH injection only in a repo README or setup note.
D: Keep PATH injection undocumented in active product docs for now.

### Round 1 Answers

- Raw response: `1A 2A 3A 4A 5A 6A`
- Parsed answers:
  - Q1 = A
  - Q2 = A
  - Q3 = A
  - Q4 = A
  - Q5 = A
  - Q6 = A

## 2026-05-02 Question Round 2

7. How should the home-path resolution and `~/.agent-bin` validation logic live inside `orchestrator/internal/agent/env`?
A: Keep it package-local in one file with small unexported helpers like `resolveAgentBinDir()` and `buildShellScript(...)`; export only the single launch-command entrypoint. `(Recommended)`
B: Split the rename into multiple files immediately, such as `paths.go`, `validate.go`, and `shell.go`.
C: Put home-path resolution in `internal/agent` and leave `env` for shell quoting only.
D: Put the path-resolution helpers in the launcher commands instead of `env`.

8. When `~/.agent-bin` is invalid, how should that failure appear to callers?
A: Let `env.BuildLaunchCommand(...)` return the underlying validation error, and have `CodexAgent` / `ClaudeAgent` wrap it as the existing `launch` agent error before any tmux text is sent. `(Recommended)`
B: Add a new public `ErrorKindEnv` / `ErrorKindPreflight` to the agent error model.
C: Return raw `env` errors all the way out without wrapping them in agent errors.
D: Move the preflight to `tmux_codex` / `tmux_claude` so the agent types never see it.

9. Should this feature change `scripts/.agentrc.example`?
A: Leave it functionally unchanged; `.agentrc.example` remains backend-startup customization, while `~/.agent-bin` setup is owned by `make setup` and active launcher docs. `(Recommended)`
B: Add comments to `.agentrc.example` explaining the new `~/.agent-bin` flow.
C: Rewrite `.agentrc.example` to export PATH directly instead of relying on launcher injection.
D: Remove `.agentrc.example` because `make setup` now covers both setup concerns.

10. What should the plan treat as the canonical manual verification path for the shipped feature?
A: Run focused Go tests, run `make setup`, prove `tmux_codex`/`tmux_claude` launch with the updated contract, run `test_echo` inside a launched session, and verify clear preflight failure when `~/.agent-bin` is temporarily missing or non-directory. `(Recommended)`
B: Limit manual verification to `make setup` plus one launcher smoke test.
C: Skip manual runtime verification and trust unit tests/doc review.
D: Add only docs-only verification with no launcher runtime check.

### Round 2 Answers

- Raw response: `7A 8A 9A 10A`
- Parsed answers:
  - Q7 = A
  - Q8 = A
  - Q9 = A
  - Q10 = A

## Final Consolidated Implementation Decisions

- Introduce `orchestrator/internal/agent/env` as the only shared launch-environment package.
- Replace the old string-only helper with `BuildLaunchCommand(command string, args ...string) (string, error)`.
- Keep `env` small and file-local: package-private helpers such as `resolveAgentBinDir()` and `buildShellScript(...)` stay in a single file; only `BuildLaunchCommand(...)` is exported.
- Resolve `~/.agent-bin` from the caller’s home directory in Go, validate that the resolved path exists and is a directory, and return an error before any tmux text is sent when validation fails.
- Emit the PATH prepend using the resolved absolute path, not a literal `~` or `$HOME/.agent-bin`, so the shell command and the validated directory stay identical.
- Preserve launch order exactly as designed: source `$HOME/.agentrc`, prepend the resolved `~/.agent-bin`, disable echo with `stty -echo`, then run the quoted backend command.
- Keep failure classification unchanged at the agent boundary: `CodexAgent.Start()` and `ClaudeAgent.Start()` wrap env validation failures as existing `launch` agent errors.
- Put most automated coverage in `orchestrator/internal/agent/env/env_test.go`; keep `orchestrator/internal/agent/agent_test.go` focused on agent start-flow and launch-error propagation.
- Extend `make setup` inline in `Makefile` instead of adding a second setup command or a helper script.
- `make setup` should continue linking `$HOME/.agentrc`, create or refresh `~/.agent-bin -> <repo>/scripts/bin`, and fail with clear guidance if `~/.agent-bin` exists as a non-symlink file or directory.
- Ship exactly one committed smoke-test command at `scripts/bin/test_echo`.
- Implement `test_echo` as a tiny executable shell script that echoes its argv, so the feature proves both PATH discovery and argument passing.
- Leave `scripts/.agentrc.example` functionally unchanged; this feature does not move PATH setup into `.agentrc`.
- Update only the canonical active launcher doc `orchestrator/cmd/tmux_codex/CONTRACT.md`; do not add a new `tmux_claude` contract while launch behavior remains shared.
- The canonical manual verification path after implementation is:
  - run focused Go tests
  - run `make setup`
  - prove `tmux_codex` and `tmux_claude` start successfully with the updated launch contract
  - ask a launched backend to run `test_echo`
  - verify clear preflight failure when `~/.agent-bin` is missing or not a directory
