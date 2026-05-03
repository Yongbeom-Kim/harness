# Implement-With-Reviewer Decision Log

Date: 2026-05-03
Topic: tmux-backed implement-with-reviewer workflow on top of `orchestrator/internal/session`

## Context

- The active reusable session package in the repo is `orchestrator/internal/session`.
- A previous design assumption about a public `orchestrator/session` package is superseded for this feature.
- The requested feature is a new `implement-with-reviewer` binary that starts one tmux session, creates two coding-agent session handles inside that tmux session on separate panes, wires mkpipe paths between them, and seeds role-specific prompts for implementer and reviewer behavior.
- The requested feature also includes a small rename of the current `session` package so it is not confused with a tmux session.

## Round 1

### 1. What should the task-input contract be for `implement-with-reviewer`?

Options presented:

- A: Require one positional `<prompt>` argument and do not read `stdin` in v1. (Recommended)
- B: Require one positional `<prompt>` argument, but also allow `stdin` when it is omitted.
- C: Keep `stdin` as the canonical task input and treat the trailing `<prompt>` shape as non-binding.
- D: Accept both a positional `<prompt>` and `stdin` and concatenate them.

User answer:

> 1A

Decision:

- `implement-with-reviewer` should require one positional prompt argument in v1.
- The command should not read task content from `stdin`.

### 2. What overall runtime model should `implement-with-reviewer` use after startup?

Options presented:

- A: Treat it as a bootstrapper: create the shared tmux session, start both agent handles, send the seeded prompts, and then hand control to tmux/human observation rather than supervising every turn itself. (Recommended)
- B: Keep a supervising harness process that watches both panes and decides when to forward each next message.
- C: Run one implementation and one review only, with no iterative loop.
- D: Restore the previous artifact-writing runner model and keep orchestration outside the agents.

User answer:

> 2A

Decision:

- The new command should be a bootstrapper rather than a turn-by-turn supervising runner.
- Agent-to-agent progression should happen through the seeded protocol and mkpipe wiring instead of a persistent orchestrator loop.

### 3. How should the shared tmux session be surfaced to the operator in v1?

Options presented:

- A: Auto-attach to the shared tmux session after both panes are ready and both initial prompts are sent. (Recommended)
- B: Do not attach; print the tmux session name and let the operator attach manually later.
- C: Add an explicit `--attach` flag and default to detached startup.
- D: Never allow operator attachment; the session is internal only.

User answer:

> 3A

Decision:

- The command should auto-attach to the shared tmux session once both panes are ready and seeded.
- No separate attach flag is needed in v1.

### 4. What exact ownership boundary should each per-agent handle have relative to tmux?

Options presented:

- A: `implement-with-reviewer` creates the one tmux session, and each agent handle receives that tmux session and allocates/owns exactly one pane plus the backend lifecycle running inside it. (Recommended)
- B: The first agent handle creates the tmux session and the second handle attaches to it later.
- C: Each agent handle keeps creating its own tmux session; the workflow command only coordinates them logically.
- D: The tmux package should own all pane allocation and backend launch directly, with no per-agent handle abstraction.

User answer:

> 4A, i expect this will also involve some refactoring for the other commands, they will also create their own tmux sessions.

Decision:

- The workflow command should create the tmux session first.
- Each per-agent runtime handle should then receive that tmux session and own one pane plus one backend lifecycle.
- This same ownership split should also become the model for the single-agent launcher commands: those commands create their own tmux sessions externally and then pass them into the per-agent runtime handles.

### 5. What minimum coordination protocol should be injected into the seeded prompts?

Options presented:

- A: Include own role, counterpart mkpipe path, shared session context, and explicit message markers for “implementation ready”, “changes requested”, and “approved” so agent-to-agent handoff is structured. (Recommended)
- B: Include only own role and counterpart mkpipe path; let agents invent the handoff format.
- C: Include only role text; no pipe path or explicit protocol.
- D: Keep the old reviewer/implementer prompts and have the harness translate between them instead of the agents talking directly.

User answer:

> 5A

Decision:

- The seeded prompts should include explicit role, peer mkpipe path, shared session context, and structured handoff markers.
- The command should not rely on free-form emergent coordination between the two agents.

### 6. How should we rename the current `internal/session` package family to avoid collision with the tmux-session concept?

Options presented:

- A: Rename it to `internal/agentruntime` and move its subpackages under that tree. (Recommended)
- B: Rename it to `internal/runtime`.
- C: Rename it to `internal/agentsession`.
- D: Keep `internal/session` and only rename exported types like `Session`.

User answer:

> 6A

Decision:

- Rename the current `internal/session` package family to `internal/agentruntime`.
- Move the current subpackages under that new package tree.

### 7. What shorthand flags should the command document and support?

Options presented:

- A: Support short flag names `-i` / `-r` in code, while documenting the long forms `--implementer` / `--reviewer` as primary. (Recommended)
- B: Support only `--i` / `--r` as the short forms.
- C: Support only `--implementer` / `--reviewer`; drop short aliases.
- D: Support both single-letter aliases and long names, but document the single-letter forms as primary.

User answer:

> 7A

Decision:

- The command should support short aliases `-i` and `-r`.
- The canonical documented forms remain `--implementer` and `--reviewer`.

## Round 2

### 8. What should be the published binary name for this feature?

Options presented:

- A: Ship the operator-facing binary as `implement-with-reviewer`, and treat `implement_with_reviewer` only as an internal/conceptual label. (Recommended)
- B: Ship the operator-facing binary as `implement_with_reviewer`.
- C: Install both names and treat them as equal aliases.
- D: Delay naming and keep the feature internal until later.

User answer:

> 8A

Decision:

- The shipped operator-facing binary name should be `implement-with-reviewer`.
- Snake_case naming can remain internal or descriptive, but not part of the CLI surface.

### 9. How should the shared tmux session be named in v1?

Options presented:

- A: Auto-generate a unique tmux session name internally and do not add a `--session` flag, so the CLI stays as close as possible to the requested shape. (Recommended)
- B: Add an optional `--session <name>` override to the new command.
- C: Reuse the implementer backend name as the tmux session name.
- D: Use one fixed global session name such as `implement-with-reviewer`.

User answer:

> 9A

Decision:

- The workflow command should auto-generate a unique tmux session name.
- v1 should not add a `--session` flag.

### 10. How should mkpipe ownership change for this design?

Options presented:

- A: Move mkpipe ownership into each agent runtime so a started runtime can expose a FIFO path and keep listening independently of `Attach`; the workflow command just reads those paths and seeds them into prompts. (Recommended)
- B: Keep mkpipe attach-only and rely on the `implement-with-reviewer` process staying attached forever.
- C: Keep mkpipe outside the runtime and let the workflow command own both FIFO listeners directly.
- D: Do not use mkpipe for agent-to-agent messaging; have the workflow command relay everything itself.

User answer:

> 10B, any issues with this?

Decision in progress:

- The current preference is to keep mkpipe attach-only and depend on the attached `implement-with-reviewer` process staying alive.
- Follow-up required because this choice creates a lifecycle tension with tmux detach: if attach exits, process-owned listeners also exit and inter-agent messaging stops.

### 11. Once the package tree is renamed to `internal/agentruntime`, how much identifier renaming should happen inside it?

Options presented:

- A: Keep the rename small: change the package/path tree, but keep most existing type and method names unless they directly create tmux-session ambiguity. (Recommended)
- B: Fully rename the main type and API surface too, such as `Session` -> `Runtime` and `SessionName()` -> `TmuxSessionName()`.
- C: Rename only the top-level package path and leave the subpackages under `internal/session/*`.
- D: Rename every public identifier that mentions `session`, even if the churn is high.

User answer:

> 11A

Decision:

- Keep the rename small.
- Change the package/path tree to `internal/agentruntime`, but do not introduce wide identifier churn unless a name is directly ambiguous with tmux-session semantics.

### 12. What should happen when the reviewer approves?

Options presented:

- A: The reviewer sends an explicit approval message to the implementer, then both agents stop autonomous pipe messaging and remain idle in their panes for human follow-up. (Recommended)
- B: The reviewer approval should cause both backend CLIs to terminate automatically.
- C: The workflow command should detect approval and kill the tmux session automatically.
- D: The implementer should automatically commit or open an MR on approval.

User answer:

> 12A

Decision:

- Approval should be an explicit reviewer-to-implementer message.
- After approval, both agents should stop autonomous pipe messaging and remain idle in their panes for human follow-up.

### 13. How should blocker or clarification situations work during the autonomous loop?

Options presented:

- A: If an agent needs missing information or gets blocked, it should stop the autonomous loop and ask the human directly in its pane instead of bouncing uncertainty through the peer pipe. (Recommended)
- B: It should always ask the peer agent first through mkpipe.
- C: It should silently retry until it eventually succeeds or times out.
- D: The whole workflow should abort immediately on the first clarification need.

User answer:

> 13B

Decision in progress:

- The current preference is peer-first clarification through mkpipe.
- Follow-up required to define the fallback when the peer cannot resolve the blocker, so the loop does not deadlock or bounce indefinitely.

### 14. What should happen to the existing single-agent launcher commands during this refactor?

Options presented:

- A: Preserve the current `tmux_codex`, `tmux_claude`, and `tmux_cursor` CLI contracts, but refactor their internals to create a tmux session first and then pass it into one agent runtime. (Recommended)
- B: Change the single-agent CLI surface now to match the new workflow model.
- C: Deprecate the single-agent commands and make `implement-with-reviewer` the main entrypoint.
- D: Leave the single-agent commands on the old path and refactor only the new workflow command.

User answer:

> 14A

Decision:

- Keep the existing single-agent command contracts unchanged.
- Refactor their internals to match the new ownership split: command creates tmux session, then passes it into one agent runtime.

## Round 3

### 15. Should autonomous agent-to-agent messaging survive a normal tmux detach/re-attach?

Options presented:

- A: Yes. If the operator detaches, the agents should keep talking; this means mkpipe/listener lifecycle cannot stay attach-scoped. (Recommended)
- B: No. V1 only guarantees autonomous messaging while `implement-with-reviewer` remains attached; detaching stops the loop.
- C: Detach is unsupported while the autonomous loop is active; the command may warn, but behavior after detach is undefined.
- D: Detach is allowed, but after detach the agents should continue only with manual human coordination.

User answer:

> 15B, I think it doesn't need to survive a detaach.

Decision:

- Autonomous agent-to-agent messaging does not need to survive detach in v1.
- The product guarantee is limited to the lifetime of the attached `implement-with-reviewer` process.

### 16. In the peer-first clarification model, what should happen if the peer also cannot resolve the blocker after one exchange?

Options presented:

- A: Escalate to the human in-pane and stop autonomous messaging until the human responds. (Recommended)
- B: Keep asking the peer repeatedly until one agent produces an answer.
- C: Abort the whole workflow immediately.
- D: Let the reviewer decide by default, even on missing task requirements.

User answer:

> 16B, it's okay let's just let them continue to message. If I find that this is a problem, I will change it later on with another feature.

Decision:

- Peer-first clarification should remain open-ended in v1.
- The system does not add deadlock-prevention or escalation rules yet; human intervention remains a manual observation concern.

### 17. What minimal pre-attach output should `implement-with-reviewer` print?

Options presented:

- A: Print one concise line with the generated tmux session name and the implementer/reviewer backend mapping before auto-attach. (Recommended)
- B: Print nothing; attach immediately.
- C: Print the tmux session name plus both mkpipe paths before attach.
- D: Print a multi-line instruction block showing how to detach and reattach.

User answer:

> 17A

Decision:

- Before auto-attach, print one concise status line with the generated tmux session name and the implementer/reviewer backend mapping.
- Do not print a longer instruction block or both mkpipe paths in v1.

### 18. How strict should the inter-agent handoff markers be?

Options presented:

- A: Require exact literal markers for the key states like implementation ready, changes requested, approved, and blocked, so both agents and humans can scan them reliably. (Recommended)
- B: Use only natural-language instructions and no exact markers.
- C: Use JSON payloads over mkpipe.
- D: Reuse the old `<promise>...` marker family unchanged.

User answer:

> 18A

Decision:

- The inter-agent protocol should use exact literal markers for key states.
- These markers should be human-scannable rather than structured JSON.

## Round 4

### 19. With `mkpipe` remaining attach-scoped, who should own the two FIFO listeners during the shared-session run?

Options presented:

- A: Each agent runtime resolves and exposes its mkpipe path, but the `implement-with-reviewer` command process owns both attach-scoped listeners for the duration of the attached run. (Recommended)
- B: Each agent runtime should own its own attach-scoped listener directly, even though the final tmux attach happens only once at the shared-session level.
- C: The command should generate and own the FIFO paths too; the runtimes should know nothing about mkpipe.
- D: Revisit the earlier choice and make mkpipe runtime-owned and detach-safe.

User answer:

> 19A, we don't need much implementation change for how the mkpipe is resolved

Decision:

- The `implement-with-reviewer` command process should own the two attach-scoped listeners for the duration of the attached run.
- Follow-up clarification: the orchestrator should be able to create the two runtimes, resolve the two mkpipe paths with minimal resolver churn, start the listeners, and then send the initial prompts that include the peer paths.
- The important correctness rule is that both FIFOs must already exist and be listening before either initial prompt is sent, so the first peer-to-peer write cannot race a missing listener.

### 20. What exact short alias spelling should the documented CLI contract use?

Options presented:

- A: Document `--i` and `--r` as the short aliases, while accepting `-i` and `-r` too if the parser naturally allows them. (Recommended)
- B: Document only `-i` and `-r`.
- C: Document both `--i`/`--r` and `-i`/`-r` equally.
- D: Drop the short aliases from the contract.

User answer:

> 20. B

Decision:

- The CLI contract should document short aliases as `-i` and `-r`.
- The documented canonical long forms remain `--implementer` and `--reviewer`.

### 21. Given that each agent runtime should own only its pane/backend lifecycle and not the tmux-session lifecycle, what should `Close()` become?

Options presented:

- A: Add pane-close support in the tmux layer and make runtime `Close()` close only its own pane/backend, never the whole tmux session. (Recommended)
- B: Remove `Close()` from the runtime API for now; tmux-session cleanup stays entirely at the command layer.
- C: Keep `Close()` but let it still kill the whole tmux session, even in shared-session mode.
- D: Make `Close()` a no-op.

User answer:

> A

Decision:

- Add pane-close support in the tmux layer.
- Runtime `Close()` should close only its own pane/backend and must never kill the whole tmux session.
- Whole-session cleanup remains a command responsibility.
