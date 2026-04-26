# Raw Decisions

## 2026-04-26 Seed Direction

- Decision: use `tmux` to wrap persistent `codex` / `claude` sessions instead of relying on one-shot `codex exec --resume` or `claude -p` pipe-based invocations.
- Reasons:
  - keeps agent context alive across turns
  - allows mid-run interruption and manual intervention
  - enables live progress capture via `tmux capture-pane`
  - lets agents surface clarification requests during a run
  - allows implementer and reviewer sessions to run in parallel
  - improves observability

## Current Codebase Context

- `orchestrator/cmd/implement-with-reviewer` currently runs a serial loop and expects each backend call to return a complete `stdout` / `stderr` result when the subprocess exits.
- `orchestrator/internal/cli/codex.go` and `orchestrator/internal/cli/claude.go` currently implement backend-specific session handling through CLI invocations rather than persistent terminal sessions.

## 2026-04-26 Question Round 1

1. What should the first design scope be?
- Answer: `D`
- Interpretation: focus this feature on wrapping `internal/cli` around persistent tmux-backed sessions while keeping the user-facing UX the same for now.

2. Where should tmux session lifecycle live in the architecture?
- Answer: `B`
- Interpretation: expand the current `CliTool` abstraction into a sessionful interface, possibly renamed to reflect persistent session semantics.

3. How should human intervention work in v1?
- Answer: `D`
- Interpretation: do not support human intervention in v1; use tmux for persistence and observability only.

4. What should the first observability contract be?
- Answer: `A`
- Interpretation: persist a structured run log plus pane snapshot/capture references while still allowing live pane inspection.

5. How should sessions be addressed in v1?
- Answer: `A`
- Interpretation: use deterministic per-run, per-role tmux sessions or panes keyed by a run ID.

6. Which backend scope should the first design commit to?
- Answer: `A`
- Interpretation: support `codex` and `claude` only in the first version.

## 2026-04-26 Question Round 2

7. How should persistent session identity relate to backend-native session IDs?
- Answer: `A`
- Interpretation: tmux session identity replaces stored backend-native session IDs inside the adapters for this feature; each adapter owns one persistent interactive process per tmux pane and reuses it.

8. How should v1 detect that a backend is ready for the next prompt?
- Answer: `B`
- Interpretation: inject an explicit `<promise>done</promise>` marker into every turn and wait for that marker to detect turn completion.

9. If an agent outputs a clarification request or says it is blocked, what should v1 do?
- Answer: `B`
- Interpretation: treat blocked or clarification text as ordinary output and continue, matching the current `implement-with-reviewer` behavior.

10. What should happen to tmux sessions after a run finishes?
- Answer: `B`
- Interpretation: kill tmux sessions immediately on both success and failure.

11. What should the persisted artifact layout look like in v1?
- Answer: `A`
- Interpretation: persist one run directory with metadata, state transitions, pane capture files, and final outcome files.

12. How should backend launch behavior be owned?
- Answer: custom
- Interpretation: `codex` and `claude` adapters own tmux semantics directly, and tmux behavior is exposed through backend wrapper methods rather than through a shared tmux/session layer.

## 2026-04-26 Question Round 3

13. For blocked / clarification text, should question 9 be recorded as "treat it like ordinary output and continue," matching today's behavior?
- Answer: `A`
- Interpretation: yes; record question 9 as the current behavior, with no special blocked-state handling in v1.

14. What tmux topology should each run use?
- Answer: `A`
- Interpretation: create one tmux session per run, with one pane per role.

15. What should happen to the `<promise>done</promise>` sentinel in captured output?
- Answer: `B`
- Interpretation: forward the sentinel in returned and captured output verbatim to keep the system simple.

16. If the sentinel never appears for a turn, what should v1 do?
- Answer: `A`
- Interpretation: fail the turn with a timeout error, persist pane captures, and kill the tmux session(s).

17. Where should runtime run artifacts live?
- Answer: `A`
- Interpretation: store runtime artifacts under a repo-local runtime directory such as `log/runs/<run-id>/`, separate from `docs/development/`.

18. How should the adapter API evolve from today’s single `SendMessage` shape?
- Answer: `A`
- Interpretation: replace the single-call API with explicit session methods such as `Start`, `Send`, `WaitForDone`, `Capture`, and `Close`.

## 2026-04-26 Question Round 4

19. What timeout model should v1 use?
- Answer: custom
- Interpretation: use only a per-turn idle timeout in v1; do not add a hard per-turn cap.

20. What default per-turn timeout should v1 start with?
- Answer: `120s`
- Interpretation: default the idle timeout to 120 seconds.

21. Should timeout be configurable by callers?
- Answer: `D`
- Interpretation: keep timeout fixed in v1.

22. Should implementer and reviewer share the same timeout budget?
- Answer: `A`
- Interpretation: use the same per-turn timeout for both roles in v1.

23. How should session startup timeout work?
- Answer: `C`
- Interpretation: no separate startup timeout in v1.

24. How should the adapter get `<promise>done</promise>` emitted?
- Answer: `A`
- Interpretation: append a per-turn instruction suffix telling the agent to end the response with exactly `<promise>done</promise>`.

## 2026-04-26 Question Round 5

25. Should `implement-with-reviewer` preserve its current CLI and stdout contract in v1?
- Answer: `A`
- Interpretation: keep the existing flags, validation rules, transcript banners, and success/failure output contract; add runtime artifacts only on disk.

26. How should run IDs for `log/runs/<run-id>/` be generated?
- Answer: custom
- Interpretation: use UUIDv7 for run IDs.

27. When should pane captures be persisted?
- Answer: `A`
- Interpretation: persist captures after each completed turn, and again on timeout or finalization.

28. How should adapters inject multi-line prompts into tmux panes?
- Answer: `A`
- Interpretation: use tmux buffer/paste semantics so multi-line prompts are sent verbatim.

29. How should stable instructions be split between session startup and per-turn prompts?
- Answer: `A`
- Interpretation: send one startup prompt with stable interaction rules, then send turn-specific prompts with the current task plus the `<promise>done</promise>` suffix each turn.

30. How should the adapter isolate new output for a turn in a persistent pane?
- Answer: custom
- Interpretation: choose the simplest isolation strategy in v1 by clearing pane history/display between turns after captures are persisted, so each new turn can be captured without diff logic.

## 2026-04-26 Question Round 6

31. Should the adapter strip trailing `<promise>done</promise>` from the semantic turn result before the orchestrator reuses it, while leaving raw pane captures unchanged?
- Answer: `B`
- Interpretation: keep `<promise>done</promise>` everywhere, including reviewer inputs and final printed output, to keep the implementation simple.

32. When should the reviewer pane start?
- Answer: `A`
- Interpretation: start both implementer and reviewer panes at run start inside the tmux session.

33. If `tmux` is unavailable or backend launch fails before any turn completes, how should the command fail?
- Answer: `A`
- Interpretation: fail fast as a runtime failure with exit code `1`, surfacing the error on `stderr`.

34. If the agent loop succeeds logically but writing captures/artifacts fails, what should happen?
- Answer: `A`
- Interpretation: artifact persistence failure is a command failure.

35. What should pane capture files contain?
- Answer: `A`
- Interpretation: persist raw tmux pane text exactly as captured, including prompts, sentinel, and CLI noise.

36. What test scope should the design require for v1?
- Answer: `C`
- Interpretation: require tmux integration tests for the v1 design rather than centering the design on unit-test-specific seams.
