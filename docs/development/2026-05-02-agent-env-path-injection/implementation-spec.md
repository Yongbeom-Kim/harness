# Agent Env PATH Injection Implementation Plan

**Goal:** Rename the shared launcher helper to `orchestrator/internal/agent/env`, inject `~/.agent-bin` into launcher-started agent sessions, ship the recommended `make setup` symlink flow, and document the updated launcher contract.

**Architecture:** Keep this feature inside the current launcher-only surface. Add a small `internal/agent/env` package that resolves and validates `~/.agent-bin` before producing the tmux launch string, then wire both `CodexAgent` and `ClaudeAgent` to that shared builder so preflight failures still surface as existing `launch` agent errors. Extend `make setup` to manage the recommended home-directory symlink, add a single `scripts/bin/test_echo` smoke command, and update only the canonical launcher contract doc without widening the operator-facing binary surface.

**Tech Stack:** Go 1.26.2, tmux-backed launcher agents, Bash shell command assembly, Make, Markdown docs, repo-managed shell scripts.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Rename `orchestrator/internal/agent/shell` to `orchestrator/internal/agent/env` and make the new package the only shared launch-environment boundary for launcher startup. | `LaunchEnvBuilder` | `CodexLaunchIntegration`, `ClaudeLaunchIntegration` | `orchestrator/internal/agent/env/env.go`, `orchestrator/internal/agent/env/env_test.go`, `orchestrator/internal/agent/shell/shell.go`, `orchestrator/internal/agent/shell/shell_test.go` | `BuildLaunchCommand(command string, args ...string) (string, error)` | `orchestrator/internal/agent/env/env_test.go`, `cd orchestrator && go test ./internal/agent/env` |
| R2 | Build the launch script in this exact order: source `$HOME/.agentrc`, prepend the resolved absolute `~/.agent-bin`, disable echo with `stty -echo`, then execute the quoted backend command; never emit a literal `~` path into `PATH`. | `LaunchEnvBuilder` | `CanonicalLauncherContract` | `orchestrator/internal/agent/env/env.go`, `orchestrator/internal/agent/env/env_test.go`, `orchestrator/cmd/tmux_codex/CONTRACT.md` | `resolveAgentBinDir()`, `buildShellScript(agentBin, command, args...)`, returned `bash -lc` command string | `orchestrator/internal/agent/env/env_test.go` order, absolute-path, and command/arg quoting assertions; doc grep in `orchestrator/cmd/tmux_codex/CONTRACT.md` |
| R3 | `tmux_codex` sessions must receive the shared env-builder behavior with no new flags, and env preflight failures must surface as existing `launch` agent errors before `pane.SendText` runs. | `CodexLaunchIntegration` | `LaunchEnvBuilder` | `orchestrator/internal/agent/codex.go`, `orchestrator/internal/agent/agent_test.go` | `CodexAgent.Start()`, `NewAgentError(ErrorKindLaunch, sessionName, "", err)` | `orchestrator/internal/agent/agent_test.go`, `cd orchestrator && go test ./internal/agent ./cmd/tmux_codex` |
| R4 | `tmux_claude` sessions must receive the same shared env-builder behavior with no new flags, and env preflight failures must surface as existing `launch` agent errors before `pane.SendText` runs. | `ClaudeLaunchIntegration` | `LaunchEnvBuilder` | `orchestrator/internal/agent/claude.go`, `orchestrator/internal/agent/agent_test.go` | `ClaudeAgent.Start()`, `NewAgentError(ErrorKindLaunch, sessionName, "", err)` | `orchestrator/internal/agent/agent_test.go`, `cd orchestrator && go test ./internal/agent ./cmd/tmux_claude` |
| R5 | Runtime validation accepts any directory at `~/.agent-bin`, even if it points outside this repo or does not contain `test_echo`; the launcher must not inspect the directory contents or require repo ownership. | `LaunchEnvBuilder` | `SetupTarget`, `CanonicalLauncherContract` | `orchestrator/internal/agent/env/env.go`, `orchestrator/internal/agent/env/env_test.go`, `orchestrator/cmd/tmux_codex/CONTRACT.md` | `os.UserHomeDir()`, `filepath.Join(home, ".agent-bin")`, `os.Stat(absAgentBin)`, `FileInfo.IsDir()` | `orchestrator/internal/agent/env/env_test.go` directory-only validation cases, manual custom-directory smoke in Task 4 |
| R6 | If `~/.agent-bin` is missing or is not a directory, launcher startup must fail clearly before the backend command is sent into tmux. | `LaunchEnvBuilder` | `CodexLaunchIntegration`, `ClaudeLaunchIntegration` | `orchestrator/internal/agent/env/env.go`, `orchestrator/internal/agent/env/env_test.go`, `orchestrator/internal/agent/codex.go`, `orchestrator/internal/agent/claude.go`, `orchestrator/internal/agent/agent_test.go` | returned `error` from `BuildLaunchCommand(...)`, `Start()` early return before `pane.SendText(...)` | `orchestrator/internal/agent/env/env_test.go` missing/non-directory cases, `orchestrator/internal/agent/agent_test.go` launch-error wrapping and no-send cases |
| R7 | If `.agentrc` also mutates `PATH`, the injected agent-bin path still wins because export happens after sourcing; command shadowing is allowed and the feature adds no collision detection or warnings. | `LaunchEnvBuilder` | `CanonicalLauncherContract` | `orchestrator/internal/agent/env/env.go`, `orchestrator/internal/agent/env/env_test.go`, `orchestrator/cmd/tmux_codex/CONTRACT.md` | shell assembly order in `buildShellScript(...)`, `export PATH="<abs>:$PATH"` with no `command -v` or collision scan | `orchestrator/internal/agent/env/env_test.go` order assertions, manual runtime smoke with `test_echo` |
| R8 | `make setup` becomes the recommended operator setup path: link `.agentrc`, create or refresh `~/.agent-bin -> <repo>/scripts/bin`, and fail with clear guidance if `~/.agent-bin` already exists as a non-symlink file or directory. | `SetupTarget` | `SmokeTestScript` | `Makefile` | `setup:` target shell logic, `ln -sfn "$(ROOT)/scripts/bin" "$$HOME/.agent-bin"` guarded by a non-symlink check | temp-`HOME` shell probes in Task 3, manual `make setup` verification in Task 4 |
| R9 | Ship exactly one initial helper command at `scripts/bin/test_echo`, implemented as a tiny executable that echoes its argv so the feature proves both PATH discovery and argument passing. | `SmokeTestScript` | `SetupTarget` | `scripts/bin/test_echo` | executable `test_echo [args...]` script entrypoint | direct shell invocation `PATH="$PWD/scripts/bin:$PATH" test_echo hello there`, manual launched-session smoke in Task 4 |
| R10 | Update only the canonical `orchestrator/cmd/tmux_codex/CONTRACT.md` to document the shared launch order, directory-only validation, recommended `make setup` flow, and unchanged launcher CLI surface for both `tmux_codex` and `tmux_claude`; do not add a new `tmux_claude` contract doc. | `CanonicalLauncherContract` | `LaunchEnvBuilder`, `SetupTarget` | `orchestrator/cmd/tmux_codex/CONTRACT.md` | contract sections `Purpose`, `Runtime Model`, `Validation Contract`, and setup documentation text | `rg -n 'agent-bin|make setup|source .*agentrc' orchestrator/cmd/tmux_codex/CONTRACT.md`, existing launcher package tests via `cd orchestrator && go test ./cmd/tmux_codex ./cmd/tmux_claude` |

## Component Responsibility Map

- `LaunchEnvBuilder`: primary owner for the renamed `internal/agent/env` package, including home-directory resolution, directory validation, PATH prepend order, shell quoting, and final `bash -lc` launch-string assembly. Collaborates with `CodexLaunchIntegration` and `ClaudeLaunchIntegration` only through `BuildLaunchCommand(...)`. Does not own agent error wrapping, `make setup`, or docs.
- `CodexLaunchIntegration`: primary owner for calling `BuildLaunchCommand("codex")` from `CodexAgent.Start()`, preserving the existing start lifecycle, and wrapping env preflight failures as `launch` agent errors. Collaborates with `LaunchEnvBuilder` through the returned command string and error. Does not own shell assembly or setup flow.
- `ClaudeLaunchIntegration`: primary owner for calling `BuildLaunchCommand("claude")` from `ClaudeAgent.Start()`, preserving the existing start lifecycle, and wrapping env preflight failures as `launch` agent errors. Collaborates with `LaunchEnvBuilder` through the returned command string and error. Does not own shell assembly or setup flow.
- `SetupTarget`: primary owner for the recommended operator-managed symlink flow in `make setup`. It keeps `.agentrc` linking in place, adds `~/.agent-bin` linking to `scripts/bin`, and fails safely when the existing path is operator-owned and not a symlink. It does not own runtime validation or `scripts/.agentrc.example`.
- `SmokeTestScript`: primary owner for the tiny committed command surface at `scripts/bin/test_echo`. It gives the repo one concrete executable to expose through the injected PATH. It does not own PATH injection logic, symlink setup, or runtime docs.
- `CanonicalLauncherContract`: primary owner for the active launcher documentation in `orchestrator/cmd/tmux_codex/CONTRACT.md`. It records the shared behavior for both launchers, the recommended setup path, and the preserved flag surface. It does not own runtime code or create a second `tmux_claude` contract file.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CodexLaunchIntegration` | `LaunchEnvBuilder` | `BuildLaunchCommand("codex") (string, error)` | `CodexAgent.Start()` must call this before `pane.SendText(...)`. If the call returns an error, `Start()` wraps it as `NewAgentError(ErrorKindLaunch, sessionName, "", err)` and returns immediately. |
| `ClaudeLaunchIntegration` | `LaunchEnvBuilder` | `BuildLaunchCommand("claude") (string, error)` | `ClaudeAgent.Start()` must follow the same preflight-before-send rule as Codex. The two backends share env behavior; they do not diverge on PATH injection. |
| `LaunchEnvBuilder` | OS home/filesystem APIs | `os.UserHomeDir()`, `filepath.Join(home, ".agent-bin")`, `os.Stat(absAgentBin)` | The resolved absolute directory is the source of truth. Validation succeeds only when the path exists and `IsDir()` is true. No repo-target, symlink-target, or per-file validation is allowed. |
| `SetupTarget` | filesystem | `make setup` shell recipe | The target must always refresh `$HOME/.agentrc`, create or refresh `$HOME/.agent-bin` only when it is absent or already a symlink, and return a clear non-zero failure when a non-symlink path already occupies `$HOME/.agent-bin`. |
| `SmokeTestScript` | launched backend terminal environment | `test_echo [args...]` | After `make setup` or any equivalent custom operator-managed directory setup, the launched backend should be able to invoke `test_echo` through PATH. The script’s only job is to echo argv; it is not a backend wrapper. |
| `CanonicalLauncherContract` | runtime and setup surfaces | contract text in `orchestrator/cmd/tmux_codex/CONTRACT.md` | The document must describe `source $HOME/.agentrc -> prepend ~/.agent-bin -> stty -echo -> backend`, directory-only validation, the `make setup` happy path, and the unchanged `--session` / `--attach` launcher UX shared by `tmux_codex` and `tmux_claude`. |

## File Ownership Map

- Create `orchestrator/internal/agent/env/env.go` - owned by `LaunchEnvBuilder`; new shared launch-environment builder with package-private helpers for home resolution, directory validation, command quoting, and shell-script assembly.
- Create `orchestrator/internal/agent/env/env_test.go` - owned by `LaunchEnvBuilder`; focused unit coverage for ordering, absolute PATH injection, directory-only validation, and missing/non-directory failures.
- Modify `orchestrator/internal/agent/codex.go` - owned by `CodexLaunchIntegration`; switch the agent from the old `shell` package to `env.BuildLaunchCommand(...)`, return early on preflight failure, and keep the existing ready-matcher logic unchanged.
- Modify `orchestrator/internal/agent/claude.go` - owned by `ClaudeLaunchIntegration`; switch the agent from the old `shell` package to `env.BuildLaunchCommand(...)`, return early on preflight failure, and keep the existing ready-matcher logic unchanged.
- Modify `orchestrator/internal/agent/agent_test.go` - owned by `CodexLaunchIntegration`; shared start-flow regression coverage already lives here, so Codex owns the file while Claude additions remain incidental to the same package-level test surface.
- Delete `orchestrator/internal/agent/shell/shell.go` - owned by `LaunchEnvBuilder`; obsolete once both callers move to `orchestrator/internal/agent/env`.
- Delete `orchestrator/internal/agent/shell/shell_test.go` - owned by `LaunchEnvBuilder`; replaced by the richer env-package unit tests.
- Modify `Makefile` - owned by `SetupTarget`; extend `setup` to manage `~/.agent-bin` safely alongside the existing `.agentrc` symlink.
- Create `scripts/bin/test_echo` - owned by `SmokeTestScript`; the single committed executable exposed through the recommended `~/.agent-bin` symlink.
- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `CanonicalLauncherContract`; update the active launcher contract to describe the shared launch-environment behavior and recommended setup flow.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/agent/env/env.go`
- `orchestrator/internal/agent/env/env_test.go`
- `orchestrator/internal/agent/codex.go`
- `orchestrator/internal/agent/claude.go`
- `orchestrator/internal/agent/agent_test.go`
- `orchestrator/internal/agent/shell/shell.go` - delete only.
- `orchestrator/internal/agent/shell/shell_test.go` - delete only.
- `Makefile`
- `scripts/bin/test_echo`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`

**Incidental-only files:**
- None expected. If implementation appears to require touching `scripts/.agentrc.example`, `orchestrator/cmd/tmux_codex/main.go`, `orchestrator/cmd/tmux_codex/main_test.go`, `orchestrator/cmd/tmux_claude/main.go`, or `orchestrator/cmd/tmux_claude/main_test.go`, stop and update the plan instead of widening scope implicitly.

## Task List

### Task 1: LaunchEnvBuilder

**Files:**
- Create: `orchestrator/internal/agent/env/env.go`
- Create: `orchestrator/internal/agent/env/env_test.go`

**Covers:** `R1`, `R2`, `R5`, `R7`
**Owner:** `LaunchEnvBuilder`
**Why:** Establish the renamed shared package and its validation/assembly contract before touching the launcher call sites. This task intentionally keeps the old `shell` package in place temporarily so the tree stays buildable until Task 2 finishes the migration.

- [ ] **Step 1: Write the failing env-package tests**

```go
func TestBuildLaunchCommandPrependsResolvedAgentBinAfterAgentrc(t *testing.T) {}

func TestBuildLaunchCommandUsesAbsoluteAgentBinPath(t *testing.T) {}

func TestBuildLaunchCommandQuotesCommandAndArgs(t *testing.T) {}

func TestBuildLaunchCommandAllowsCustomDirectoryWithoutInspectingContents(t *testing.T) {}

func TestBuildLaunchCommandFailsWhenAgentBinMissing(t *testing.T) {}

func TestBuildLaunchCommandFailsWhenAgentBinIsNotDirectory(t *testing.T) {}
```

- [ ] **Step 2: Run the focused tests to verify they fail**

Run: `cd orchestrator && go test ./internal/agent/env`
Expected: FAIL because the `env` package does not exist yet.

- [ ] **Step 3: Write the minimal implementation**

```go
func BuildLaunchCommand(command string, args ...string) (string, error) {
	agentBin, err := resolveAgentBinDir()
	if err != nil {
		return "", err
	}
	return "bash -lc " + shellQuote(buildShellScript(agentBin, command, args...)), nil
}
```

In the same file:
- implement `resolveAgentBinDir()` with `os.UserHomeDir()`, `filepath.Join(home, ".agent-bin")`, and `os.Stat(...)`
- reject missing paths and non-directories with clear error text that names `~/.agent-bin`
- implement `buildShellScript(...)` so the emitted script sources `$HOME/.agentrc`, prepends the resolved absolute path, runs `stty -echo`, and then launches the fully quoted backend command
- preserve the old quoting regression coverage by asserting that a command/arg set such as `("codex", "one two", "it's")` survives shell quoting correctly in the final launch string
- keep all helpers in `env.go`; do not split the package into extra files in this task

- [ ] **Step 4: Re-run the env-package tests**

Run: `cd orchestrator && go test ./internal/agent/env`
Expected: PASS, proving the new package can build and enforces the required order and validation contract on its own.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agent/env/env.go \
        orchestrator/internal/agent/env/env_test.go
git commit -m "feat(agent): add env launch builder"
```

### Task 2: Codex And Claude Launch Integrations

**Files:**
- Modify: `orchestrator/internal/agent/codex.go`
- Modify: `orchestrator/internal/agent/claude.go`
- Modify: `orchestrator/internal/agent/agent_test.go`
- Delete: `orchestrator/internal/agent/shell/shell.go`
- Delete: `orchestrator/internal/agent/shell/shell_test.go`

**Covers:** `R3`, `R4`, `R6`
**Owner:** `CodexLaunchIntegration`
**Why:** Move both launcher agents onto the new env builder in one task so the old `shell` package can be removed cleanly and preflight failures become real launch errors before any tmux pane text is sent.

- [ ] **Step 1: Write the failing agent-start tests**

```go
func TestCodexAgentStartWrapsEnvPreflightError(t *testing.T) {}

func TestClaudeAgentStartWrapsEnvPreflightError(t *testing.T) {}
```

Each new test should:
- create a temp `HOME`
- omit `HOME/.agent-bin` or replace it with a regular file
- confirm `errors.As(err, *AgentError)` succeeds
- assert `Kind == ErrorKindLaunch`
- assert the fake pane recorded no `SendText(...)` call

Also update the existing happy-path start tests to set `HOME` to a temp directory and create `HOME/.agent-bin` before calling `Start()`, because the new preflight is now part of normal startup.

- [ ] **Step 2: Run the package tests to verify they fail**

Run: `cd orchestrator && go test ./internal/agent`
Expected: FAIL because the new tests expect env-based preflight behavior, but both agents still import the old `shell` package.

- [ ] **Step 3: Write the minimal integration**

```go
launchText, err := agentenv.BuildLaunchCommand(a.launchCommand)
if err != nil {
	_ = session.Close()
	return NewAgentError(ErrorKindLaunch, a.sessionName, "", err)
}
if err := pane.SendText(launchText); err != nil {
	_ = session.Close()
	return NewAgentError(ErrorKindLaunch, a.sessionName, "", err)
}
```

In the same change:
- switch both agent files from `internal/agent/shell` to `internal/agent/env`
- keep readiness logic and session cleanup behavior unchanged
- delete `orchestrator/internal/agent/shell/shell.go` and `shell_test.go` after both callers have migrated

- [ ] **Step 4: Re-run the affected tests**

Run: `cd orchestrator && go test ./internal/agent ./cmd/tmux_codex ./cmd/tmux_claude`
Expected: PASS, proving both call sites compile against the new env package and existing launcher packages still build cleanly.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/agent/codex.go \
        orchestrator/internal/agent/claude.go \
        orchestrator/internal/agent/agent_test.go
git add -u orchestrator/internal/agent/shell
git commit -m "refactor(agent): route launcher startup through env builder"
```

### Task 3: SetupTarget

**Files:**
- Modify: `Makefile`
- Create: `scripts/bin/test_echo`

**Covers:** `R8`, `R9`
**Owner:** `SetupTarget`
**Why:** The runtime contract uses `~/.agent-bin`, so the repo needs a safe recommended setup path plus one concrete script that proves PATH injection works after setup.

- [ ] **Step 1: Write the failing setup and smoke probes**

```bash
tmp_home="$(mktemp -d)"
HOME="$tmp_home" make setup
test -L "$tmp_home/.agentrc"
test -L "$tmp_home/.agent-bin"
test "$(readlink "$tmp_home/.agent-bin")" = "$PWD/scripts/bin"
PATH="$PWD/scripts/bin:$PATH" test_echo hello there

bad_home="$(mktemp -d)"
mkdir "$bad_home/.agent-bin"
! HOME="$bad_home" make setup
```

- [ ] **Step 2: Run the probes to verify they fail**

Run: `tmp_home="$(mktemp -d)" && HOME="$tmp_home" make setup && test -L "$tmp_home/.agentrc" && test -L "$tmp_home/.agent-bin" && test "$(readlink "$tmp_home/.agent-bin")" = "$PWD/scripts/bin" && PATH="$PWD/scripts/bin:$PATH" test_echo hello there && bad_home="$(mktemp -d)" && mkdir "$bad_home/.agent-bin" && ! HOME="$bad_home" make setup`
Expected: FAIL because `make setup` does not yet manage `~/.agent-bin`, `scripts/bin/test_echo` does not exist yet, and the current setup target does not reject a pre-existing non-symlink directory.

- [ ] **Step 3: Write the minimal implementation**

```make
setup:
	ln -sfn "$(ROOT)/scripts/.agentrc" "$$HOME/.agentrc"
	@if [ -e "$$HOME/.agent-bin" ] && [ ! -L "$$HOME/.agent-bin" ]; then \
		echo '$$HOME/.agent-bin already exists and is not a symlink; move it aside or replace it manually' >&2; \
		exit 1; \
	fi
	ln -sfn "$(ROOT)/scripts/bin" "$$HOME/.agent-bin"
```

Create `scripts/bin/test_echo` as an executable script like:

```bash
#!/bin/bash
printf '%s\n' "$@"
```

Do not modify `scripts/.agentrc.example` in this task; setup ownership stays in `Makefile`, not in the example shell config.

- [ ] **Step 4: Re-run the setup and smoke probes**

Run: `tmp_home="$(mktemp -d)" && HOME="$tmp_home" make setup && test -L "$tmp_home/.agentrc" && test -L "$tmp_home/.agent-bin" && test "$(readlink "$tmp_home/.agent-bin")" = "$PWD/scripts/bin" && PATH="$PWD/scripts/bin:$PATH" test_echo hello there && bad_home="$(mktemp -d)" && mkdir "$bad_home/.agent-bin" && ! HOME="$bad_home" make setup`
Expected: PASS. `test_echo hello there` should print:

```text
hello
there
```

and the second `make setup` invocation should fail with clear guidance instead of silently replacing the operator-owned directory.

- [ ] **Step 5: Commit**

```bash
git add Makefile scripts/bin/test_echo
git commit -m "feat(setup): link agent-bin smoke scripts"
```

### Task 4: CanonicalLauncherContract

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`

**Covers:** `R10`
**Owner:** `CanonicalLauncherContract`
**Why:** The feature changes the active launcher contract, so the checked-in canonical launcher doc must describe the shipped behavior and the final verification pass must prove the docs and runtime match.

- [ ] **Step 1: Write the failing doc checks**

```bash
rg -n 'prepend `~/.agent-bin` to `PATH`' orchestrator/cmd/tmux_codex/CONTRACT.md
rg -n '`make setup`' orchestrator/cmd/tmux_codex/CONTRACT.md
rg -n 'source .*agentrc.*if present' orchestrator/cmd/tmux_codex/CONTRACT.md
```

- [ ] **Step 2: Run the doc checks to verify they fail**

Run: `rg -n 'prepend `~/.agent-bin` to `PATH`' orchestrator/cmd/tmux_codex/CONTRACT.md && rg -n '`make setup`' orchestrator/cmd/tmux_codex/CONTRACT.md && rg -n 'source .*agentrc.*if present' orchestrator/cmd/tmux_codex/CONTRACT.md`
Expected: FAIL because the current contract does not yet mention PATH injection or the recommended setup path.

- [ ] **Step 3: Update the canonical launcher contract**

Document all of the following in `orchestrator/cmd/tmux_codex/CONTRACT.md`:
- the shared launch order for both `tmux_codex` and `tmux_claude`: source `.agentrc`, prepend `~/.agent-bin`, `stty -echo`, then run the backend
- the fact that runtime validates only that `~/.agent-bin` exists and is a directory
- the fact that `make setup` is the recommended repo-backed happy path, but runtime does not require `~/.agent-bin` to point back to this repo
- the unchanged `--session` / `--attach` launcher surface

Do not add a new `orchestrator/cmd/tmux_claude/CONTRACT.md`; this feature keeps the single canonical contract doc model.

- [ ] **Step 4: Re-run the doc checks and full Go test suite**

Run: `rg -n 'prepend `~/.agent-bin` to `PATH`' orchestrator/cmd/tmux_codex/CONTRACT.md && rg -n '`make setup`' orchestrator/cmd/tmux_codex/CONTRACT.md && rg -n 'source .*agentrc.*if present' orchestrator/cmd/tmux_codex/CONTRACT.md && cd orchestrator && go test ./...`
Expected: PASS, proving the active doc matches the feature and the full Go tree still builds.

- [ ] **Step 5: Run the final manual verification**

This step requires working `codex`, `claude`, and `tmux` operator prerequisites.

Run:

```bash
make build
tmp_home="$(mktemp -d)"
HOME="$tmp_home" make setup

HOME="$tmp_home" bin/tmux_codex --session env-codex
tmux send-keys -t env-codex 'Run `test_echo codex-smoke` in the terminal and reply with the exact output only.' C-m
sleep 20
tmux capture-pane -pt env-codex | tail -40
tmux kill-session -t env-codex

HOME="$tmp_home" bin/tmux_claude --session env-claude
tmux send-keys -t env-claude 'Run `test_echo claude-smoke` in the terminal and reply with the exact output only.' C-m
sleep 20
tmux capture-pane -pt env-claude | tail -40
tmux kill-session -t env-claude

bad_home="$(mktemp -d)"
! HOME="$bad_home" bin/tmux_codex --session env-missing

bad_home="$(mktemp -d)"
: > "$bad_home/.agent-bin"
! HOME="$bad_home" bin/tmux_codex --session env-file
```

Expected:
- both successful launches print their normal success banners
- the captured Codex pane contains `codex-smoke`
- the captured Claude pane contains `claude-smoke`
- the missing-path and non-directory launches both exit `1` with a clear `.agent-bin` preflight error before the backend starts

- [ ] **Step 6: Commit**

```bash
git add orchestrator/cmd/tmux_codex/CONTRACT.md
git commit -m "docs: describe launcher env path injection"
```
