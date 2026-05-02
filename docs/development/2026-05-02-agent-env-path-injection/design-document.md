# Agent Launch Environment Rename and PATH Injection

**Goal:** Rename the agent launch `shell` package to `env` and add a launcher-level PATH injection feature so tmux-backed agent sessions can run operator-managed commands from `~/.agent-bin`.

**Status:** Reviewed design input complete; ready for implementation planning.

## Summary

The current launcher runtime has one small shared responsibility boundary: `orchestrator/internal/agent/shell` builds the sourced shell command that each backend sends into tmux. Right now that helper only sources `$HOME/.agentrc`, disables echo, and launches the backend command.

This design turns that boundary into an explicit launch-environment package:

- rename `orchestrator/internal/agent/shell` to `orchestrator/internal/agent/env`
- keep it scoped to agent launch rather than making it a repo-wide environment abstraction
- make it responsible for launch-environment assembly, including PATH injection
- prepend `~/.agent-bin` to `PATH` for launched `tmux_codex` and `tmux_claude` sessions
- keep `.agentrc` as the place for user-specific backend startup customization
- provide a recommended setup path through `make setup`, which should link `~/.agent-bin` to the repo’s committed `scripts/bin`

The first shipped script surface is intentionally tiny. The repo adds a committed `scripts/bin/` directory with one minimal smoke-test command named `test_echo` to prove the PATH injection works. The launcher runtime does not require that specific script to exist at `~/.agent-bin`; it only requires that `~/.agent-bin` resolves to a directory.

## Current State

- `orchestrator/internal/agent/shell/shell.go` owns the shared launch command builder.
- `CodexAgent` and `ClaudeAgent` both call that helper before waiting for backend readiness.
- The current launch command does three things:
  - source `$HOME/.agentrc` if present
  - disable local terminal echo with `stty -echo`
  - start the backend command
- The repo already has a top-level `scripts/` directory with `.agentrc` and `.agentrc.example`.
- `make setup` currently links the repo’s `.agentrc` into `$HOME/.agentrc`.
- The active product surface is launcher-only, so this feature must preserve the external behavior of `tmux_codex` and `tmux_claude` aside from the new launch environment contract.

## Goals

- Rename the shared launch helper package from `shell` to `env`.
- Make `env` the single shared boundary for agent launch-environment assembly.
- Prepend `~/.agent-bin` to `PATH` for all launcher-started agent sessions.
- Preserve the current launcher CLI surface: no new flags or environment variables in V1.
- Apply the feature consistently to both `tmux_codex` and `tmux_claude`.
- Keep `$HOME/.agentrc` as a supported user customization surface.
- Ship one committed smoke-test script named `test_echo` under `scripts/bin/` to prove the feature works end to end.
- Document a recommended operator setup path through `make setup`.
- Fail launcher startup clearly when the required injected directory does not exist.

## Non-Goals

- No generic repo-wide environment abstraction beyond agent launch.
- No new operator-facing flags such as `--path-prepend`.
- No runtime auto-creation or auto-repair of `~/.agent-bin` by launcher commands.
- No runtime requirement that `~/.agent-bin` must point to this repo.
- No per-backend PATH customization differences between Codex and Claude.
- No startup validation of individual commands inside `~/.agent-bin`.
- No broad initial helper-script suite; V1 ships only a smoke-test command.
- No removal or deprecation of `$HOME/.agentrc`.

## Product Behavior

### Launch environment contract

For both `tmux_codex` and `tmux_claude`, the shared launch helper assembles the backend startup command in this order:

1. source `$HOME/.agentrc` if present
2. prepend `~/.agent-bin` to `PATH`
3. disable local terminal echo with `stty -echo`
4. execute the backend command

This ordering is part of the active launcher contract.

### Injected PATH entry

The user-facing injected PATH location is `~/.agent-bin`.

The launcher runtime does not inject the repo path directly. The contract is intentionally a stable home-directory path so launched agent sessions can rely on a predictable operator-managed command location across working directories.

Implementation detail for this contract:

- launcher code must resolve `~/.agent-bin` to the caller's home-directory path before validating it
- the emitted shell command must prepend the resolved home-directory path in a shell-safe way, for example `export PATH="$HOME/.agent-bin:$PATH"` or an equivalent absolute path form
- the runtime must not rely on a literal quoted `~` inside `PATH`

Because `~/.agent-bin` is prepended, normal shell lookup applies and command shadowing is allowed. If an executable in `~/.agent-bin` has the same name as one found later in `PATH`, the `~/.agent-bin` version wins. This design adds no collision detection, warnings, or special-case logic.

### Runtime validation

Before sending the backend startup command into tmux, the shared `env` path must validate that `~/.agent-bin` exists and resolves to a directory.

This validation happens in launcher Go code as part of launch-environment assembly, not as a best-effort shell check after the tmux session has already started the backend command.

Validation does not require any of the following:

- that `~/.agent-bin` is a symlink
- that it points to this repo’s `scripts/bin`
- that it contains `test_echo`
- that every file under it is executable

If `~/.agent-bin` is missing or resolves to something other than a directory, launcher startup returns a clear error to the caller and the backend command is not started.

### Scope of affected launchers

The feature applies to both supported launcher binaries:

- `tmux_codex`
- `tmux_claude`

The behavior is shared through `orchestrator/internal/agent/env`, not duplicated independently per command.

## Package Boundary

### Rename

`orchestrator/internal/agent/shell` is renamed to `orchestrator/internal/agent/env`.

### Responsibility

`orchestrator/internal/agent/env` owns the launch-environment boundary for backend startup. That includes:

- quoting the backend command and arguments
- sourcing `$HOME/.agentrc`
- validating the injected directory contract
- prepending `~/.agent-bin` to `PATH`
- emitting the final shell command sent into tmux

It should remain an agent-launch package, not a general-purpose environment utility package for the rest of the repo.

### Callers

`CodexAgent` and `ClaudeAgent` remain the callers of the shared launch-environment builder. The launcher commands should continue to interact with those agent types as they do today; this design changes the shared launch behavior, not the launcher command UX.

## Script Surface

### Repo directory

The repo adds a committed directory at:

- `scripts/bin/`

V1 supports exactly this one repo directory for the recommended setup flow. The runtime contract still targets `~/.agent-bin`, not the repo path.

### Initial contents

V1 ships exactly one minimal smoke-test script in `scripts/bin/`:

- `test_echo`

Its purpose is to prove that a launched agent can successfully run a command supplied through the injected PATH surface. It is not the start of a broader helper-command suite, and it does not replace backend launch wrappers such as `codex` or `claude`.

### File validation

The launcher runtime does not inspect `scripts/bin/` contents and does not validate per-file executability. Shell PATH behavior remains normal: executable files are runnable, and non-executable files are inert.

## Setup Model

### Ownership

The operator owns `~/.agent-bin`. Launcher runtime does not create or mutate it.

This is consistent with the existing model where runtime sources `$HOME/.agentrc`, but setup is handled out of band.

### Recommended happy path

The recommended setup is:

- `~/.agent-bin` is a symlink to this repo’s `scripts/bin/`
- `make setup` creates or refreshes that recommended symlink
- `make setup` continues to link the repo’s `.agentrc` to `$HOME/.agentrc`

This keeps setup in one operator-invoked command instead of requiring ad hoc manual shell instructions.

### Makefile behavior

`make setup` should configure the recommended setup path, but it must still respect the operator-owned nature of `~/.agent-bin`.

That means:

- if `~/.agent-bin` is absent, `make setup` creates the symlink to `<repo>/scripts/bin`
- if `~/.agent-bin` is already a symlink, `make setup` refreshes it to the recommended target
- if `~/.agent-bin` exists as a non-symlink directory or non-directory file, `make setup` should fail with clear guidance instead of silently replacing it

This keeps `make setup` convenient without turning it into a destructive override of a user-managed directory.

## Relationship With `.agentrc`

`$HOME/.agentrc` remains the user-specific backend startup customization surface.

Its role does not change:

- backend aliases or wrappers may still live there
- user-specific shell setup may still live there

The new PATH injection is a separate concern:

- `.agentrc` customizes backend launch behavior
- `~/.agent-bin` exposes operator-managed commands that the launched agent can execute after startup

The two surfaces are complementary, not replacements for one another.

## Documentation Model

This feature is part of the active launcher product contract and should be documented explicitly in active launcher docs.

At minimum, active launcher documentation should state that launcher sessions:

- source `$HOME/.agentrc` if present
- prepend `~/.agent-bin` to `PATH`
- then start the backend command

The canonical checked-in active launcher contract remains `orchestrator/cmd/tmux_codex/CONTRACT.md`, which already declares the supported launcher surface as `tmux_codex` and `tmux_claude`. This design should update that canonical contract to describe the shared launch-environment behavior for both launchers. A second launcher contract document should only be added if implementation introduces launcher-specific behavior that cannot be documented clearly in the canonical contract.

Development docs should also describe:

- the `shell` to `env` package rename
- the recommended `make setup` symlink flow
- the fact that runtime validates directory existence only, not repo ownership of the path
- that validation is a preflight launcher error rather than an in-pane shell warning

## Edge Cases

### `~/.agent-bin` points somewhere other than this repo

Launcher startup still succeeds as long as the path exists and is a directory. The recommended setup points to this repo’s `scripts/bin`, but runtime does not enforce that target.

### `~/.agent-bin` exists but does not contain `test_echo`

Launcher startup still succeeds. `test_echo` is part of the recommended repo setup, not a mandatory runtime validation requirement.

### `~/.agent-bin` shadows a common command

That is allowed. The prepended path wins normal lookup order, and the design intentionally adds no collision handling.

### `.agentrc` also modifies `PATH`

That is allowed. Because `.agentrc` is sourced first and `~/.agent-bin` is prepended afterward, the injected path still takes precedence over earlier PATH state assembled by `.agentrc`.

### Operator prefers a custom `~/.agent-bin` directory

That is supported. The runtime contract only requires a directory. `make setup` represents the recommended repo-backed path, not the only valid configuration.

## Acceptance Criteria

This design is satisfied when all of the following are true:

- the shared launch package is renamed from `orchestrator/internal/agent/shell` to `orchestrator/internal/agent/env`
- both launcher-backed agents use the renamed shared package
- launcher startup sources `$HOME/.agentrc`, prepends `~/.agent-bin`, disables echo, and then runs the backend command
- the injected PATH location is documented to operators as `~/.agent-bin`, while implementation resolves that home-directory path before shell injection
- launcher startup fails clearly if `~/.agent-bin` is missing or not a directory
- invalid `~/.agent-bin` is reported as a launcher preflight error before the backend command is sent into tmux
- launcher startup does not require `~/.agent-bin` to point to this repo
- no new launcher flags or extra configuration inputs are introduced for PATH injection
- the repo contains `scripts/bin/` with exactly one minimal smoke-test command named `test_echo`
- `make setup` supports the recommended `~/.agent-bin -> <repo>/scripts/bin` symlink flow in addition to the existing `.agentrc` setup
- active launcher docs explicitly describe the PATH injection contract in the canonical launcher contract doc
- `.agentrc` remains documented as the user-specific backend startup customization surface rather than being replaced by `scripts/bin`
