# Cursor Backend Design Decision Log

Date: 2026-05-03
Topic: Cursor CLI backend and `tmux_cursor` launcher

## Round 1

### 1. What should the operator-facing CLI surface be for Cursor support?

Options presented:

- A: Add a dedicated `tmux_cursor` binary alongside `tmux_codex` and `tmux_claude`, and extend the supported launcher surface to exactly those three commands.
- B: Add Cursor only behind a new generic `tmux_agent --backend cursor` command, and keep the existing two commands unchanged.
- C: Replace all backend-specific commands with one generic launcher now.
- D: Add Cursor support only at the library layer for now and postpone any operator-facing command.

User answer:

> 1A

Decision:

- Add a dedicated `tmux_cursor` binary.
- Extend the supported launcher surface to `tmux_codex`, `tmux_claude`, and `tmux_cursor`.
- Do not introduce or revive a generic launcher in this feature.

### 2. How should Cursor be exposed from the public session package?

Options presented:

- A: Add `session.NewCursor(config)` backed by a private `internal/session/backend.Cursor` descriptor, matching the current Codex/Claude pattern.
- B: Keep Cursor internal-only and have `tmux_cursor` construct a generic backend descriptor itself.
- C: Replace `NewCodex`/`NewClaude` with a single `session.New(config)` plus a public backend string or enum.
- D: Skip public session exposure and launch Cursor directly from `cmd/tmux_cursor`.

User answer:

> 2A

Decision:

- Add `session.NewCursor(config)` to the public package.
- Implement Cursor behavior through a private `internal/session/backend.Cursor` descriptor.
- Preserve the current explicit-constructor pattern rather than introducing a generic public backend selector.

### 3. What exact backend command should the harness launch for Cursor?

Options presented:

- A: Launch bare `agent` as the interactive Cursor entrypoint, since the current local CLI already starts the Cursor Agent by default.
- B: Launch `agent agent` explicitly so the subcommand is always spelled out.
- C: Launch `cursor-agent` or another repo-defined wrapper instead of the installed CLI.
- D: Make the command string fully configurable at runtime and do not define a default.

User answer:

> 3A

Decision:

- Launch bare `agent` for the Cursor backend.
- Do not introduce a wrapper command or runtime-configured command string in v1.

### 4. How should v1 handle Cursor-specific startup flags and trust/auth behavior?

Options presented:

- A: Keep v1 symmetric with Codex/Claude and launch Cursor with no extra flags; auth/trust/workspace setup remains an operator prerequisite outside the harness.
- B: Always launch Cursor with force/trust-style flags by default so it is more autonomous out of the box.
- C: Add new `tmux_cursor` flags for Cursor-specific startup options in v1.
- D: Read a Cursor-specific flag bundle from repo config or environment and inject it automatically.

User answer:

> 4A

Decision:

- Launch Cursor with no extra startup flags in v1.
- Treat authentication, trust, and workspace prerequisites as operator-managed setup outside the harness.
- Keep the v1 CLI surface symmetric with the existing launchers.

### 5. What should the initial readiness contract be for the Cursor backend?

Options presented:

- A: Add a backend-specific readiness matcher that waits for a stable interactive Cursor prompt and treats login/trust/setup screens as not-ready, mirroring the Codex/Claude approach.
- B: Treat any non-empty capture as ready, like the current Claude matcher.
- C: Skip readiness waiting for Cursor and attach immediately after launch.
- D: Reuse the Codex readiness matcher unchanged.

User answer:

> 5A, after I launch cursor this is the terminal:
>
> ➜  vault-v2 git:(main) ✗ agent
>
>
>   Cursor Agent
>   v2026.05.01-eea359f
>
>
>
>
>
>
>
>   → Plan, search, build anything
>
>
>
>
>   GPT-5.5 1M                                                     Auto-run
>   ~/.ows/workspaces/obsidian/vault-v2 · main

Decision:

- Add a backend-specific Cursor readiness matcher.
- Treat the observed `Cursor Agent` welcome screen as part of the expected ready-state shape.
- Readiness must exclude login, trust, or other setup interstitials rather than treating any non-empty capture as ready.

### 6. Should this feature stay narrowly scoped to “third backend + third launcher,” or also generalize the launcher contract now?

Options presented:

- A: Keep the scope narrow: add Cursor support within the existing architecture without redesigning the command model again.
- B: Use this feature to introduce a fully generic backend registry and generic launcher surface now.
- C: Use this feature to start a broader multi-agent workflow layer.
- D: Pause Cursor support until the command surface is redesigned more generally.

User answer:

> 6A

Decision:

- Keep this feature narrowly scoped to adding Cursor support within the current architecture.
- Do not redesign the launcher model, backend registry, or workflow layer in this feature.

## Round 2

### 7. What should the default tmux session name and user-facing success label be for the new launcher?

Options presented:

- A: Use default session name `cursor` and human-facing label `Cursor`, matching the current `codex` / `claude` launcher style.
- B: Use default session name `agent` and label `Agent`, matching the executable name.
- C: Use default session name `cursor-agent` and label `Cursor Agent`, matching the splash screen text.
- D: Require `--session` and avoid a default.

User answer:

> 7A

Decision:

- Use `cursor` as the default tmux session name.
- Use `Cursor` as the user-facing success label in CLI output.

### 8. Should `tmux_cursor` keep the same CLI and mkpipe contract as the existing launchers?

Options presented:

- A: Yes. Support `tmux_cursor [--session <name>] [--attach] [--mkpipe [<path>]]` with the same validation, attach-only mkpipe behavior, and exit-code semantics as `tmux_codex` / `tmux_claude`.
- B: Support `--session` and `--attach`, but omit mkpipe for Cursor in v1.
- C: Add Cursor-specific startup flags in v1 even though the other launchers do not have them.
- D: Make `tmux_cursor` attach-only and skip detached launch.

User answer:

> 8A

Decision:

- `tmux_cursor` should use the same CLI and mkpipe contract as the existing launchers.
- Validation, attach-only mkpipe behavior, and exit-code semantics should remain parallel across all three binaries.

### 9. How strict should the Cursor readiness matcher be against the observed splash UI?

Options presented:

- A: Use a tolerant multi-signal matcher: require stable capture containing `Cursor Agent` plus at least one ready-state signal such as the tagline or workspace/model footer, while rejecting login/trust/setup prompts.
- B: Require an exact match of the current splash copy and layout.
- C: Treat `Cursor Agent` alone as sufficient for readiness.
- D: Treat any stable non-empty capture as ready.

User answer:

> 9B

Decision:

- Follow-up required: the user wants a stricter Cursor-specific readiness contract, but the exact required visible markers need to avoid brittle dependence on volatile UI lines.

### 10. What should the runtime environment contract be for making `agent` resolvable inside the harness shell?

Options presented:

- A: Keep the shared launch-environment contract unchanged: source `$HOME/.agentrc`, prepend `~/.agent-bin`, and assume `agent` is available through the operator’s shell environment or existing shell function setup.
- B: Add a repo-managed `scripts/bin/agent` wrapper and require `make setup` to provide Cursor.
- C: Add a new runtime preflight that explicitly checks `command -v agent` inside the launch shell before startup.
- D: Require operators to symlink the installed Cursor CLI into `~/.agent-bin` as a new hard requirement.

User answer:

> 10A

Decision:

- Keep the shared launch-environment contract unchanged.
- Cursor availability remains an operator-managed shell environment concern.
- The existing `.agentrc` passthrough model remains sufficient for v1.

## Round 3

### 11. To make your “exact current splash copy/layout” choice implementable, which visible Cursor screen elements must the readiness matcher require?

Options presented:

- A: Require stable capture containing `Cursor Agent`, the tagline `→ Plan, search, build anything`, and the workspace footer line `<workspace> · <branch>`, but ignore model/account-specific footer details like `GPT-5.5 1M` and `Auto-run`.
- B: Require the full currently observed visible shape, including `Cursor Agent`, version line, tagline, model line, `Auto-run`, and workspace footer.
- C: Require only `Cursor Agent` plus the tagline.
- D: Treat exact readiness markers as operator-configured test data rather than part of the product contract.

User answer:

> 11 the primary issue here is that the tech line might change so let's just only check for the Cursor agent line 1st and then later on we will revise this if there's any problems

Decision:

- The user prefers a less brittle matcher than the original “exact splash copy/layout” option.
- Follow-up required: confirm whether v1 should key readiness only on the stable `Cursor Agent` line.

### 12. How should v1 document and handle Cursor login/trust/setup interstitials?

Options presented:

- A: Document them as operator prerequisites; if launch lands on login/trust/setup UI, the backend stays not-ready, readiness times out, and the command exits through the standard runtime failure path.
- B: Treat those interstitials as ready and let operators finish setup inside the attached tmux session.
- C: Add Cursor-specific remediation text in launcher stderr on readiness failure.
- D: Add a separate preflight/setup command for Cursor in this feature.

User answer:

> 12 A

Decision:

- Document Cursor login, trust, and setup interstitials as operator prerequisites.
- If launch lands on those screens, readiness should not succeed and the launcher should fail through the standard runtime failure path.
- Do not add Cursor-specific remediation text or a separate setup command in v1.

### 13. How should the contract docs change for this feature?

Options presented:

- A: Add `orchestrator/cmd/tmux_cursor/CONTRACT.md` and update the shared “supported operator-facing harness binaries” sections in the existing Codex/Claude contract docs to list all three launchers.
- B: Add only `tmux_cursor/CONTRACT.md` and leave existing contract docs untouched.
- C: Replace the per-launcher contract docs with one shared launcher contract now.
- D: Skip contract-doc changes in this feature.

User answer:

> 13 A

Decision:

- Add a new `orchestrator/cmd/tmux_cursor/CONTRACT.md`.
- Update the shared launcher-surface wording in the existing Codex and Claude contract docs so all three launchers are listed.

### 14. What should happen to the repo build/setup contract?

Options presented:

- A: `make build` should produce `bin/tmux_cursor`; `make setup` stays unchanged because Cursor availability remains operator-managed through the existing shell environment and `.agentrc` passthrough.
- B: `make build` should produce `tmux_cursor`, and `make setup` should install a repo-managed Cursor wrapper too.
- C: Add `tmux_cursor` code now but leave it out of `make build` until later.
- D: Add a separate `make setup-cursor` flow in this feature.

User answer:

> 14 A

Decision:

- Extend `make build` to produce `bin/tmux_cursor`.
- Keep `make setup` unchanged in v1.
- Do not add a repo-managed Cursor wrapper or a separate setup target in this feature.

## Round 4

### 15. Given your note, what should the minimal required ready-state marker be for Cursor in v1?

Options presented:

- A: Require only a stable capture containing `Cursor Agent`, while still rejecting known login/trust/setup interstitials.
- B: Require `Cursor Agent` plus the tagline.
- C: Require `Cursor Agent` plus the workspace footer.
- D: Leave the exact ready marker unspecified and decide during implementation.

User answer:

> A

Decision:

- Cursor readiness in v1 should require only a stable capture containing `Cursor Agent`.
- The matcher should still explicitly reject known login, trust, or setup interstitials.
- Broader or more specific ready-state markers can be revisited later if the minimal matcher proves too permissive.
