# File-Channel Communication for `implement-with-reviewer` Implementation Plan

**Goal:** Add a FIFO-backed side channel between implementer and reviewer sessions so either role can send explicit out-of-band messages without changing the existing CLI surface or main review loop.

**Architecture:** Add a small generic `orchestrator/internal/filechannel` package that owns FIFO creation, dedicated read loops, and shutdown/remove lifecycle for fixed pipe paths. Keep `orchestrator/internal/implementwithreviewer` as the orchestration owner: it will compose startup instructions, track per-role readiness, translate raw FIFO reads into wrapped side-channel deliveries, log `channel-events.jsonl`, and fail the run when file-channel infrastructure breaks. Extend `cli.Session` with a narrow asynchronous injection method so the runner can deliver wrapped prompt text into a live tmux-backed session without exposing tmux details across package boundaries.

**Tech Stack:** Go 1.26, Unix FIFOs via `syscall.Mkfifo`, existing tmux-backed `cli.Session` transport, JSONL run artifacts under `log/runs/<run-id>/`.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Create `./to_reviewer.pipe` and `./to_implementer.pipe` before either agent session starts, remove any existing paths first, and recreate them as FIFOs after the directory lock is already held. | `FileChannelManager` | `RunnerSideChannelLifecycle` | `orchestrator/internal/filechannel/fifo.go`, `orchestrator/internal/implementwithreviewer/runner.go` | `filechannel.NewManager(filechannel.Config{Paths: ...})`, `prepareFIFO(path string)` | `orchestrator/internal/filechannel/fifo_test.go` for stale-path replacement and FIFO creation; `orchestrator/internal/implementwithreviewer/runner_test.go` for startup ordering |
| R2 | Start one dedicated background reader per FIFO before agent startup; one message is one writer open-write-close cycle, with EOF as the boundary. | `FileChannelManager` | `RunnerSideChannelLifecycle` | `orchestrator/internal/filechannel/fifo.go`, `orchestrator/internal/filechannel/fifo_test.go` | `Manager.Messages() <-chan filechannel.Message`, `readMessage(path string)` | `orchestrator/internal/filechannel/fifo_test.go` with real FIFOs proving one message per open-write-close cycle |
| R3 | Keep the external CLI unchanged while exposing the side channel through startup prompts that show the literal relative FIFO paths and the write contract. | `SideChannelDeliveryCoordinator` | `RunnerSideChannelLifecycle` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `BuildStartupPrompt(role, basePrompt string) string` (or internal `buildStartupPrompt` with the same contract), `session.Start(prompt string)` | `orchestrator/internal/implementwithreviewer/sidechannel_test.go` for prompt text; `orchestrator/internal/implementwithreviewer/runner_test.go` for start flow |
| R4 | Treat a destination as ready to receive side-channel traffic only after that role’s `session.Start(...)` call returns successfully; messages to a not-yet-ready destination are dropped and logged as `dropped_not_started`. | `SideChannelDeliveryCoordinator` | `RunnerSideChannelLifecycle` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/sidechannel_test.go`, `orchestrator/internal/implementwithreviewer/runner_test.go` | `MarkReady(role string)`, `HandleMessage(msg filechannel.Message)`, `ChannelStatusDroppedNotStarted` | `orchestrator/internal/implementwithreviewer/sidechannel_test.go` for readiness gating; `runner_test.go` for startup-order regression |
| R5 | Deliver non-empty side-channel bodies immediately into the destination tmux pane using the neutral `<side_channel_message>...</side_channel_message>` wrapper, without changing approval, iteration, or other control-flow semantics. | `SideChannelDeliveryCoordinator` | `SessionSideChannelReceiver`, `RunnerSideChannelLifecycle` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/cli/interface.go`, `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go`, `orchestrator/internal/implementwithreviewer/runner.go` | `wrapSideChannelMessage(body string)`, `Session.InjectSideChannel(message string) error` | `orchestrator/internal/cli/command_test.go` for async injection transport; `orchestrator/internal/implementwithreviewer/sidechannel_test.go` for wrapper shape and delivery decisions |
| R6 | Empty or whitespace-only FIFO bodies are not delivered; they are logged as `dropped_empty` and the run continues. | `SideChannelDeliveryCoordinator` | `ChannelArtifactLog` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/sidechannel_test.go`, `orchestrator/internal/implementwithreviewer/types.go` | `HandleMessage(msg filechannel.Message)`, `ChannelStatusDroppedEmpty` | `orchestrator/internal/implementwithreviewer/sidechannel_test.go` for trim/drop behavior |
| R7 | If tmux-side injection fails after a message is read successfully, record `delivery_failed` with raw body and continue the run. | `SideChannelDeliveryCoordinator` | `SessionSideChannelReceiver`, `ChannelArtifactLog` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/sidechannel_test.go`, `orchestrator/internal/cli/interface.go` | `Session.InjectSideChannel(...)`, `ChannelStatusDeliveryFailed` | `orchestrator/internal/implementwithreviewer/sidechannel_test.go` with a failing fake session receiver |
| R8 | File-channel setup failures and reader runtime failures are terminal run failures; they must propagate back into the runner even though individual message delivery failures are non-fatal. | `RunnerSideChannelLifecycle` | `FileChannelManager` | `orchestrator/internal/implementwithreviewer/types.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/runner_test.go`, `orchestrator/internal/filechannel/fifo.go` | `RunConfig.NewFileChannelManager`, `monitorSideChannelErrors()`, `Manager.Errors() <-chan error` | `orchestrator/internal/implementwithreviewer/runner_test.go` for fatal startup/read-path errors; `orchestrator/internal/filechannel/fifo_test.go` for surfaced reader errors |
| R9 | Persist a dedicated `log/runs/<run-id>/channel-events.jsonl` artifact that records timestamp, source role, destination role, channel path, status, and raw body when available. | `ChannelArtifactLog` | `SideChannelDeliveryCoordinator` | `orchestrator/internal/implementwithreviewer/types.go`, `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/internal/implementwithreviewer/artifact_paths.go`, `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/runner_test.go` | `ArtifactSink.AppendChannelEvent(ChannelEvent) error`, `channelEventsPath`, `ChannelEvent`, status strings per design: `delivered`, `delivery_failed`, `dropped_empty`, `dropped_not_started`, `reader_error` (use `reader_error` when logging a channel infrastructure failure to JSONL before a fatal R8 exit) | `orchestrator/internal/implementwithreviewer/runner_test.go` and `sidechannel_test.go` asserting event schema, all listed statuses, and written file name via fake sink |
| R10 | Cleanup order must stop the file-channel manager first, then close agent sessions, then remove the FIFO paths on both success and failure so delivery cannot race against session teardown. | `RunnerSideChannelLifecycle` | `FileChannelManager` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/runner_test.go`, `orchestrator/internal/filechannel/fifo.go` | `Manager.Stop() error`, `Manager.Remove() error`, `runner.finish(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` for stop-close-remove ordering |
| R11 | Update the checked-in command contract to document the fixed FIFO paths, the startup prompt capability, the new `channel-events.jsonl` artifact, and the non-goal that side-channel traffic does not alter loop control. | `ContractDocumentation` | `RunnerSideChannelLifecycle`, `ChannelArtifactLog` | `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `implement-with-reviewer` product contract sections | Manual spec review plus diff review in implementation task; final regression is doc text matching runtime behavior |
| E1 | If a payload contains `</side_channel_message>`, deliver it literally inside the wrapper with no escaping or validation. | `SideChannelDeliveryCoordinator` | `SessionSideChannelReceiver` | `orchestrator/internal/implementwithreviewer/sidechannel.go`, `orchestrator/internal/implementwithreviewer/sidechannel_test.go` | `wrapSideChannelMessage(body string)` | `orchestrator/internal/implementwithreviewer/sidechannel_test.go` for literal wrapping |
| E2 | If a side-channel message lands while the destination role is already in the middle of `RunTurn`, inject it immediately; it may appear in the in-flight pane capture and that is acceptable. | `SessionSideChannelReceiver` | `SideChannelDeliveryCoordinator` | `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go`, `orchestrator/internal/implementwithreviewer/sidechannel.go` | `Session.InjectSideChannel(...)`, `persistentSession.runPrompt(...)` | `orchestrator/internal/cli/command_test.go` with concurrent `RunTurn`/`InjectSideChannel` behavior |
| E3 | Concurrent outbound writes from `RunTurn` and `InjectSideChannel` must not race each other or corrupt the prompt text sent to tmux. | `SessionSideChannelReceiver` | `SideChannelDeliveryCoordinator` | `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go` | `persistentSession.sendMu`, `sendPaneText(text string)` | `orchestrator/internal/cli/command_test.go` proving write serialization and unchanged completion-instruction behavior |
| E4 | The existing implementer/reviewer turn loop, approval detection, and CLI argument handling must remain unchanged; the side channel only adds prompt text into live sessions. | `RunnerSideChannelLifecycle` | `ContractDocumentation` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/runner_test.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `executeTurn(...)`, `isApproved(review string)`, unchanged `main.go` CLI surface | `orchestrator/internal/implementwithreviewer/runner_test.go`; final `cd orchestrator && go test ./...` |

## Component Responsibility Map

- `FileChannelManager`: primary owner for OS-level FIFO lifecycle in `orchestrator/internal/filechannel`. It creates/recreates the fixed pipe paths, starts one reader goroutine per path, emits raw message events merged onto one channel, surfaces fatal infrastructure errors, and provides explicit stop/remove hooks for teardown. It does not know about implementer/reviewer roles, startup readiness, XML-like wrappers, or artifact schemas.

- `SessionSideChannelReceiver`: primary owner for asynchronous prompt injection into a live backend session. It extends the `cli.Session` contract with `InjectSideChannel(message string) error` and keeps `persistentSession` responsible for how text reaches tmux panes. It owns send-side synchronization between ordinary prompts and side-channel writes. It does not decide when a message should be delivered, what wrapper to apply, or whether failures are fatal to the run.

- `SideChannelDeliveryCoordinator`: primary owner for feature semantics in `orchestrator/internal/implementwithreviewer/sidechannel.go`. It composes startup prompt instructions, maps FIFO paths to source/destination roles, tracks per-role readiness, drops early/empty messages, wraps deliverable bodies in `<side_channel_message>`, and records the correct `ChannelEvent` status. It does not own FIFO syscalls, session startup/finish ordering, or result-file persistence.

- `ChannelArtifactLog`: primary owner for the `channel-events.jsonl` artifact schema and writer plumbing. It defines `ChannelEvent`, status constants (including `reader_error` per R9 when the runner logs infrastructure failure), the new `ArtifactSink.AppendChannelEvent(...)` hook, and the file path under `log/runs/<run-id>/`. It does not decide when to emit events; it only persists the records requested by orchestration code.

- `RunnerSideChannelLifecycle`: primary owner for integrating the side channel into the existing run lifecycle. It decides startup order (manager before sessions, readiness marks after `Start` returns), owns the goroutine that consumes raw file-channel messages/errors and forwards them through `SideChannelDeliveryCoordinator`, and enforces teardown order (`Stop` manager, `Close` sessions, `Remove` FIFOs). It does not perform raw tmux I/O or implement FIFO read loops itself.

- `ContractDocumentation`: primary owner for updating `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` so the checked-in product contract reflects the new fixed pipe paths, startup capability, artifact file, and non-control-flow semantics. It does not own runtime behavior.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `RunnerSideChannelLifecycle` | `FileChannelManager` | `NewManager(Config{Paths []string}) (Manager, error)` | Constructor must remove any existing path and recreate a FIFO before sessions start. Returned manager must already have reader goroutines running so startup prompts can safely reference the paths. |
| `FileChannelManager` | `RunnerSideChannelLifecycle` | `Messages() <-chan filechannel.Message`, `Errors() <-chan error` | `Message` carries `Path`, `Body`, and receipt timestamp only. Role mapping, wrapper selection, and delivery status classification happen in `implementwithreviewer`, not in `filechannel`. |
| `RunnerSideChannelLifecycle` | `SideChannelDeliveryCoordinator` | `HandleMessage(msg filechannel.Message)`, `MarkReady(role string)` | `MarkReady` must be called only after `session.Start(...)` returns. `HandleMessage` must be safe to call from the background delivery goroutine while ordinary turns are running. |
| `RunnerSideChannelLifecycle` | `SideChannelDeliveryCoordinator` | `BuildStartupPrompt(role string, basePrompt string) string` | The startup prompt stays feature-owned because it includes run-context data (literal `./to_reviewer.pipe` / `./to_implementer.pipe`). Backend-specific startup acknowledgment instructions in `cli` remain unchanged. |
| `SideChannelDeliveryCoordinator` | `SessionSideChannelReceiver` | `Session.InjectSideChannel(wrapSideChannelMessage(body)) error` | This call must send raw prompt text into the live pane immediately, without appending `Finish your response...`, without waiting for `<promise>done</promise>`, and without changing the runner loop state. |
| `SideChannelDeliveryCoordinator` | `ChannelArtifactLog` | `ArtifactSink.AppendChannelEvent(ChannelEvent)` | Every delivery attempt, drop, or non-fatal per-message outcome becomes exactly one channel event with the chosen status and raw body when available. Empty bodies omit delivery. Fatal infrastructure errors are R8: the runner must fail the run; if the runner appends a final `reader_error` line before exit, that line uses the same `ChannelEvent` schema. |
| `SessionSideChannelReceiver` | `tmux.TmuxPaneLike` | `SendText(text string) error`, concurrent with `Capture()` polling | Outbound sends should be serialized with a `sendMu` so `RunTurn` and `InjectSideChannel` do not interleave the bytes they send. `Capture()` can continue concurrently because immediate mid-turn delivery is part of the design. |
| `RunnerSideChannelLifecycle` | `FileChannelManager` | `Stop() error`, then `Remove() error` | `Stop()` should unblock any blocked FIFO readers and wait for their goroutines to exit. `Remove()` should delete `./to_reviewer.pipe` and `./to_implementer.pipe` after session cleanup so stale paths do not persist after the run. |

## File Ownership Map

- Create `orchestrator/internal/filechannel/fifo.go` - owned by `FileChannelManager`; FIFO creation/removal, raw message event type, reader goroutines, stop/remove lifecycle, and fatal error propagation.
- Create `orchestrator/internal/filechannel/fifo_test.go` - owned by `FileChannelManager`; real FIFO tests in temp directories for EOF-delimited reads, stale-path replacement, and shutdown/remove behavior.
- Modify `orchestrator/internal/cli/interface.go` - owned by `SessionSideChannelReceiver`; extend `cli.Session` with the async side-channel injection method.
- Modify `orchestrator/internal/cli/command.go` - owned by `SessionSideChannelReceiver`; implement `InjectSideChannel`, add outbound-send serialization, and keep `RunTurn` / startup behavior unchanged.
- Modify `orchestrator/internal/cli/command_test.go` - owned by `SessionSideChannelReceiver`; cover side-channel injection transport and concurrent-write invariants without real tmux.
- Create `orchestrator/internal/implementwithreviewer/sidechannel.go` - owned by `SideChannelDeliveryCoordinator`; startup instruction builder, readiness tracking, wrapper helper, path-to-role mapping, and message-to-event classification.
- Create `orchestrator/internal/implementwithreviewer/sidechannel_test.go` - owned by `SideChannelDeliveryCoordinator`; delivery/drop logic tests with fake sessions and fake artifact sinks.
- Modify `orchestrator/internal/implementwithreviewer/types.go` - **ChannelArtifactLog** (primary) owns `ChannelEvent`, status constants, and the `AppendChannelEvent` method on the `ArtifactSink` interface; **RunnerSideChannelLifecycle** (primary) owns the `NewFileChannelManager` constructor seam on `RunConfig` and any runner-only wiring types if needed. Single file, two non-overlapping concerns with distinct primary owners per symbol group.
- Modify `orchestrator/internal/implementwithreviewer/artifact_writer.go` - owned by `ChannelArtifactLog`; append JSONL channel events.
- Modify `orchestrator/internal/implementwithreviewer/artifact_paths.go` - owned by `ChannelArtifactLog`; add the `channel-events.jsonl` path alongside existing artifact files.
- Modify `orchestrator/internal/implementwithreviewer/runner.go` - owned by `RunnerSideChannelLifecycle`; wire manager startup before sessions, launch the background side-channel delivery loop, mark readiness, enforce fatal read-path failures, and stop/close/remove in the correct order.
- Modify `orchestrator/internal/implementwithreviewer/runner_test.go` - owned by `RunnerSideChannelLifecycle`; extend fakes for the new session/artifact/manager seams and cover startup ordering, fatal manager errors, non-fatal delivery failures, and teardown order.
- Modify `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `ContractDocumentation`; document the runtime behavior added by this feature.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/filechannel/fifo.go`
- `orchestrator/internal/filechannel/fifo_test.go`
- `orchestrator/internal/cli/interface.go`
- `orchestrator/internal/cli/command.go`
- `orchestrator/internal/cli/command_test.go`
- `orchestrator/internal/implementwithreviewer/sidechannel.go`
- `orchestrator/internal/implementwithreviewer/sidechannel_test.go`
- `orchestrator/internal/implementwithreviewer/types.go`
- `orchestrator/internal/implementwithreviewer/artifact_writer.go`
- `orchestrator/internal/implementwithreviewer/artifact_paths.go`
- `orchestrator/internal/implementwithreviewer/runner.go`
- `orchestrator/internal/implementwithreviewer/runner_test.go`
- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`

**Incidental-only files:**
- None expected. If implementation appears to require touching any additional file, stop and update this plan first rather than widening scope ad hoc.

## Task List

### Task 1: `FileChannelManager`

**Files:**
- Create: `orchestrator/internal/filechannel/fifo.go`
- Test: `orchestrator/internal/filechannel/fifo_test.go`

**Covers:** `R1`, `R2`
**Owner:** `FileChannelManager`
**Why:** Land the generic FIFO transport first so the orchestration layer can depend on a tested message stream instead of inventing OS-level behavior inside `runner.go`.

- [ ] **Step 1: Write the failing FIFO manager tests**

```go
func TestManagerReadsOneMessagePerOpenWriteCloseCycle(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewManager(Config{
		Paths: []string{
			filepath.Join(dir, "to_reviewer.pipe"),
			filepath.Join(dir, "to_implementer.pipe"),
		},
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Remove()

	writeFIFO(t, filepath.Join(dir, "to_reviewer.pipe"), "first message")
	writeFIFO(t, filepath.Join(dir, "to_reviewer.pipe"), "second message")

	got1 := <-manager.Messages()
	got2 := <-manager.Messages()
	if got1.Body != "first message" || got2.Body != "second message" {
		t.Fatalf("unexpected bodies: %#v %#v", got1, got2)
	}
}

func TestManagerStopAndRemoveCleanUpPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_reviewer.pipe")
	manager, err := NewManager(Config{Paths: []string{path}})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := manager.Remove(); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected FIFO removal, got err=%v", err)
	}
}

func TestManagerRecreatesStaleNonFIFOPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_reviewer.pipe")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	manager, err := NewManager(Config{Paths: []string{path}})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Remove()
	var st os.FileInfo
	st, err = os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if st.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("expected FIFO after replace, got mode %v", st.Mode())
	}
}
```

- [ ] **Step 2: Run the targeted FIFO tests to verify they fail**

Run: `cd orchestrator && go test ./internal/filechannel -run 'TestManagerReadsOneMessagePerOpenWriteCloseCycle|TestManagerStopAndRemoveCleanUpPaths|TestManagerRecreatesStaleNonFIFOPath' -v`
Expected: FAIL because the `internal/filechannel` package does not exist yet.

- [ ] **Step 3: Implement the minimal FIFO manager**

```go
type Message struct {
	Path       string
	Body       string
	ReceivedAt time.Time
}

type Manager interface {
	Messages() <-chan Message
	Errors() <-chan error
	Stop() error
	Remove() error
}
```

Implement `Config`, FIFO recreation with `syscall.Mkfifo`, one read loop per path using `io.ReadAll` until EOF, a merged `messages` channel, a merged `errors` channel, and a `Stop()` implementation that unblocks blocked readers before waiting for them to exit. Keep `Remove()` separate from `Stop()` so the runner can enforce the chosen teardown order.

- [ ] **Step 4: Run the file-channel package tests to verify they pass**

Run: `cd orchestrator && go test ./internal/filechannel -v`
Expected: PASS, including real FIFO tests for EOF framing and cleanup.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/filechannel/fifo.go orchestrator/internal/filechannel/fifo_test.go
git commit -m "feat: add FIFO file-channel manager"
```

### Task 2: `SessionSideChannelReceiver`

**Files:**
- Modify: `orchestrator/internal/cli/interface.go`
- Modify: `orchestrator/internal/cli/command.go`
- Modify: `orchestrator/internal/cli/command_test.go`

**Covers:** `R5`, `E2`, `E3`
**Owner:** `SessionSideChannelReceiver`
**Why:** The runner needs a narrow asynchronous delivery hook that can inject wrapped prompt text into a live tmux session without leaking tmux types into `implementwithreviewer`.

- [ ] **Step 1: Add failing session tests for async injection**

```go
// seqTestPane: like testPane but records every SendText in calls (E3) and keeps last for Capture parity with testPane.sent.
// Add in command_test.go next to testPane; import "sync" and "strings".
type seqTestPane struct {
	mu        sync.Mutex
	calls     []string
	sent      string
	ready     string
	readyUsed bool
	phase     int
}

func (p *seqTestPane) SendText(text string) error {
	p.mu.Lock()
	p.calls = append(p.calls, text)
	p.sent = text
	if !p.readyUsed {
		p.readyUsed = true
	}
	p.mu.Unlock()
	return nil
}

func (p *seqTestPane) Capture() (string, error) {
	if !p.readyUsed {
		if p.ready != "" {
			return p.ready, nil
		}
		return "> ", nil
	}
	p.phase++
	if p.phase == 1 {
		return "> " + p.sent + "\n", nil
	}
	return "> " + p.sent + "\nUse Token = \"v2\"\n<promise>done</promise>\n", nil
}

func (p *seqTestPane) Target() string { return "%0" }

func TestRunTurnAndInjectSideChannelSerializeOutboundWrites(t *testing.T) {
	pane := &seqTestPane{ready: "> "}
	s := &persistentSession{
		tmuxSession: &fakeTmuxSessionForTest{name: "iwr-concurrency"},
		pane:        pane,
		backendName: "codex",
		idleTimeout: 2 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := s.RunTurn("turn prompt body\n")
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := s.InjectSideChannel("<side_channel_message>x</side_channel_message>\n"); err != nil {
		t.Fatalf("InjectSideChannel: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	pane.mu.Lock()
	calls := append([]string(nil), pane.calls...)
	pane.mu.Unlock()
	if len(calls) < 2 {
		t.Fatalf("want at least two SendText calls (turn + side channel), got %d: %v", len(calls), calls)
	}
	joined := strings.Join(calls, "")
	iTurn := strings.Index(joined, "turn prompt")
	iSide := strings.Index(joined, "<side_channel_message>")
	if iTurn < 0 || iSide < 0 || iTurn >= iSide {
		t.Fatalf("want turn text before side-channel text in non-interleaved SendText stream: %q", joined)
	}
}

func TestInjectSideChannelSendsRawPromptWithoutCompletionInstruction(t *testing.T) {
	pane := &testPane{ready: "> "}
	session := &persistentSession{
		tmuxSession: &fakeTmuxSessionForTest{name: "iwr-test-reviewer"},
		pane:        pane,
		backendName: "codex",
		idleTimeout: time.Second,
	}

	err := session.InjectSideChannel("<side_channel_message>\nhello\n</side_channel_message>\n")
	if err != nil {
		t.Fatalf("InjectSideChannel returned error: %v", err)
	}
	if strings.Contains(pane.sent, completionInstruction) {
		t.Fatalf("side-channel injection must not append completion instruction: %q", pane.sent)
	}
}
```

Success criteria: `seqTestPane.calls` records each `SendText` as a full string; `strings.Join(calls, "")` order shows the decorated turn body before `<side_channel_message>` (E3). If `RunTurn` performs multiple `SendText` calls, keep the same ordering assertion style (first index of turn text before first index of side-channel marker in the joined stream).

- [ ] **Step 2: Run the targeted `cli` tests to verify they fail**

Run: `cd orchestrator && go test ./internal/cli -run 'TestInjectSideChannelSendsRawPromptWithoutCompletionInstruction|TestRunTurnAndInjectSideChannelSerializeOutboundWrites' -v`
Expected: FAIL because `cli.Session` and `persistentSession` do not expose side-channel injection yet.

- [ ] **Step 3: Implement the async injection seam**

```go
type Session interface {
	Start(rolePrompt string) error
	RunTurn(prompt string) (TurnResult, error)
	InjectSideChannel(message string) error
	SessionName() string
	Close() error
}
```

In `command.go`, add a `sendMu sync.Mutex`, factor normal prompt sending into a helper like `sendPaneText(text string) error`, make `RunTurn(...)` use that helper, and implement `InjectSideChannel(...)` as a raw `sendPaneText` call with no prompt decoration and no done-marker waiting.

- [ ] **Step 4: Run the `cli` package tests to verify they pass**

Run: `cd orchestrator && go test ./internal/cli -v`
Expected: PASS, including existing session-core regressions plus the new side-channel injection coverage.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/cli/interface.go orchestrator/internal/cli/command.go orchestrator/internal/cli/command_test.go
git commit -m "feat: add async side-channel session injection"
```

### Task 3: `SideChannelDeliveryCoordinator` + `ChannelArtifactLog` + `RunnerSideChannelLifecycle` (single landing)

`runner.go`, `types.go`, `artifact_writer.go`, `artifact_paths.go`, and the new `sidechannel.go` all depend on each other. Do not land only part of this task: the runner needs the new artifact API, the new file-channel manager seam, the delivery coordinator, and updated test doubles in one buildable commit.

**Files:**
- Create: `orchestrator/internal/implementwithreviewer/sidechannel.go`
- Create: `orchestrator/internal/implementwithreviewer/sidechannel_test.go`
- Modify: `orchestrator/internal/implementwithreviewer/types.go`
- Modify: `orchestrator/internal/implementwithreviewer/artifact_writer.go`
- Modify: `orchestrator/internal/implementwithreviewer/artifact_paths.go`
- Modify: `orchestrator/internal/implementwithreviewer/runner.go`
- Modify: `orchestrator/internal/implementwithreviewer/runner_test.go`

**Covers:** `R3`, `R4`, `R6`, `R7`, `R8`, `R9`, `R10`, `E1`, `E4`
**Owner:** `RunnerSideChannelLifecycle` leads the landing; `SideChannelDeliveryCoordinator` owns message semantics; `ChannelArtifactLog` owns persistence additions.
**Why:** This task is where the feature becomes real: startup prompts gain pipe instructions, raw FIFO bodies become classified side-channel events, the runner observes fatal infrastructure failures, and teardown order is enforced.

- [ ] **Step 1: Add failing coordinator and runner tests**

```go
func TestSideChannelCoordinatorDropsNotStartedAndLogsEvent(t *testing.T) {
	sink := &fakeArtifactWriter{}
	coordinator := newSideChannelCoordinator(sink, map[string]cli.Session{
		RoleImplementer: &fakeSession{role: RoleImplementer},
		RoleReviewer:    &fakeSession{role: RoleReviewer},
	})

	coordinator.MarkReady(RoleImplementer)
	coordinator.HandleMessage(filechannel.Message{
		Path: "./to_reviewer.pipe",
		Body: "hello reviewer",
	})

	if len(sink.channelEvents) != 1 || sink.channelEvents[0].Status != ChannelStatusDroppedNotStarted {
		t.Fatalf("unexpected events: %+v", sink.channelEvents)
	}
}

func TestRunnerStopsManagerBeforeClosingSessions(t *testing.T) {
	var order []string
	mgr := &fakeFileChannelManager{
		stop: func() error {
			order = append(order, "manager.Stop")
			return nil
		},
		remove: func() error {
			order = append(order, "manager.Remove")
			return nil
		},
	}
	imp := &fakeSession{name: "impl", close: func() error { order = append(order, "CloseImplementer"); return nil }}
	rev := &fakeSession{name: "rev", close: func() error { order = append(order, "CloseReviewer"); return nil }}
	runSideChannelHarnessToCompletion(t, runHarnessOpts{
		manager:    mgr,
		implementer: imp,
		reviewer:   rev,
	})
	wantPrefix := []string{"manager.Stop", "CloseImplementer", "CloseReviewer", "manager.Remove"}
	if len(order) < len(wantPrefix) {
		t.Fatalf("teardown order too short: got %v", order)
	}
	if !slices.Equal(order[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("teardown order: got %v want prefix %v", order, wantPrefix)
	}
}
```

Add `import "slices"` (stdlib, Go 1.21+). Fakes: `fakeFileChannelManager` implements the `Stop`/`Remove` hooks the runner calls in `finish` (or the `filechannel.Manager` interface you inject via `RunConfig`). `runSideChannelHarnessToCompletion` is a test helper that exercises the same shutdown path as a normal run exit without real FIFOs, using `t.TempDir` and injected `NewFileChannelManager`. Align `wantPrefix` session close order with `runner.go` (`CloseImplementer` / `CloseReviewer` order may swap), but `manager.Stop` must be first and `manager.Remove` last (R10).

- [ ] **Step 2: Run the targeted runner/coordinator tests to verify they fail**

Run: `cd orchestrator && go test ./internal/implementwithreviewer -run 'TestSideChannelCoordinatorDropsNotStartedAndLogsEvent|TestRunnerStopsManagerBeforeClosingSessions' -v`
Expected: FAIL because the coordinator, artifact schema, and manager seam do not exist yet.

- [ ] **Step 3: Implement the side-channel orchestration landing**

```go
type ChannelEvent struct {
	At              time.Time `json:"at"`
	SourceRole      string    `json:"source_role,omitempty"`
	DestinationRole string    `json:"destination_role,omitempty"`
	ChannelPath     string    `json:"channel_path"`
	Status          string    `json:"status"`
	RawBody         string    `json:"raw_body,omitempty"`
}
```

Implement all of the following together:

- `sidechannel.go`: fixed path constants, startup-instruction builder, readiness map, path-to-role mapping, wrapper helper, and delivery/drop classification.
- `types.go`: extend `ArtifactSink` with `AppendChannelEvent(ChannelEvent) error`, add `ChannelEvent` and status constant strings, and add a `NewFileChannelManager` seam in `RunConfig` (or equivalent constructor hook for tests).
- `artifact_paths.go` / `artifact_writer.go`: `channel-events.jsonl` path plus JSONL append method.
- `runner.go`: start the file-channel manager before sessions, launch a background goroutine that consumes `manager.Messages()` and `manager.Errors()`, mark readiness immediately after each `session.Start(...)` succeeds, surface manager errors as terminal, and enforce `Stop()` -> `Close()` -> `Remove()` in teardown.
- `runner_test.go`: extend fakes for the new `cli.Session` method, new artifact sink method, and fake file-channel manager.

- [ ] **Step 4: Run the full `implementwithreviewer` package tests to verify they pass**

Run: `cd orchestrator && go test ./internal/implementwithreviewer -v`
Expected: PASS, including startup-order, status logging, fatal reader-error, and teardown-order coverage.

- [ ] **Step 5: Run the full orchestrator test suite**

Run: `cd orchestrator && go test ./...`
Expected: PASS. Existing CLI, tmux, and runner behavior should remain unchanged outside the new side-channel capability.

- [ ] **Step 6: Commit**

```bash
git add orchestrator/internal/implementwithreviewer/sidechannel.go orchestrator/internal/implementwithreviewer/sidechannel_test.go orchestrator/internal/implementwithreviewer/types.go orchestrator/internal/implementwithreviewer/artifact_writer.go orchestrator/internal/implementwithreviewer/artifact_paths.go orchestrator/internal/implementwithreviewer/runner.go orchestrator/internal/implementwithreviewer/runner_test.go
git commit -m "feat: wire file-channel side messages into reviewer runs"
```

### Task 4: `ContractDocumentation`

**Files:**
- Modify: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`

**Covers:** `R11`, `E4`
**Owner:** `ContractDocumentation`
**Why:** The checked-in command contract is the reader’s zero-context source of truth; it must match the implemented feature instead of silently drifting behind the runtime behavior.

- [ ] **Step 1: Update the contract document with the new side-channel behavior**

```md
### Side-channel capability

Each role receives startup instructions for:

- `./to_reviewer.pipe`
- `./to_implementer.pipe`

One FIFO write open-write-close cycle equals one side-channel message.
```

Also update the runtime artifacts section to mention `channel-events.jsonl`, the failure semantics section to distinguish fatal infrastructure failure from non-fatal delivery failure, and the runtime model section to state that side-channel text does not change loop control.

- [ ] **Step 2: Review the doc diff against the design and implementation**

Run: `cd orchestrator && git diff -- cmd/implement-with-reviewer/CONTRACT.md` (from repo root: `git diff -- orchestrator/cmd/implement-with-reviewer/CONTRACT.md`)
Expected: The diff should describe fixed FIFO paths, startup instructions, `channel-events.jsonl`, drop statuses, and unchanged CLI surface only.

- [ ] **Step 3: Re-run the full orchestrator test suite after the doc update**

Run: `cd orchestrator && go test ./...`
Expected: PASS. This confirms the final tree still builds and tests after the last scoped edit.

- [ ] **Step 4: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer/CONTRACT.md
git commit -m "docs: record implementer reviewer file-channel contract"
```
