# Raw Decisions - Agent Env PATH Injection

- Date: 2026-05-02
- Topic: Rename the agent launch `shell` package to `env` and add repo-managed `$PATH` injection so launched agent sessions can run harness-provided scripts.
- Seed direction from user: rename `shell` into `env` (or `environment`) and inject a bunch of scripts into `$PATH` for agents to run.

## Project Context Checked Before Questions

- Current launch flow uses `orchestrator/internal/agent/shell/shell.go` to source `$HOME/.agentrc`, disable echo, and launch the backend command.
- `CodexAgent` and `ClaudeAgent` both use that shared helper.
- The repo already has a top-level `scripts/` directory containing `.agentrc` and `.agentrc.example`.
- The active launcher product surface keeps `tmux_codex` and `tmux_claude` stable, so the new feature should fit inside the existing shared launch boundary.

## Round 1

Questions asked:

1. For the current `orchestrator/internal/agent/shell` package, what should the rename target be?
A: Rename it to `env`; keep the package small and focused on launch-environment assembly at the agent boundary. `(Recommended)`
B: Rename it to `environment`; prefer explicitness over brevity even for a small internal package.
C: Keep `shell` as the package name, and only rename functions/types inside it.
D: Split responsibilities now: keep `shell` for quoting/command assembly and add a separate `env` package for PATH logic.

2. After the rename, what should that package own?
A: Own the full launch-environment boundary: sourced-shell startup, PATH injection, and command/script assembly for agent launch. `(Recommended)`
B: Own only PATH mutation; keep sourced-shell command building elsewhere.
C: Become a generic repo-wide environment package used beyond agent launch.
D: Stay minimal and only provide helpers; push PATH/script decisions into each launcher command.

3. The repo already has a top-level `scripts/` folder for `.agentrc`. Where should harness-managed executable shims live?
A: Add a dedicated committed directory at `scripts/bin/` and prepend that directory to PATH for launched agents. `(Recommended)`
B: Reuse the existing top-level `scripts/` directory directly as a PATH entry.
C: Put the shims under `orchestrator/internal/agent/...` alongside the Go code.
D: Keep the scripts outside the repo and only document the expected location.

4. How should operators configure PATH injection in V1?
A: No new operator-facing flags; always inject the harness-managed repo directory and otherwise keep today’s launcher CLI surface unchanged. `(Recommended)`
B: Add a repeatable launcher flag such as `--path-prepend <dir>`.
C: Read one extra environment variable such as `HARNESS_EXTRA_PATH`.
D: Support both a built-in repo dir and optional operator-supplied extra dirs.

5. In what order should the launch environment be assembled?
A: Source `$HOME/.agentrc` first, then prepend the harness-managed PATH entries, then start the backend command. `(Recommended)`
B: Prepend the harness-managed PATH entries first, then source `$HOME/.agentrc`.
C: Stop sourcing `$HOME/.agentrc`; the harness should own the launch environment completely.
D: Keep sourcing `$HOME/.agentrc`, but do not modify PATH unless the operator opts in separately.

6. Which launcher surfaces should receive the injected PATH?
A: Both `tmux_codex` and `tmux_claude`, through the shared agent launch helper. `(Recommended)`
B: Only `tmux_codex` for now.
C: Only `tmux_claude` for now.
D: Make it backend-specific and configurable per launcher.

7. If the harness-managed script directory is part of the product contract but is missing at runtime, what should happen?
A: Fail the launch with a clear error so the missing repo-managed tool surface is explicit. `(Recommended)`
B: Skip injection silently and continue launching the backend.
C: Warn, continue, and rely on the agent/backend to fail later if it needs those scripts.
D: Auto-create an empty directory and continue.

User response:

- Raw answer:
  - `1 A`
  - `2 A`
  - `3 A`
  - `4 A`
  - `5 A`
  - `6 A`
  - `7 A`

Parsed answers:

- Q1 = A
- Q2 = A
- Q3 = A
- Q4 = A
- Q5 = A
- Q6 = A
- Q7 = A

## Consolidated Decisions So Far

- Rename `orchestrator/internal/agent/shell` to `orchestrator/internal/agent/env`.
- Make `env` the full launch-environment boundary for agent startup rather than a narrow quoting helper.
- Add a committed repo-managed executable directory at `scripts/bin/`.
- Prepend `scripts/bin/` automatically for launched agents without adding new launcher flags in V1.
- Keep the current launcher surface stable: both `tmux_codex` and `tmux_claude` inherit the new behavior through the shared agent path.
- Assemble launch state in this order: source `$HOME/.agentrc`, prepend the harness-managed PATH entries, then start the backend command.
- Treat the presence of the injected script directory as part of the product contract and fail clearly if it is missing at runtime.

## Round 2

Questions asked:

8. What should `scripts/bin/` contain in V1?
A: Repo-managed helper commands for agents to invoke during work; it is not a replacement for backend launch wrappers like `codex`/`claude`. `(Recommended)`
B: Repo-managed helper commands plus backend wrapper commands, moving launch customization out of `.agentrc`.
C: Any personal or repo-local scripts without a defined scope boundary.
D: Leave the contents intentionally unspecified for now.

9. How should command-name collisions be handled for the prepended `scripts/bin/` directory?
A: Require repo-managed scripts to use harness-specific names and treat accidental shadowing of common system commands as out of scope/disallowed. `(Recommended)`
B: Allow intentional shadowing; if `scripts/bin/` wins, that is the product contract.
C: Keep prepending, but warn at runtime when a collision is detected.
D: Append instead of prepend when collisions are a concern.

10. How many repo-managed script directories should V1 support?
A: Exactly one fixed committed directory: `scripts/bin/`. `(Recommended)`
B: Multiple fixed directories under `scripts/**/bin`.
C: One committed directory plus one optional untracked local overlay directory.
D: A configurable list loaded from a manifest file.

11. How should the launcher treat non-executable files inside `scripts/bin/`?
A: Do not validate individual files at launch time; only executable files naturally become runnable through PATH, and the directory itself is the contract. `(Recommended)`
B: Fail launch if any file in `scripts/bin/` is not executable.
C: Warn about non-executable files but continue.
D: Ignore the directory contents entirely and treat `scripts/bin/` as a future placeholder.

12. How should the launcher locate the repo-managed `scripts/bin/` directory?
A: Resolve it from the harness checkout/binary location and treat copied or relocated standalone binaries as unsupported for this feature. `(Recommended)`
B: Resolve it from the current working directory.
C: Require an explicit environment variable pointing at the repo root.
D: Leave path resolution undefined for now.

13. Once `scripts/bin/` exists, what is the role of `$HOME/.agentrc`?
A: Keep `.agentrc` for user-specific backend startup customization; `scripts/bin/` is a separate repo-managed agent tool surface. `(Recommended)`
B: Move backend startup wrappers into `scripts/bin/` and make `.agentrc` optional.
C: Deprecate `.agentrc` entirely.
D: Let either pattern exist with no documented preference.

User response:

- Q8: "just a mock, like test_echo or something, to just test its effectiveness"
- Q9 = B
- Q10 = A
- Q11 = A
- Q12: "Let's symlink it to like ~/.agent-bin/..."
- Q13 = A

Parsed answers:

- Q8 = custom answer: V1 should ship only a minimal smoke-test script (for example `test_echo`) to prove PATH injection works, rather than a broader helper-command surface yet.
- Q9 = B
- Q10 = A
- Q11 = A
- Q12 = custom answer pending clarification: user prefers a stable injected path via a home-directory symlink such as `~/.agent-bin/...`.
- Q13 = A

## Round 3

Questions asked:

14. For V1, what should the repo actually ship under `scripts/bin/`?
A: Exactly one minimal smoke-test script such as `test_echo` to prove PATH injection works; no broader helper suite yet. `(Recommended)`
B: Two or three minimal diagnostic scripts such as `test_echo`, `print_path`, and `whoami`.
C: An empty `scripts/bin/` directory; prove the feature only through tests, not a shipped script.
D: Start with a small real helper-command set instead of a smoke test.

15. How should the stable injected path work?
A: Inject `~/.agent-bin`, and treat that path as the product contract instead of injecting the repo path directly. `(Recommended)`
B: Inject the repo’s `scripts/bin` directly, and keep `~/.agent-bin` only as an optional manual convenience.
C: Inject both `~/.agent-bin` and the repo’s `scripts/bin`.
D: Keep the path-resolution model undefined for now.

16. If `~/.agent-bin` is the injected contract, who should own creating and updating it?
A: The operator owns the symlink; the launcher verifies it exists and fails clearly if it does not. `(Recommended)`
B: The launcher should create or refresh the symlink automatically from the repo’s `scripts/bin`.
C: The launcher should create it once if missing, but never update it afterward.
D: No verification; prepend `~/.agent-bin` and let later commands fail if it is wrong.

17. If the launcher verifies `~/.agent-bin`, what should count as valid?
A: It must exist and resolve to this repo’s committed `scripts/bin`; otherwise launch fails clearly. `(Recommended)`
B: Any directory at `~/.agent-bin` is acceptable, even if it is unrelated to this repo.
C: Any symlink target is acceptable as long as it is a directory.
D: Only check that the path exists; do not validate the target.

User response:

- Q14 = A
- Q15 = A
- Q16 = A
- Q17 = B

Parsed answers:

- Q14 = A
- Q15 = A
- Q16 = A
- Q17 = B

## Decision Overrides After Round 3

- The injected runtime PATH contract is no longer the repo path `scripts/bin` directly. The injected path is `~/.agent-bin`.
- `~/.agent-bin` is operator-owned rather than launcher-managed.
- Runtime validation only requires that `~/.agent-bin` exists as a directory; it does not need to point to this repo's `scripts/bin`.
- The repo still ships `scripts/bin/` with a minimal smoke-test script, but that repo directory is now a recommended source for the operator-managed `~/.agent-bin` path rather than the enforced runtime target.

## Round 4

Questions asked:

18. What should the documented happy path be for `~/.agent-bin`?
A: Document `~/.agent-bin` as a manual symlink to this repo’s `scripts/bin` so the shipped `test_echo` works in the standard setup. `(Recommended)`
B: Document `~/.agent-bin` as any operator-managed directory, with no preferred target.
C: Document both equally with no recommended default.
D: Do not document setup yet; only describe the runtime behavior.

19. If `~/.agent-bin` exists but does not contain the repo’s shipped `test_echo`, should launcher startup still succeed?
A: Yes. Startup only validates that `~/.agent-bin` exists as a directory; the smoke-test command is for the recommended setup, not a hard runtime requirement. `(Recommended)`
B: No. Startup should also require `test_echo` to exist in `~/.agent-bin`.
C: Startup should warn but continue.
D: Leave this undefined in V1.

20. How visible should this feature be in the launcher product contract?
A: Explicitly document that launcher sessions prepend `~/.agent-bin` after sourcing `.agentrc`, so agents can run operator-managed helper scripts. `(Recommended)`
B: Keep it implementation-detail-only for now; mention it only in development docs.
C: Mention PATH injection in README/docs, but not in command-level contracts.
D: Mention it only as future direction.

User response:

- Q18 = A, with additional instruction: "and do it with the makefile"
- Q19 = A
- Q20 = A

Parsed answers:

- Q18 = A, with an added design requirement that the recommended `~/.agent-bin -> <repo>/scripts/bin` setup should be handled by a Makefile setup target rather than by vague manual documentation.
- Q19 = A
- Q20 = A

## Final Consolidated Decisions

- Rename `orchestrator/internal/agent/shell` to `orchestrator/internal/agent/env`.
- Make `env` the shared agent-launch environment boundary: source startup shell config, assemble PATH, quote commands, and emit the final launch command.
- The launcher launch order is: source `$HOME/.agentrc` if present, prepend `~/.agent-bin` to `PATH`, disable echo, then run the backend command.
- Both `tmux_codex` and `tmux_claude` inherit the feature through the shared launch helper; there are no new operator-facing flags in V1.
- The injected PATH entry is exactly `~/.agent-bin`.
- Command shadowing is allowed: because `~/.agent-bin` is prepended, its executables win normal shell lookup with no special collision logic.
- Runtime validation only checks that `~/.agent-bin` exists as a directory. It does not need to resolve to this repo's `scripts/bin`.
- The launcher fails clearly if `~/.agent-bin` does not exist.
- The operator, not the launcher runtime, owns creating and updating `~/.agent-bin`.
- The recommended happy path is provided through the repo `Makefile`: `make setup` should create or refresh `~/.agent-bin` as a symlink to this repo's committed `scripts/bin`, alongside the existing `.agentrc` setup.
- The repo ships exactly one initial script in `scripts/bin/`: a minimal smoke-test command such as `test_echo` to prove PATH injection works.
- `scripts/bin/test_echo` is part of the recommended setup, but not a hard runtime requirement. Launcher startup still succeeds if `~/.agent-bin` points elsewhere and does not contain `test_echo`.
- Non-executable files inside `scripts/bin/` are not validated at launch time.
- `$HOME/.agentrc` remains the place for user-specific backend startup customization; `~/.agent-bin` is a separate operator-managed command surface available to agents after launch.
- The feature is part of the launcher product contract and should be documented explicitly in active launcher docs.
