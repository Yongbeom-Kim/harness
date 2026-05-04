# Tmux Input Delivery Guard Implementation Decisions Log

Date: 2026-05-04
Topic: implementation architecture for tmux pane interactivity checks and send delivery guards

## Design Input

- Current conversation request to plan a fix for cases where `tmux send-keys -l` succeeds but the target app receives nothing.
- Existing tmux/runtime/backend implementation under `orchestrator/internal/agentruntime/*`.
- Local reproduction notes gathered during context exploration on `tmux 3.6a`.

## Explored Context

- `orchestrator/internal/agentruntime/tmux/tmux.go` currently sends prompt text with repeated `tmux send-keys -l` chunks, then relies on a fixed `25ms` settle before `PressKey` sends `Enter` or `Tab`.
- `orchestrator/internal/agentruntime/backend/backend.go` owns the generic `sendTextAndKeys(...)` sequencing that all backend launch and prompt flows use.
- `orchestrator/internal/agentruntime/runtime.go` serializes each send sequence with `sendMu`, so the current non-delivery bug is more likely pane-state loss than interleaving inside one runtime instance.
- `orchestrator/internal/agentruntime/tmux/interfaces.go` exposes only `SendText`, `PressKey`, `Capture`, and `Close`, making the tmux layer the narrowest shared place to enforce pane interactivity before all launch and prompt sends.
- `orchestrator/internal/agentruntime/tmux/errors.go` currently surfaces only raw tmux command failures; it has no typed way to report "tmux accepted the command but the pane was not interactive".
- `orchestrator/internal/agentruntime/tmux/tmux_test.go` is the natural place for pane-state guard and self-heal tests because it already stubs `runTmuxCommand` and `sleepBeforePressKey`.
- `orchestrator/internal/agentruntime/runtime_test.go` already covers send serialization and mkpipe forwarding, so it should stay focused on runtime semantics rather than low-level pane-state parsing.
- Local tmux repros showed that `tmux send-keys -l` can return success while delivering zero bytes when `#{pane_input_off}=1` or `#{pane_in_mode}=1`.
- Local tmux repros also confirmed that `tmux copy-mode -q -t <pane>` exits pane modes and `tmux select-pane -e -t <pane>` re-enables pane input, which makes them viable low-level recovery actions.

## Round 1

1. Which layer should be the primary owner for guarding against "tmux accepted the command but the pane could not receive input"?
A: Make `orchestrator/internal/agentruntime/tmux` the primary owner by enforcing pane interactivity inside `TmuxPane.SendText` and `TmuxPane.PressKey`, so launch, direct prompts, and mkpipe prompts all inherit the same guard automatically. (Recommended)
B: Keep tmux as a dumb transport and put the guard in `orchestrator/internal/agentruntime/runtime.go` before every backend send.
C: Push the guard into backend adapters so each CLI can decide when a pane is interactive enough for its own send semantics.
D: Leave the shared runtime untouched and add the guard only in command packages that currently hit the bug.

2. When tmux reports a non-interactive pane state, how aggressive should the fix be about self-healing before failing?
A: Perform bounded tmux-local recovery first by running `copy-mode -q` to exit pane modes and `select-pane -e` to re-enable pane input, then re-read pane state and fail if the pane is still non-interactive. (Recommended)
B: Detect the bad state and fail immediately without any tmux-side mutation so callers always see the original condition.
C: Retry `send-keys` a few times with backoff and never mutate pane state directly.
D: Escalate to session-level recovery such as reattach/restart when pane input is blocked.

3. Should this feature add a post-send delivery verification step before the follow-up `Enter` or `Tab` submit key?
A: Yes. Capture pane content before text injection, poll for a bounded after-capture change, and suppress the follow-up submit key if the pane never shows any visible change. (Recommended)
B: No. Pre-send pane-state gating plus better errors is enough for this fix; do not add capture-based delivery verification yet.
C: Only verify after queued sends, not immediate sends, because queued prompts are less urgent.
D: Only verify in debug/test builds and keep production sends fire-and-forget.

4. How should this feature treat pane interactivity during startup/readiness versus only during later prompt sends?
A: Enforce the pane-interactivity guard for both launch-time key injection and later prompt sends by keeping the check in the tmux pane layer, but do not change backend `WaitUntilReady(...)` contracts yet. (Recommended)
B: Enforce the guard only for later prompt sends and leave launch-time injection untouched to minimize startup behavior changes.
C: Extend backend readiness matchers so a runtime is never considered ready until pane interactivity checks also pass.
D: Move all interactivity checking into readiness and skip explicit send-time checks.

5. How should the new failure surface be exposed when tmux accepts a command but the pane is non-interactive or delivery verification fails?
A: Add a new typed tmux-layer error that includes the pane target, the attempted operation, and a pane-state snapshot (`pane_input_off`, `pane_in_mode`, `pane_mode`, `pane_dead`, `pane_current_command`) so runtime callers get actionable diagnostics. (Recommended)
B: Reuse `SendKeysError` and stuff the pane-state snapshot into its wrapped error string.
C: Log pane-state diagnostics to stderr but keep returning only the existing wrapped runtime error text.
D: Return raw `fmt.Errorf` strings from `tmux.go` and avoid any new error types in this fix.

User answer:

> 1A 2A with bounded retry before failing (about five attempts) 3A 4A 5A

Decisions:

- Put pane interactivity guarding in `orchestrator/internal/agentruntime/tmux`, inside the shared pane send path, so every launch and prompt send inherits the same behavior.
- When a pane is non-interactive, perform bounded tmux-local recovery first: exit pane modes with `copy-mode -q`, re-enable input with `select-pane -e`, re-check pane state, and retry a small bounded number of times before failing.
- Add post-send delivery verification before submit keys. Capture before text injection, poll for bounded after-capture change, and suppress the follow-up `Enter` or `Tab` if visible delivery never occurs.
- Apply the tmux-layer interactivity guard to both launch-time injection and later prompt sends, while leaving backend readiness contracts unchanged in this feature.
- Add a typed tmux-layer error that reports the pane target, attempted operation, and a pane-state snapshot so higher layers get actionable diagnostics.

## Round 2

6. Where should the bounded retry loop live relative to the tmux-layer interactivity check and the backend/runtime layers?
A: Keep the retry loop entirely inside `orchestrator/internal/agentruntime/tmux/tmux.go` around one shared "ensure interactive then send" helper, so callers still observe a single send attempt with deterministic tmux-local recovery semantics. (Recommended)
B: Put the retry loop in `orchestrator/internal/agentruntime/backend/backend.go` so backends can retry `sendTextAndKeys(...)` as one unit.
C: Put the retry loop in `orchestrator/internal/agentruntime/runtime.go` so launch sends and prompt sends can share one outer retry wrapper.
D: Split retries across layers: tmux retries state repair, backend retries delivery verification, runtime retries wrapped errors.

7. After a successful tmux-local recovery, how much pane-state stability should the sender require before injecting text?
A: Require only a fresh pane-state read that shows interactive values (`input_off=0`, `in_mode=0`, `dead=0`) and proceed immediately; rely on post-send delivery verification for any remaining races. (Recommended)
B: Add a second quiet-period wait in the tmux layer and require two consecutive interactive pane-state reads before every send.
C: Sleep a fixed delay after every successful recovery before sending, regardless of pane state.
D: Skip post-send verification if recovery succeeded because the pane is now known-good.

8. What should count as "delivery verified" for the post-send check before `Enter` or `Tab`?
A: Treat any visible capture change from the pre-send snapshot as sufficient delivery evidence, because the shared tmux layer cannot reliably parse backend-specific compose widgets. (Recommended)
B: Require the full tail of the prompt text to appear in the capture before allowing the submit key.
C: Require backend-specific prompt-box markers by adding a new verification hook to each backend adapter.
D: Verify only that tmux cursor coordinates changed, not capture content.

9. Which operations should use the new delivery verification gate?
A: Verify text sends only when they are followed by a submit key in the same logical sequence; standalone `SendText` stays unsubmitted but still uses pre-send interactivity checks. (Recommended)
B: Verify every `SendText`, even when no key will follow immediately.
C: Verify only immediate sends (`Enter`) and skip queued sends (`Tab`).
D: Verify only launch-time sends and not later prompts.

10. How should tests split between tmux, backend, and runtime packages for this fix?
A: Put pane-state parsing, tmux-local recovery, bounded retry, and delivery-verification behavior in `orchestrator/internal/agentruntime/tmux/tmux_test.go`; keep backend tests focused on launch/prompt choreography and runtime tests focused on state/mkpipe/error propagation. (Recommended)
B: Put most verification logic in `runtime_test.go` because the runtime owns user-visible send APIs.
C: Put the new tests mainly in backend tests because send verification matters only when a backend submits input.
D: Rely mostly on manual tmux repros and add only one happy-path unit test in the tmux package.

11. Which product/docs surfaces should explicitly mention the new pane-interactivity guard behavior?
A: Update only code comments and error messages; operator-facing contract docs should stay unchanged because this is an internal reliability fix.
B: Update the tmux-layer code comments plus the command contract docs' failure semantics to mention that sends may fail when tmux pane input remains disabled/non-interactive after bounded auto-recovery, because this is now an operator-visible failure mode. (Recommended)
C: Document the new behavior only in `implement-with-reviewer/CONTRACT.md` because that workflow is the most automation-heavy caller.
D: Add a new top-level troubleshooting doc outside the existing contract docs and leave current command contracts unchanged.

User answer:

> 6A 7A 8A 9A 10A 11A

Decisions:

- Keep the bounded retry loop entirely inside `orchestrator/internal/agentruntime/tmux/tmux.go` around one shared tmux-layer helper so higher layers still see one logical send attempt.
- After tmux-local recovery succeeds, proceed as soon as a fresh pane-state read shows the pane is interactive; do not add a second quiet-period gate before sending.
- Treat any visible capture change from the pre-send snapshot as sufficient delivery evidence for the shared post-send verification gate.
- Apply delivery verification only to text sends that are followed by a submit key in the same logical sequence; standalone `SendText` gets pre-send interactivity checks but not mandatory post-send verification.
- Keep pane-state parsing, auto-recovery, retry, and delivery-verification tests in `orchestrator/internal/agentruntime/tmux/tmux_test.go`; keep backend tests focused on gesture choreography and runtime tests focused on state, mkpipe, and error propagation.
- Do not change operator-facing contract docs for this fix; limit behavior explanation to code comments and actionable error messages.

## Round 3

12. Given the existing split between `SendText` and `PressKey`, where should the "pending text that must be verified before submit" state live?
A: Keep it private inside `TmuxPane`, next to the existing settle-before-next-key behavior, so `SendText` records the pre-send snapshot and `PressKey` can decide whether to verify before `Enter` or `Tab`. (Recommended)
B: Move that state into `backend.sendTextAndKeys(...)` and keep `TmuxPane` stateless.
C: Add a new explicit pane method such as `SendTextForSubmit(text string)` and make backends stop calling plain `SendText`.
D: Put the state in `Runtime` so all backends share one outer delivery-verification tracker.

13. If text injection finishes but post-send verification never sees a capture change, should the fix retry the text injection itself?
A: No. Treat that as a terminal delivery-verification failure, suppress the submit key, and return a typed error; only pre-send pane-state repair gets retried. (Recommended)
B: Yes. Re-run the full text injection up to the same retry budget before failing.
C: Retry text injection only if the pane is still interactive and the capture is byte-for-byte unchanged.
D: Clear the compose box with extra keypresses, then retry the text injection.

14. Which follow-up keys should trigger the submit-time verification gate when there is pending text from a prior `SendText`?
A: Only `Enter` and `Tab`, because those are the current harness submit/queue gestures; `PressKey` remains generic for other callers and should not assume every key is a submission gesture. (Recommended)
B: Any key after `SendText` should require verification because any key could matter.
C: Add a backend-supplied allowlist of verification-triggering keys.
D: Only `Enter` should trigger verification; `Tab` should bypass it.

15. Where should the new pane-state snapshot type and typed non-interactive/delivery-verification errors live?
A: Add the typed error definitions to `orchestrator/internal/agentruntime/tmux/errors.go`, and keep pane-state snapshot parsing/helpers private to `tmux.go` unless tests need a small exported-free helper. (Recommended)
B: Put both the error types and pane-state snapshot struct directly in `tmux.go` to keep everything in one file.
C: Put the new typed errors in `orchestrator/internal/agentruntime/errors.go` because runtime callers ultimately surface them.
D: Reuse only anonymous structs and raw `fmt.Errorf` values; do not add any new named type for pane state.

16. How should timing for recovery retries and post-send verification polling be configured?
A: Keep private tmux-package defaults as constants/vars in `tmux.go`, with the same style of test seams already used for `sleepBeforePressKey`, rather than threading new timing config through runtime or backend APIs. (Recommended)
B: Thread all timing through `Runtime.deps` so callers can tune retry and verification behavior per runtime.
C: Add backend-specific timing knobs because Codex, Claude, and Cursor differ.
D: Expose CLI flags or env vars so operators can tune recovery and verification timing manually.

User answer:

> 12A 13A 14A 15A 16A

Decisions:

- Keep the pending "text must be verified before submit" state private to `TmuxPane`, alongside the existing send-to-next-key state.
- If post-send verification never sees a visible capture change, treat that as a terminal delivery-verification failure, suppress the submit key, and do not retry the text injection itself.
- Trigger submit-time verification only for `Enter` and `Tab` when they follow a prior `SendText`; `PressKey` remains generic for all other keys.
- Put the new typed tmux errors in `orchestrator/internal/agentruntime/tmux/errors.go`, while keeping pane-state snapshot parsing and helpers private to `tmux.go`.
- Keep retry and verification timing as private tmux-package defaults with local test seams, rather than threading new configuration through runtime, backend, or CLI surfaces.
