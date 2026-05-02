# Implementation Decisions

## 2026-04-27 Exploration Context

- `implement-with-reviewer` currently owns run lifecycle, session startup ordering, turn execution, and artifact persistence in `orchestrator/internal/implementwithreviewer/runner.go`.
- Runtime artifacts currently flow through `ArtifactSink` (`WriteMetadata`, `AppendTransition`, `WriteCapture`, `WriteResult`) and are implemented by `ArtifactWriter` with files under `log/runs/<run-id>/`.
- Backend startup and turn execution go through `cli.Session`, but that interface only exposes `Start`, `RunTurn`, `SessionName`, and `Close`.
- The current persistent session implementation already hides tmux details behind `cli.Session`, while `tmux.TmuxPaneLike.SendText` is the only primitive that can inject raw text into a live pane without waiting for `<promise>done</promise>`.
- The design requires immediate side-channel injection during active turns, dropped-not-started behavior before a destination role finishes startup, fixed FIFO paths in the working directory, and a dedicated `channel-events.jsonl` artifact.

## 2026-04-27 Question Round 1

1. How should the runner inject a side-channel message into a live destination session?
A: Extend `cli.Session` with a narrow async method such as `InjectSideChannel(message string) error` that writes prompt text into the live pane without appending completion instructions or waiting for a turn result. This keeps tmux hidden behind the existing backend/session boundary. `(Recommended)`
B: Define a second optional interface (for example `type SideChannelReceiver interface { InjectSideChannel(string) error }`) and have the runner type-assert sessions that support it.
C: Expose tmux pane or pane-writer objects from `cli` to `implementwithreviewer` and let the runner inject directly at the tmux layer.
D: Reuse `RunTurn` with a special prompt shape for side-channel deliveries, even though that turns delivery into a normal blocking turn.

2. Where should FIFO path setup, reader goroutines, and delivery orchestration primarily live?
A: Add a feature-specific side-channel module inside `orchestrator/internal/implementwithreviewer` (for example `sidechannel.go` plus tests) so FIFO lifecycle, destination readiness, and artifact semantics stay close to the runner that owns both roles. `(Recommended)`
B: Create a new generic package such as `orchestrator/internal/filechannel` that owns FIFO creation and reader loops, with `implementwithreviewer` providing callbacks for delivery and logging.
C: Fold everything directly into `runner.go` with no new helper module.
D: Put FIFO lifecycle into `orchestrator/internal/cli` because sessions eventually receive the injected text.

3. Where should the startup prompt gain the file-channel instructions and fixed FIFO paths?
A: Keep backend-specific `cli` startup instructions unchanged, and have `implementwithreviewer.startSessions()` compose the role prompt plus a feature-owned side-channel instruction block before calling `session.Start(...)`. The paths are run-context data owned by the feature, not by the backend adapters. `(Recommended)`
B: Move the side-channel instructions into `orchestrator/internal/cli` startup prompt decoration so every backend owns both persistent-session startup and channel explanation text.
C: Edit the static `ImplementerRolePrompt` and `ReviewerRolePrompt` constants to include the FIFO instructions directly.
D: Start sessions with the existing prompts, then inject a second post-start message containing the channel instructions.

4. What should be the exact source of truth for whether a destination is "ready to receive" and early messages should be delivered instead of logged as `dropped_not_started`?
A: Track a per-role side-channel readiness flag in the runner and set it only after that role's `session.Start(...)` call returns successfully, since that is when the startup prompt has been fully written by the current session contract. `(Recommended)`
B: Mark a role ready immediately after tmux session creation, before startup prompt delivery.
C: Infer readiness from artifact transitions or capture text after the fact instead of keeping runtime state.
D: Use a fixed time delay after startup begins and assume the role is ready once the delay expires.

5. How should channel event persistence fit the existing artifact-writing boundary?
A: Extend `ArtifactSink` and `ArtifactWriter` with a dedicated `AppendChannelEvent(ChannelEvent) error` API that writes to `log/runs/<run-id>/channel-events.jsonl`, keeping channel logging as a first-class artifact parallel to transitions and captures. `(Recommended)`
B: Reuse `AppendTransition(...)` and encode channel-specific metadata into `StateTransition.Details`.
C: Have the runner write `channel-events.jsonl` directly with `os.OpenFile`, bypassing `ArtifactSink`.
D: Avoid a dedicated log file and rely on pane captures plus normal transitions only.

6. What test strategy should v1 plan around for the FIFO read path?
A: Cover the FIFO lifecycle and EOF-delimited message boundary with real named-pipe tests in temp directories, while keeping runner/session delivery tests stub-driven. This gives direct confidence in the most platform-specific behavior without requiring real tmux sessions. `(Recommended)`
B: Hide FIFO reads behind a reader abstraction and test only with in-memory stubs.
C: Skip automated FIFO tests and rely on manual validation of the feature end to end.
D: Add a full integration suite that requires real tmux sessions and live agent backends in CI.

- Answers: `1A2B3A4A5A6A`
- Interpretations:
  - `1A`: extend `cli.Session` with a first-class asynchronous side-channel injection method instead of leaking tmux details into the runner.
  - `2B`: create a new generic `orchestrator/internal/filechannel` package for FIFO setup and reader lifetime, while `implementwithreviewer` owns role mapping, readiness, and delivery semantics.
  - `3A`: compose the FIFO instructions in `implementwithreviewer.startSessions()` instead of moving run-context prompt text into backend adapters.
  - `4A`: track destination readiness in runner state and only mark a role ready after its `session.Start(...)` call completes.
  - `5A`: extend `ArtifactSink` / `ArtifactWriter` with a dedicated `AppendChannelEvent` path for `channel-events.jsonl`.
  - `6A`: plan real FIFO tests in temp directories plus stub-driven runner/session tests; do not rely on full tmux integration coverage.

## 2026-04-27 Question Round 2

7. What should the new generic `orchestrator/internal/filechannel` package expose to `implementwithreviewer`?
A: A small manager API that owns FIFO creation/removal plus one background EOF-delimited reader loop per configured path, and emits raw message events back through callbacks/channels. `implementwithreviewer` still owns role mapping, readiness checks, envelope building, and artifact logging. `(Recommended)`
B: Only low-level helpers like `CreateFIFO`, `Remove`, and `ReadOneMessage`, with `implementwithreviewer` owning all goroutines and lifecycle.
C: A specialized two-channel API hard-coded to implementer/reviewer semantics.
D: Only a thin `Mkfifo` wrapper; keep all actual logic in `runner.go`.

8. How should the new package create the FIFO files on this Unix-only harness?
A: Use Go’s native Unix APIs (`syscall.Mkfifo` plus `os.OpenFile` / `os.Remove`) instead of shelling out to `mkfifo`, so the feature stays self-contained and unit-testable. `(Recommended)`
B: Shell out to the `mkfifo` command from Go.
C: Use regular files in tests and FIFOs only in production.
D: Replace FIFOs with Unix domain sockets.

9. How should reader runtime failures propagate back from `filechannel` to the runner?
A: The file-channel manager should surface setup/read failures to `implementwithreviewer` through an error channel or `Wait`/`Err` method, and the runner should treat those as terminal run failures. `(Recommended)`
B: The manager should log the error internally and keep the run alive.
C: The manager should silently restart failed readers forever.
D: Reader failures should only become `channel-events.jsonl` records and never fail the run.

10. Where should the `<side_channel_message>...</side_channel_message>` wrapper be constructed?
A: In `implementwithreviewer`, via a small feature-owned helper, because the wrapper is part of this command’s orchestration contract rather than a generic FIFO or backend-session concern. `(Recommended)`
B: In `cli.Session.InjectSideChannel(...)`.
C: In `internal/filechannel`.
D: Inline in `runner.go` with no helper.

11. Where should the `channel-events.jsonl` schema live?
A: Define `ChannelEvent` plus status constants in `orchestrator/internal/implementwithreviewer`, alongside the other run-artifact schemas; `filechannel` should only report raw reads/errors. `(Recommended)`
B: Define `ChannelEvent` in `orchestrator/internal/filechannel` and reuse it as the artifact schema directly.
C: Avoid a typed schema and write anonymous JSON maps from the artifact writer.
D: Reuse `StateTransition` instead of adding a channel-specific event type.

12. What file layout should we plan for the new generic package?
A: Start minimal with `orchestrator/internal/filechannel/fifo.go` and `orchestrator/internal/filechannel/fifo_test.go`; only split further if the implementation actually forces it. `(Recommended)`
B: Start with `manager.go`, `fifo.go`, `types.go`, and `manager_test.go` immediately.
C: Put the package in one file and skip dedicated tests.
D: Start with platform-specific files like `fifo_unix.go` and `fifo_unix_test.go`.

13. What shutdown order should the runner use for side-channel infrastructure?
A: Stop the file-channel manager first, then close agent sessions, then remove the FIFO paths during cleanup so no reader/delivery races happen against session teardown. `(Recommended)`
B: Close agent sessions first, then stop the file-channel manager.
C: Leave readers running until process exit and only remove FIFOs opportunistically.
D: Remove the FIFO paths immediately after startup once the readers are open.

- Answers: `7A8A9A10A11A12A13A`
- Interpretations:
  - `7A`: `filechannel` should own FIFO path setup/removal and one reader loop per path, but `implementwithreviewer` still owns orchestration semantics.
  - `8A`: create FIFOs with native Unix APIs rather than shelling out.
  - `9A`: any file-channel infrastructure failure after startup is terminal and must propagate back to the runner.
  - `10A`: keep the XML-like delivery wrapper in `implementwithreviewer`, not in generic transport/session layers.
  - `11A`: keep `ChannelEvent` as part of the run artifact schema inside `implementwithreviewer`.
  - `12A`: keep the new generic package minimal with one implementation file and one test file initially.
  - `13A`: shut down the file-channel manager before session cleanup and FIFO removal to avoid delivery races during teardown.
