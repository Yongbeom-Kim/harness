# Tmux-Backed Session Orchestration Implementation Plan

**Goal:** Replace one-shot backend subprocess invocations in `implement-with-reviewer` with tmux-backed persistent backend sessions while preserving the command-line contract and writing runtime artifacts.

**Architecture:** Extract orchestration into `orchestrator/internal/implementwithreviewer`, replace `internal/cli.CliTool` with a high-level `internal/cli.Session` API, and have backend-specific constructors such as `codex.NewSession(...)` and `claude.NewSession(...)` own tmux lifecycle entirely behind that API. Per the recorded implementation decisions, v1 intentionally diverges from the reviewed design in two places: each backend session creates and owns its own tmux session rather than sharing one run-level two-pane session, and tmux runtime verification is manual rather than a new checked-in integration suite.

**Tech Stack:** Go 1.26, `tmux`, bash-sourced CLI launchers via `.agentrc`, `github.com/google/uuid` v1.6.0 for UUIDv7 run IDs.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Preserve the existing CLI surface: `--implementer`, `--reviewer`, optional `--max-iterations`, `MAX_ITERATIONS` precedence, stdin validation, supported backends, existing exit codes, and no new tmux-specific flags. | `CliEntrypoint` | `RunCoordinator`, `SessionFactory` | `orchestrator/cmd/implement-with-reviewer/main.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go` | `run(args, runnerConfig)`, `resolveMaxIterations(...)`, `readTask(...)` | `orchestrator/cmd/implement-with-reviewer/main_test.go`; manual smoke for `-h`, missing flags, invalid backend |
| R2 | Replace `CliTool.SendMessage` one-shot subprocess calls with a persistent session API selected by backend name, and remove fallback to the old `exec` / `resume` / `-p` model. | `SessionFactory` | `CodexSession`, `ClaudeSession`, `RunCoordinator` | `orchestrator/internal/cli/interface.go`, `orchestrator/internal/cli/factory.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go` | `NewSession(name, SessionOptions)`, `Session.Start(...)`, `Session.RunTurn(...)`, `Session.Close()` | `orchestrator/internal/implementwithreviewer/runner_test.go`; `cd orchestrator && go test ./...` |
| R3 | Start both implementer and reviewer sessions before the first real implementation turn; startup acknowledgements must complete successfully but stay out of the normal user-visible transcript. | `RunCoordinator` | `CodexSession`, `ClaudeSession`, `ArtifactWriter` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go` | `startSessions(...)`, `Session.Start(rolePrompt)` | `orchestrator/internal/implementwithreviewer/runner_test.go`; manual success run |
| R4 | Keep implementer/reviewer/rewrite prompt templates in the feature package, while backend sessions append the exact v1 completion instruction `Finish your response with exactly <promise>done</promise>.` and backend-specific startup prompts internally. | `PromptBuilder` | `RunCoordinator`, `CodexSession`, `ClaudeSession` | `orchestrator/internal/implementwithreviewer/prompts.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go` | `BuildInitialImplementerPrompt(...)`, `BuildReviewerPrompt(...)`, `BuildRewritePrompt(...)`, adapter-local `decorateTurnPrompt(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go`; manual capture inspection |
| R5 | Preserve reviewer approval by substring match on `<promise>APPROVED</promise>` and continue the rewrite loop until approval or `maxIterations`. | `RunCoordinator` | `PromptBuilder` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/prompts.go` | `isApproved(review string)`, `runReviewLoop(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` |
| R6 | Preserve the stdout/stderr transcript contract: print run headers and `--- iter N - ROLE (backend) ---` banners, emit completed turn text on stdout only, send runtime failures to stderr, and do not add run-ID or tmux-session headers. | `RunCoordinator` | `CliEntrypoint` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/cmd/implement-with-reviewer/main.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `printBanner(...)`, `writeTurnOutput(...)`, `writeRuntimeError(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`; manual smoke |
| R7 | Keep `<promise>done</promise>` in raw captures, reviewer inputs, and the final printed implementation; do not strip it in v1. | `RunCoordinator` | `CodexSession`, `ClaudeSession` | `orchestrator/internal/implementwithreviewer/prompts.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | runner-side `doneMarker` literal, adapter completion checks | `orchestrator/internal/implementwithreviewer/runner_test.go`; manual capture and final-output inspection |
| R8 | Treat clarification requests, blocked text, or uncertainty as ordinary turn output with no special orchestration branch. | `RunCoordinator` | `PromptBuilder` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | no blocked-state branch in `runReviewLoop(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` |
| R9 | Keep the runner completely tmux-agnostic: it only knows about `cli.Session` objects and backend names, while backend-specific constructors create and own per-role tmux sessions behind the session API. This is an intentional implementation-plan deviation from the reviewed design. | `SessionFactory` | `RunCoordinator`, `CodexSession`, `ClaudeSession` | `orchestrator/internal/cli/factory.go`, `orchestrator/internal/cli/interface.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go` | `SessionOptions{RunID, Role, IdleTimeout}`, `NewSession(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go`; manual tmux inspection |
| R10 | Implement Codex as a tmux-backed persistent session that creates its own role-specific tmux session, launches `codex`, sends startup/turn prompts, waits for the done marker with a 120-second idle timeout, captures raw pane text, resets the pane, and closes cleanly. | `CodexSession` | `RunCoordinator` | `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/command.go` | `NewCodexSession(SessionOptions)`, `Start(...)`, `RunTurn(...)`, `Close()`, `SessionError{Kind,Capture,SessionName}` | Manual verification task; `cd orchestrator && go test ./...` compile/unit coverage only |
| R11 | Implement Claude as a tmux-backed persistent session with the same contract as Codex: its own role-specific tmux session, startup/turn prompts, 120-second idle timeout, raw capture/reset behavior, and close semantics. | `ClaudeSession` | `RunCoordinator` | `orchestrator/internal/cli/claude.go`, `orchestrator/internal/cli/command.go` | `NewClaudeSession(SessionOptions)`, `Start(...)`, `RunTurn(...)`, `Close()`, `SessionError{Kind,Capture,SessionName}` | Manual verification task; `cd orchestrator && go test ./...` compile/unit coverage only |
| R12 | Persist runtime artifacts under `log/runs/<uuidv7>/` after each completed turn and again on timeout/finalization, including `metadata.json`, `state-transitions.jsonl`, `captures/*`, and `result.json`. | `ArtifactWriter` | `RunCoordinator` | `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/internal/implementwithreviewer/artifact_paths.go`, `orchestrator/internal/implementwithreviewer/runner.go` | `NewArtifactWriter(...)`, `WriteMetadata(...)`, `AppendTransition(...)`, `WriteCapture(...)`, `WriteResult(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` with fake writer; manual artifact inspection |
| R13 | Generate UUIDv7 run IDs via `github.com/google/uuid`, use them in artifact paths, and record per-role tmux session names in metadata. | `RunCoordinator` | `ArtifactWriter`, `SessionFactory` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/artifact_paths.go`, `orchestrator/go.mod`, `orchestrator/go.sum` | `newRunID()`, `Session.SessionName()`, `metadata.sessions[role].tmux_session_name` | `orchestrator/internal/implementwithreviewer/runner_test.go` with stubbed run ID; manual metadata inspection |
| E1 | If `tmux` is unavailable or backend launch/startup fails, fail fast with exit code `1`, surface the error on stderr, preserve whatever artifacts were already written, and do not fall back to the previous one-shot path. | `RunCoordinator` | `CodexSession`, `ClaudeSession`, `ArtifactWriter` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | constructor / `Start(...)` errors, `SessionError.Kind()`, `SessionError.Capture()` | `orchestrator/internal/implementwithreviewer/runner_test.go` startup-failure case; manual launch-failure check |
| E2 | If a turn never reaches `<promise>done</promise>` before 120 seconds of idle time, persist a timeout capture best-effort, write a failure result, exit with code `1`, and close both sessions. | `RunCoordinator` | `ArtifactWriter`, `CodexSession`, `ClaudeSession` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `SessionError.Kind()=="timeout"`, `SessionError.Capture()`, `WriteCapture("...-timeout.txt", ...)` | Manual timeout check |
| E3 | If artifact persistence fails after the agent logic otherwise succeeded, the command still fails and surfaces the persistence error on stderr. | `ArtifactWriter` | `RunCoordinator` | `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | synchronous writer errors from `WriteMetadata`, `AppendTransition`, `WriteCapture`, `WriteResult` | `orchestrator/internal/implementwithreviewer/runner_test.go` writer-failure case |
| E4 | If approval never arrives by `maxIterations`, preserve the existing non-convergence behavior, write runtime artifacts, and clean up both sessions. | `RunCoordinator` | `ArtifactWriter` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/implementwithreviewer/artifact_writer.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `runReviewLoop(...)`, `AppendTransition("non_converged", ...)`, `WriteResult(...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` |
| E5 | On both success and failure, ask both sessions to close, let each one kill its own tmux session, and surface a cleanup error if cleanup itself becomes the terminal failure. This is an intentional implementation-plan deviation from the reviewed design. | `RunCoordinator` | `CodexSession`, `ClaudeSession`, `ArtifactWriter` | `orchestrator/internal/implementwithreviewer/runner.go`, `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go`, `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `closeSessions(...)`, `Session.Close()`, `AppendTransition("closed", ...)` | `orchestrator/internal/implementwithreviewer/runner_test.go` cleanup-failure case; manual `tmux ls` cleanup check |
| E6 | Update the checked-in command contract to match the tmux-backed runtime model, per-role tmux sessions, artifact files, preserved markers, and failure semantics, and document the two approved implementation deviations from the original reviewed design. | `ContractDoc` | `CliEntrypoint`, `RunCoordinator` | `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | contract doc sections only | Manual doc review against runtime behavior |

## Component Responsibility Map

- `CliEntrypoint`: owns CLI parsing, env precedence, stdin reading, usage-vs-runtime exit code classification, and wiring the thin entrypoint into the new runner. Collaborates with `SessionFactory` only through backend name validation and with `RunCoordinator` through a single `Run` call. Does not own tmux behavior, runtime artifacts, or prompt composition.
- `RunCoordinator`: owns the implementer/reviewer loop, startup ordering, approval detection, transcript banners, final success/non-convergence output, timeout and cleanup classification, and deciding when artifacts are written. Collaborates with `PromptBuilder`, `SessionFactory`, and `ArtifactWriter`. Does not own tmux command details or backend launch strings.
- `PromptBuilder`: owns the implementer/reviewer/rewrite prompt shapes and the runner-side marker literals. Collaborates with backend sessions only through plain strings. Does not own startup prompts, tmux paste semantics, or completion polling.
- `ArtifactWriter`: owns run directory creation, file naming, `metadata.json`, `state-transitions.jsonl`, `captures/*`, and `result.json`. Collaborates with `RunCoordinator` through explicit write calls. Does not decide loop control, approval, or timeout policy.
- `SessionFactory`: owns mapping backend names to concrete session constructors and keeping the runner tmux-agnostic. Collaborates with `CodexSession` and `ClaudeSession` through the `Session` interface. Does not own loop control or artifact formats.
- `CodexSession`: owns Codex-specific tmux session creation, backend launch command, startup prompt text, completion suffix decoration, idle-time polling, raw capture/reset, and close semantics. Collaborates with `RunCoordinator` only through the `Session` contract. Does not own the review loop or filesystem artifacts.
- `ClaudeSession`: owns Claude-specific tmux session creation, backend launch command, startup prompt text, completion suffix decoration, idle-time polling, raw capture/reset, and close semantics. Collaborates with `RunCoordinator` only through the `Session` contract. Does not own the review loop or filesystem artifacts.
- `ContractDoc`: owns the checked-in runtime contract in `CONTRACT.md`. Collaborates with `CliEntrypoint` and `RunCoordinator` to keep the document aligned with the shipped behavior and the recorded implementation deviations. Does not own executable logic.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CliEntrypoint` | `RunCoordinator` | `Run(ctx context.Context, cfg RunConfig) error` | `RunConfig` should include task text, backend names, max iterations, fixed `IdleTimeout`, stdout/stderr writers, and injectable `NewSession`, `NewArtifactWriter`, and `NewRunID` hooks for tests. |
| `RunCoordinator` | `SessionFactory` | `NewSession(name string, opts SessionOptions) (Session, error)` | `SessionOptions` must include `RunID`, `Role`, and `IdleTimeout`. Constructors are allowed to create their own tmux session immediately; the runner must remain unaware of tmux commands and tmux topology. |
| `RunCoordinator` | `PromptBuilder` | `BuildInitialImplementerPrompt(task string) string`, `BuildReviewerPrompt(task, implementation string) string`, `BuildRewritePrompt(task, implementation, review string) string` | These functions return role content only. They must not add tmux transport instructions or startup prompts. |
| `RunCoordinator` | `Session` | `Start(rolePrompt string) error`, `RunTurn(prompt string) (TurnResult, error)`, `SessionName() string`, `Close() error` | `TurnResult` should include `Output` and `RawCapture`. `Start` receives the role-specific startup rules only; the backend session appends its own transport-level startup instructions, completes startup acknowledgement, and leaves the session ready for the first real turn without printing startup text to parent stdout. `SessionName()` gives the runner stable metadata before the first real turn executes. |
| `Session` | `RunCoordinator` | `SessionError` failure contract | Session errors should expose `Kind()` (`launch`, `startup`, `timeout`, `capture`, `close`), `Capture()` for best-effort pane text, and `SessionName()` so the runner can persist failure artifacts and classify stderr output. |
| `CodexSession` / `ClaudeSession` | external `tmux` + backend CLI | backend-owned tmux lifecycle | Each constructor owns one role-specific tmux session name derived from `RunID` and `Role`, launches the backend CLI inside that tmux session, appends the exact v1 suffix line `Finish your response with exactly <promise>done</promise>.`, and uses raw `tmux capture-pane` text as the source of truth. |
| `RunCoordinator` | `ArtifactSink` | `WriteMetadata(RunMetadata)`, `AppendTransition(StateTransition)`, `WriteCapture(name, text string)`, `WriteResult(RunResult)` | The runner must call `WriteMetadata` after both sessions have started successfully and their `SessionName()` values are available, but before the first real turn. It must call `AppendTransition` for every lifecycle edge, `WriteCapture` immediately after each successful turn and on timeout/startup failure when capture text exists, and `WriteResult` exactly once at terminal state. |
| `ArtifactWriter` | filesystem | `log/runs/<run-id>/...` layout | Directory creation must happen before the first artifact write. Capture filenames must stay iteration-addressable: `iter-0-implementer.txt`, `iter-1-reviewer.txt`, `iter-1-implementer.txt`, timeout or startup failure files with explicit suffixes. Metadata should record two session names, one for each role. |
| `RunCoordinator` | stdout / stderr | transcript contract | Successful turn text always goes to stdout under the existing banners. Runtime errors, persistence failures, and cleanup failures go to stderr. No attempt should be made to reconstruct backend-native stdout/stderr inside captured turns. |

## File Ownership Map

- Create `orchestrator/internal/implementwithreviewer/types.go` - owned by `RunCoordinator`; defines `RunConfig`, `ArtifactSink`, terminal result/error types, transition payloads, and small shared structs consumed by the runner and tests.
- Create `orchestrator/internal/implementwithreviewer/runner.go` - owned by `RunCoordinator`; starts both sessions, drives the review loop, writes transcript output, classifies failure modes, and coordinates cleanup plus artifact calls.
- Create `orchestrator/internal/implementwithreviewer/prompts.go` - owned by `PromptBuilder`; stores the implementer/reviewer/rewrite prompt builders and runner-side marker literals.
- Create `orchestrator/internal/implementwithreviewer/artifact_writer.go` - owned by `ArtifactWriter`; writes `metadata.json`, appends `state-transitions.jsonl`, writes `result.json`, and persists captures.
- Create `orchestrator/internal/implementwithreviewer/artifact_paths.go` - owned by `ArtifactWriter`; creates `log/runs/<run-id>/`, derives stable capture filenames, and centralizes path helpers used by the writer and manual verification.
- Create `orchestrator/internal/implementwithreviewer/runner_test.go` - owned by `RunCoordinator`; lightweight fake-session coverage for startup ordering, startup failure handling, approval, non-convergence, blocked text handling, cleanup failure handling, and artifact-writer failure handling.
- Modify `orchestrator/internal/cli/interface.go` - owned by `SessionFactory`; replaces `CliTool` with `Session`, `SessionOptions`, `TurnResult`, and `SessionError`.
- Create `orchestrator/internal/cli/factory.go` - owned by `SessionFactory`; dispatches backend names to `NewCodexSession` / `NewClaudeSession` and keeps unknown-backend validation centralized.
- Modify `orchestrator/internal/cli/codex.go` - owned by `CodexSession`; removes one-shot session-ID logic and implements tmux-backed Codex lifecycle with a backend-owned tmux session.
- Modify `orchestrator/internal/cli/claude.go` - owned by `ClaudeSession`; removes one-shot prompt/session-ID logic and implements tmux-backed Claude lifecycle with a backend-owned tmux session.
- Delete `orchestrator/internal/cli/codex_test.go` - owned by `CodexSession`; removes obsolete tests that assert the removed `exec` / `resume` session-ID path.
- Delete `orchestrator/internal/cli/claude_test.go` - owned by `ClaudeSession`; removes obsolete tests that assert the removed `-p` / `--resume` path.
- Modify `orchestrator/internal/cli/command.go` - owned by `SessionFactory`; keep only shared command-execution helpers that are still useful after the tmux refactor.
- Modify `orchestrator/cmd/implement-with-reviewer/main.go` - owned by `CliEntrypoint`; reduces the command to flag parsing, stdin reading, and a single call into `RunCoordinator`.
- Modify `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `CliEntrypoint`; retains validation and read-task coverage after orchestration moves out of `main.go`.
- Modify `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `ContractDoc`; documents per-role tmux sessions, artifact files, marker retention, failure semantics, and the approved deviations from the original reviewed design.
- Modify `orchestrator/go.mod` - owned by `RunCoordinator`; adds `github.com/google/uuid` for UUIDv7 run IDs.
- Modify `orchestrator/go.sum` - owned by `RunCoordinator`; checksum update for the UUID dependency only.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/cmd/implement-with-reviewer/main.go`
- `orchestrator/cmd/implement-with-reviewer/main_test.go`
- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- `orchestrator/internal/implementwithreviewer/types.go`
- `orchestrator/internal/implementwithreviewer/runner.go`
- `orchestrator/internal/implementwithreviewer/prompts.go`
- `orchestrator/internal/implementwithreviewer/artifact_writer.go`
- `orchestrator/internal/implementwithreviewer/artifact_paths.go`
- `orchestrator/internal/implementwithreviewer/runner_test.go`
- `orchestrator/internal/cli/interface.go`
- `orchestrator/internal/cli/factory.go`
- `orchestrator/internal/cli/codex.go`
- `orchestrator/internal/cli/claude.go`
- `orchestrator/internal/cli/codex_test.go` - delete only.
- `orchestrator/internal/cli/claude_test.go` - delete only.
- `orchestrator/go.mod`

**Incidental-only files:**
- `orchestrator/internal/cli/command.go` - shared process/tmux command helpers only.
- `orchestrator/go.sum` - dependency checksum update only.

## Task List

### Task 1: RunCoordinator

**Files:**
- Create: `orchestrator/internal/implementwithreviewer/types.go`
- Create: `orchestrator/internal/implementwithreviewer/runner.go`
- Create: `orchestrator/internal/implementwithreviewer/prompts.go`
- Create: `orchestrator/internal/implementwithreviewer/runner_test.go`
- Modify: `orchestrator/internal/cli/interface.go`
- Modify: `orchestrator/cmd/implement-with-reviewer/main.go`
- Modify: `orchestrator/cmd/implement-with-reviewer/main_test.go`

**Covers:** `R1`, `R3`, `R4`, `R5`, `R6`, `R7`, `R8`, `E1`, `E4`
**Owner:** `RunCoordinator`
**Why:** Establish the new internal runner package, introduce the final session seam early enough for the runner to compile against it, and preserve the external CLI contract before concrete tmux adapter work lands.

- [ ] **Step 1: Write the failing runner tests**

```go
func TestRunStartsBothSessionsBeforeFirstTurn(t *testing.T) {
    // fake sessions record Start/RunTurn order
}

func TestRunStartupFailureDoesNotPrintStartupTranscript(t *testing.T) {
    // fake session Start returns a startup SessionError with capture text
}

func TestRunKeepsDoneMarkerInFinalImplementation(t *testing.T) {
    // fake implementer/reviewer return output that still contains <promise>done</promise>
}
```

- [ ] **Step 2: Run the focused tests to verify they fail**

Run: `cd orchestrator && go test ./internal/implementwithreviewer ./cmd/implement-with-reviewer`
Expected: FAIL because the new package and runner wiring do not exist yet.

- [ ] **Step 3: Write the minimal runner and prompt implementation**

```go
type RunConfig struct {
    Task              string
    Implementer       string
    Reviewer          string
    MaxIterations     int
    IdleTimeout       time.Duration
    Stdout            io.Writer
    Stderr            io.Writer
    NewSession        func(string, cli.SessionOptions) (cli.Session, error)
    NewArtifactWriter func(string) (ArtifactSink, error)
    NewRunID          func() (string, error)
}
```

In the same change, introduce the new `cli.Session` / `cli.SessionError` types in `orchestrator/internal/cli/interface.go` so the runner compiles against the final adapter seam before Task 2 fills in the tmux-backed implementations.

Also move the current loop-behavior coverage out of `main_test.go` into `runner_test.go`; after this task, `main_test.go` should keep only CLI parsing, env precedence, and stdin-read validation coverage.

Wire `main.go` to the runner through the injectable `RunConfig.NewSession` hook rather than concrete backend constructors in this task; Task 2 is the point where production backend dispatch is filled in.

- [ ] **Step 4: Re-run the focused tests**

Run: `cd orchestrator && go test ./internal/implementwithreviewer ./cmd/implement-with-reviewer`
Expected: PASS for the new runner contract and the retained CLI validation tests.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer/main.go \
        orchestrator/cmd/implement-with-reviewer/main_test.go \
        orchestrator/internal/cli/interface.go \
        orchestrator/internal/implementwithreviewer/types.go \
        orchestrator/internal/implementwithreviewer/runner.go \
        orchestrator/internal/implementwithreviewer/prompts.go \
        orchestrator/internal/implementwithreviewer/runner_test.go
git commit -m "refactor: extract implement-with-reviewer runner"
```

### Task 2: SessionFactory

**Files:**
- Modify: `orchestrator/internal/cli/interface.go`
- Create: `orchestrator/internal/cli/factory.go`
- Modify: `orchestrator/internal/cli/codex.go`
- Modify: `orchestrator/internal/cli/claude.go`
- Modify: `orchestrator/internal/cli/command.go`
- Delete: `orchestrator/internal/cli/codex_test.go`
- Delete: `orchestrator/internal/cli/claude_test.go`

**Covers:** `R2`, `R9`, `R10`, `R11`, `E1`, `E2`, `E5`
**Owner:** `SessionFactory`
**Why:** Replace the removed one-shot adapter API with the persistent session contract and make each backend own its own tmux session lifecycle.

- [ ] **Step 1: Run the full test suite before changing the adapter contract**

Run: `cd orchestrator && go test ./...`
Expected: FAIL because the runner now depends on the new session seam, but the concrete backend session implementations and factory dispatch have not replaced the old one-shot adapter behavior yet.

- [ ] **Step 2: Implement the session contract and backend sessions**

```go
type Session interface {
    Start(rolePrompt string) error
    RunTurn(prompt string) (TurnResult, error)
    SessionName() string
    Close() error
}

type SessionOptions struct {
    RunID       string
    Role        string
    IdleTimeout time.Duration
}

type TurnResult struct {
    Output     string
    RawCapture string
}
```

Keep `interface.go` compatible with the Task 1 runner seam while replacing the old `CliTool` implementation details underneath it.

- [ ] **Step 3: Re-run the full test suite**

Run: `cd orchestrator && go test ./...`
Expected: PASS for the retained unit tests. No checked-in tmux integration suite should be added in this task.

- [ ] **Step 4: Commit**

```bash
git add orchestrator/internal/cli/interface.go \
        orchestrator/internal/cli/factory.go \
        orchestrator/internal/cli/codex.go \
        orchestrator/internal/cli/claude.go \
        orchestrator/internal/cli/command.go
git rm orchestrator/internal/cli/codex_test.go orchestrator/internal/cli/claude_test.go
git commit -m "feat: add tmux-backed cli session adapters"
```

### Task 3: ArtifactWriter

**Files:**
- Create: `orchestrator/internal/implementwithreviewer/artifact_writer.go`
- Create: `orchestrator/internal/implementwithreviewer/artifact_paths.go`
- Modify: `orchestrator/internal/implementwithreviewer/runner.go`
- Modify: `orchestrator/internal/implementwithreviewer/runner_test.go`
- Modify: `orchestrator/go.mod`
- Modify: `orchestrator/go.sum`

**Covers:** `R12`, `R13`, `E3`, `E4`, `E5`
**Owner:** `ArtifactWriter`
**Why:** Make runtime artifacts part of the success contract and wire UUIDv7 run identity into both persistence and per-role tmux session metadata.

- [ ] **Step 1: Add the failing artifact tests**

```go
func TestRunWritesStableCaptureNames(t *testing.T) {
    // assert iter-0-implementer.txt, iter-1-reviewer.txt, iter-1-implementer.txt
}

func TestRunWritesPerRoleSessionMetadata(t *testing.T) {
    // assert metadata contains tmux session names for implementer and reviewer
}

func TestRunFailsWhenArtifactWriterFails(t *testing.T) {
    // fake writer returns an error after logical approval
}

func TestRunFailsWhenCleanupFailsAfterApproval(t *testing.T) {
    // fake sessions succeed, then Close returns a terminal cleanup error
}
```

- [ ] **Step 2: Run the runner tests to verify they fail**

Run: `cd orchestrator && go test ./internal/implementwithreviewer`
Expected: FAIL because artifact writing and UUIDv7 run IDs do not exist yet.

- [ ] **Step 3: Implement artifact persistence and run IDs**

```go
u, err := uuid.NewV7()
if err != nil {
    return "", fmt.Errorf("failed to generate run ID: %w", err)
}
runID := u.String()
```

- [ ] **Step 4: Re-run the runner tests and the full suite**

Run: `cd orchestrator && go test ./internal/implementwithreviewer && go test ./...`
Expected: PASS, with fake-writer coverage proving that persistence errors are terminal.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/implementwithreviewer/artifact_writer.go \
        orchestrator/internal/implementwithreviewer/artifact_paths.go \
        orchestrator/internal/implementwithreviewer/runner.go \
        orchestrator/internal/implementwithreviewer/runner_test.go \
        orchestrator/go.mod orchestrator/go.sum
git commit -m "feat: persist implement-with-reviewer run artifacts"
```

### Task 4: ContractDoc

**Files:**
- Modify: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`
- Modify: `orchestrator/internal/implementwithreviewer/runner.go` (only if manual verification exposes a runtime mismatch)
- Modify: `orchestrator/internal/cli/codex.go` (only if manual verification exposes a runtime mismatch)
- Modify: `orchestrator/internal/cli/claude.go` (only if manual verification exposes a runtime mismatch)

**Covers:** `E6`
**Owner:** `ContractDoc`
**Why:** Sync the checked-in command contract with the shipped runtime model and verify the tmux-backed flow manually with deterministic fake backends.

- [ ] **Step 1: Update the contract document**

```md
- persistent role sessions replace one-shot subprocess calls
- each role owns its own tmux session in v1
- runtime artifacts live under log/runs/<run-id>/
- `metadata.json` records per-role tmux session names
- <promise>done</promise> remains in captures and final output
- note the two approved implementation-plan deviations from the original reviewed design
```

- [ ] **Step 2: Build the command**

Run: `make build`
Expected: PASS and refresh `bin/implement-with-reviewer`.

- [ ] **Step 3: Create temporary fake `codex` and `claude` backends**

```bash
tmpbin="$(mktemp -d)"
printf '%s\n' "$tmpbin" > /tmp/iwr-tmpbin-path
cat >"$tmpbin/codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
role="$(basename "$0")"
mode="${HARNESS_FAKE_MODE:-success}"
done_line='Finish your response with exactly <promise>done</promise>.'
turn=0
while IFS= read -r line; do
  [[ "$line" == "$done_line" ]] || continue
  turn=$((turn + 1))
  case "$mode:$role:$turn" in
    success:*:1) printf '%s-started\n<promise>done</promise>\n' "$role" ;;
    success:codex:2) printf 'package demo\nconst HarnessIntegrationToken = "v1"\n<promise>done</promise>\n' ;;
    success:codex:3) printf 'package demo\nconst HarnessIntegrationToken = "v2"\n<promise>done</promise>\n' ;;
    timeout:*:1) printf '%s-started\n<promise>done</promise>\n' "$role" ;;
    timeout:codex:2) printf 'package demo\nconst HarnessIntegrationToken = "v1"\n<promise>done</promise>\n' ;;
    launchfail:*:*) exit 17 ;;
  esac
done
EOF
cp "$tmpbin/codex" "$tmpbin/claude"
chmod +x "$tmpbin/codex" "$tmpbin/claude"
```

- [ ] **Step 4: Adjust the fake reviewer backend for approval and timeout**

```bash
tmpbin="$(cat /tmp/iwr-tmpbin-path)"
cat >"$tmpbin/claude" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
role="$(basename "$0")"
mode="${HARNESS_FAKE_MODE:-success}"
done_line='Finish your response with exactly <promise>done</promise>.'
turn=0
while IFS= read -r line; do
  [[ "$line" == "$done_line" ]] || continue
  turn=$((turn + 1))
  case "$mode:$role:$turn" in
    success:*:1) printf '%s-started\n<promise>done</promise>\n' "$role" ;;
    success:claude:2) printf 'Use HarnessIntegrationToken = "v2"\n<promise>done</promise>\n' ;;
    success:claude:3) printf '<promise>APPROVED</promise>\n<promise>done</promise>\n' ;;
    timeout:*:1) printf '%s-started\n<promise>done</promise>\n' "$role" ;;
    timeout:claude:2) printf 'reviewer-stalled\n'; sleep 130 ;;
    launchfail:*:*) exit 17 ;;
  esac
done
EOF
chmod +x "$tmpbin/claude"
```

- [ ] **Step 5: Run the success-path manual verification**

Run:

```bash
cd orchestrator
tmpbin="$(cat /tmp/iwr-tmpbin-path)"
HARNESS_FAKE_MODE=success PATH="$tmpbin:$PATH" ../bin/implement-with-reviewer \
  --implementer codex --reviewer claude --max-iterations 2 <<'EOF'
Create a Go snippet that defines package demo and constant HarnessIntegrationToken. Return code only.
EOF
```

Expected: success exit, transcript banners, reviewer feedback then approval, final implementation containing `HarnessIntegrationToken = "v2"`, and a new `log/runs/<run-id>/` directory with `metadata.json`, `state-transitions.jsonl`, `result.json`, `captures/iter-0-implementer.txt`, `captures/iter-1-reviewer.txt`, `captures/iter-1-implementer.txt`, and `captures/iter-2-reviewer.txt`.

- [ ] **Step 6: Run the launch-failure verification**

Run:

```bash
cd orchestrator
tmpbin="$(cat /tmp/iwr-tmpbin-path)"
HARNESS_FAKE_MODE=launchfail PATH="$tmpbin:$PATH" ../bin/implement-with-reviewer \
  --implementer codex --reviewer claude --max-iterations 1 <<'EOF'
Return code only.
EOF
```

Expected: exit `1`, runtime error on stderr, no fallback to one-shot adapter behavior, and a partial run directory when metadata was already initialized.

- [ ] **Step 7: Run the timeout verification**

Run:

```bash
cd orchestrator
tmpbin="$(cat /tmp/iwr-tmpbin-path)"
HARNESS_FAKE_MODE=timeout PATH="$tmpbin:$PATH" ../bin/implement-with-reviewer \
  --implementer codex --reviewer claude --max-iterations 1 <<'EOF'
Create a Go snippet that defines package demo and constant HarnessIntegrationToken. Return code only.
EOF
```

Expected: exit `1` after roughly 120 seconds of reviewer inactivity, `captures/iter-1-reviewer-timeout.txt` in the newest run directory, and `result.json` marked failed.

- [ ] **Step 8: Inspect artifacts and cleanup**

Run:

```bash
cd orchestrator
run_dir="$(ls -td log/runs/* | head -n 1)"
run_id="$(basename "$run_dir")"
test -f "$run_dir/metadata.json"
test -f "$run_dir/state-transitions.jsonl"
test -f "$run_dir/result.json"
python - <<'PY' "$run_dir/metadata.json"
import json, sys
data = json.load(open(sys.argv[1]))
assert "implementer" in data["sessions"]
assert "reviewer" in data["sessions"]
print(data["sessions"]["implementer"]["tmux_session_name"])
print(data["sessions"]["reviewer"]["tmux_session_name"])
PY
! tmux ls 2>/dev/null | rg "$run_id"
```

Expected: all artifact files exist, metadata records two role session names, and no tmux session names containing the latest `run_id` remain after command completion.

- [ ] **Step 9: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer/CONTRACT.md
git commit -m "docs: update implement-with-reviewer contract"
```
