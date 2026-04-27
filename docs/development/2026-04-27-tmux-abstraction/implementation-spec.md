# Tmux Abstraction Refactor Implementation Plan

**Goal:** Introduce a dedicated tmux abstraction layer with pane-scoped transport and explicit session/pane types, then refactor the existing `codex` / `claude` persistent sessions to depend on that abstraction without changing the runner-facing `cli.Session` contract.

**Architecture:** Add a new `orchestrator/internal/tmux` package that owns tmux contracts plus the exec-backed single-pane session implementation used today. Keep `orchestrator/internal/cli` responsible for backend-specific launch commands, startup prompts, ready matchers, and turn polling; the shared persistent session core should hold an injected `tmux.TmuxLike` pane transport plus its owning `tmux.TmuxSession`. Keep `implementwithreviewer.RunConfig.NewSession` as the only runner seam, and bind a tmux-aware session factory in `main.go` so the runner stays tmux-agnostic.

**Tech Stack:** Go 1.26, `tmux`, bash-sourced launchers via `.agentrc`, existing `implement-with-reviewer` runner and artifact pipeline.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Add a dedicated tmux package with a pane-scoped `TmuxLike` contract plus explicit `TmuxSession` and `TmuxPane` types. `TmuxLike` must stay focused on pane-local transport; session identity, attach-target behavior, and close semantics stay on `TmuxSession`. | `TmuxLayer` | `PersistentSessionCore` | `orchestrator/internal/tmux/tmux.go`, `orchestrator/internal/tmux/tmux_test.go` | `type TmuxLike interface`, `type Factory interface`, `type TmuxSession struct`, `type TmuxPane struct`, `(*TmuxSession).Name()`, `(*TmuxSession).AttachTarget()` | `orchestrator/internal/tmux/tmux_test.go` for contract shape, session identity, pane target, and attach-target behavior |
| R2 | Move the raw tmux command execution now embedded in `orchestrator/internal/cli/command.go` into the new tmux package while preserving the current single-pane launch/capture/reset/close behavior. | `TmuxLayer` | `PersistentSessionCore` | `orchestrator/internal/tmux/tmux.go`, `orchestrator/internal/tmux/tmux_test.go` | `Factory.NewOwnedSession(name, launchCommand)`, `(*TmuxPane).SendText(text)`, `(*TmuxPane).Capture()`, `(*TmuxPane).Reset()`, `(*TmuxSession).Close()` | `orchestrator/internal/tmux/tmux_test.go` with stubbed command execution for `new-session`, `capture-pane`, `load-buffer`, `paste-buffer`, `send-keys`, `clear-history`, `has-session`, and `kill-session` |
| R3 | Refactor the shared persistent session core to depend on injected tmux session/pane objects rather than shelling out to `tmux` directly, while preserving `Start`, `RunTurn`, `Close`, current turn-marker behavior, timeout polling, and `SessionError` normalization. | `PersistentSessionCore` | `TmuxLayer` | `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go`, `orchestrator/internal/tmux/tmux.go` | `newPersistentSession(...)`, `persistentSession{tmuxSession, pane}`, `Start(rolePrompt)`, `RunTurn(prompt)`, `Close()`, `SessionName()` | `orchestrator/internal/cli/command_test.go` for prompt send/capture flow, startup reset normalization, timeout propagation, and close semantics using fake tmux transports |
| R4 | Keep backend-specific behavior in `codex` / `claude` while switching constructors and dispatch to the new tmux-aware creation path. Backend prompt text, ready matchers, and skip-reset behavior must remain in the backend constructors instead of moving into the tmux package. | `BackendSessionFactory` | `PersistentSessionCore`, `TmuxLayer` | `orchestrator/internal/cli/codex.go`, `orchestrator/internal/cli/claude.go`, `orchestrator/internal/cli/factory.go`, `orchestrator/internal/cli/factory_test.go` | `NewCodexSession(tmuxFactory, opts)`, `NewClaudeSession(tmuxFactory, opts)`, `NewSession(name, tmuxFactory, opts)` | `orchestrator/internal/cli/factory_test.go` for backend dispatch, unknown backend errors, and tmux-factory injection; `orchestrator/internal/cli/command_test.go` remains the shared session-core regression suite |
| R5 | Keep the runner tmux-agnostic by continuing to use `implementwithreviewer.RunConfig.NewSession` as the seam. Production wiring in `main.go` must provide a tmux-aware closure instead of passing the bare `cli.NewSession` symbol directly. `defaultRunConfig` in `implementwithreviewer` must not assign `cfg.NewSession = cli.NewSession` once `cli.NewSession` takes a `tmux.Factory` (the assigned type would not match `RunConfig.NewSession` and would reintroduce tmux into the runner package). | `ProductionSessionWiring` | `BackendSessionFactory` | `orchestrator/cmd/implement-with-reviewer/main.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/internal/implementwithreviewer/types.go` | `run(args, runnerConfig)`, `implementwithreviewer.RunConfig{NewSession: ...}`, `defaultRunConfig` (drop default `NewSession` assignment) | `orchestrator/cmd/implement-with-reviewer/main_test.go` asserting the runner config still preserves CLI behavior and passes a non-nil session factory closure; `go test ./...` covers `defaultRunConfig` and explicit `NewSession` in tests |
| R6 | Preserve current runtime topology and metadata semantics: each backend role still owns its own tmux session for now, and `SessionName()` must continue to return the owning tmux session name rather than a pane target. | `PersistentSessionCore` | `BackendSessionFactory`, `TmuxLayer` | `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go`, `orchestrator/internal/tmux/tmux.go` | `sessionNameFor(runID, role)`, `(*TmuxSession).Name()`, `SessionName()` | `orchestrator/internal/cli/command_test.go` asserting session-name reporting still uses the owning tmux session name |
| E1 | Closing an already-absent tmux session must still succeed silently, while unexpected close failures continue surfacing as `SessionErrorKindClose`. | `TmuxLayer` | `PersistentSessionCore` | `orchestrator/internal/tmux/tmux.go`, `orchestrator/internal/tmux/tmux_test.go` | `(*TmuxSession).Close()`, absent-session detection helper | `orchestrator/internal/tmux/tmux_test.go` for `no server running`, `can't find session`, and `server exited unexpectedly` close paths |
| E2 | Pane send/capture/reset failures must still normalize into the existing startup/capture/timeout session errors with pane text preserved when available. | `PersistentSessionCore` | `TmuxLayer` | `orchestrator/internal/cli/command.go`, `orchestrator/internal/cli/command_test.go` | `normalizeStartupError(...)`, `runPrompt(...)`, `waitForDone(...)` | `orchestrator/internal/cli/command_test.go` for startup reset failure, capture failure, and timeout capture propagation |
| E3 | The refactor must stay unit-testable without requiring real tmux: fake tmux factories, sessions, and panes must be injectable into constructor and entrypoint tests, and no new checked-in real-tmux integration suite should be added in this change. | `ProductionSessionWiring` | `BackendSessionFactory`, `TmuxLayer` | `orchestrator/internal/cli/factory_test.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/internal/tmux/tmux_test.go`, `orchestrator/internal/implementwithreviewer/types.go` | fake `tmux.Factory`, fake `TmuxLike`, `RunConfig.NewSession` closure injection; tests must not rely on a default `NewSession` that bakes in real tmux | `orchestrator/internal/cli/factory_test.go`, `orchestrator/cmd/implement-with-reviewer/main_test.go`, `orchestrator/internal/tmux/tmux_test.go`; `implementwithreviewer` tests already pass explicit `NewSession` fakes; final regression is `cd orchestrator && go test ./...` |

## Component Responsibility Map

- `TmuxLayer`: primary owner for the new `orchestrator/internal/tmux` package. It defines the pane-scoped `TmuxLike` contract, the concrete `TmuxSession` / `TmuxPane` types, the exec-backed factory that creates a single-pane session for today’s runtime model, and the tmux absent-session handling rules. It collaborates with `PersistentSessionCore` via `TmuxLike`, `TmuxSession`, and `Factory`. It does not own backend startup prompts, turn polling, or runner wiring.

- `PersistentSessionCore`: primary owner for the shared session behavior currently concentrated in `orchestrator/internal/cli/command.go`: turn marker generation, prompt decoration, startup-vs-turn normalization, done-marker polling, timeout classification, and returning `TurnResult`. It collaborates with `TmuxLayer` through pane transport methods and with `BackendSessionFactory` through `newPersistentSession(...)`. It does not own tmux command execution details, backend-specific startup text, or top-level `RunConfig` wiring.

- `BackendSessionFactory`: primary owner for backend-specific constructor behavior and factory dispatch in `orchestrator/internal/cli`. It keeps Codex and Claude launch commands, ready matchers, and skip-reset flags close to their constructors, and it maps backend names to concrete sessions using an injected tmux factory. It collaborates with `PersistentSessionCore` through `newPersistentSession(...)`. It does not own the runner loop or tmux command execution internals.

- `ProductionSessionWiring`: primary owner for binding the tmux-aware session factory into `implement-with-reviewer` without exposing tmux concepts to the `implementwithreviewer` package. The cmd binary may import `orchestrator/internal/tmux` to build the closure; the runner and `RunConfig` type must only see `func(string, cli.SessionOptions) (cli.Session, error)`. It updates `defaultRunConfig` in `orchestrator/internal/implementwithreviewer/types.go` so it no longer assigns `cfg.NewSession = cli.NewSession` (that default becomes type-invalid and would force the runner to depend on `tmux`). It collaborates with `BackendSessionFactory` through a `RunConfig.NewSession` closure. It does not own backend dispatch, tmux transport behavior, or any turn logic.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `ProductionSessionWiring` | `BackendSessionFactory` | `func(name string, opts cli.SessionOptions) (cli.Session, error)` closure bound to a concrete `tmux.Factory` | `main.go` should pass this closure into `implementwithreviewer.RunConfig.NewSession`. The runner remains unaware of tmux packages and tmux topology. |
| `BackendSessionFactory` | `TmuxLayer` | `tmux.Factory.NewOwnedSession(name string, launchCommand string) (*tmux.TmuxSession, tmux.TmuxLike, error)` | Each backend role still gets its own tmux session. The factory returns the owning session plus the pane-local transport that the session core will use. |
| `BackendSessionFactory` | `PersistentSessionCore` | `newPersistentSession(backendName string, tmuxFactory tmux.Factory, launchCommand string, startupInstruction string, readyMatcher func(string) bool, skipPaneReset bool, opts SessionOptions) (Session, error)` | Keep backend prompts, launch strings, ready matchers, and skip-reset policy in `codex.go` / `claude.go`. Do not move those decisions into `tmux`. |
| `PersistentSessionCore` | `TmuxLike` | `SendText(prompt string) error`, `Capture() (string, error)`, `Reset() error`, `Target() string` | `Capture()` must return raw pane text exactly as tmux captured it. `Reset()` must run only after a successful capture unless startup/turn flow is already failing. `Target()` is diagnostic only; it must not replace `SessionName()`. |
| `PersistentSessionCore` | `TmuxSession` | `Name() string`, `Close() error`, `AttachTarget() string` | `SessionName()` on the `cli.Session` wrapper must delegate to `TmuxSession.Name()`. `AttachTarget()` is future-facing operator affordance; it is not used by the current runner. |

## File Ownership Map

- Create `orchestrator/internal/tmux/tmux.go` - owned by `TmuxLayer`; defines `TmuxLike`, `Factory`, `TmuxSession`, `TmuxPane`, exec-backed creation helpers, and close/attach helpers.
- Create `orchestrator/internal/tmux/tmux_test.go` - owned by `TmuxLayer`; verifies tmux package behavior with stubbed command execution and no real tmux dependency.
- Modify `orchestrator/internal/cli/command.go` - owned by `PersistentSessionCore`; replaces direct tmux shell calls with injected `TmuxSession` / `TmuxLike` collaborators while preserving turn semantics.
- Modify `orchestrator/internal/cli/command_test.go` - owned by `PersistentSessionCore`; replaces raw `runCommand` stubs with fake tmux transport/session collaborators and preserves regression coverage.
- Modify `orchestrator/internal/cli/codex.go` - owned by `BackendSessionFactory`; updates the Codex constructor to call the tmux-aware persistent session constructor while preserving startup prompt and ready matcher behavior.
- Modify `orchestrator/internal/cli/claude.go` - owned by `BackendSessionFactory`; updates the Claude constructor to call the tmux-aware persistent session constructor while preserving startup prompt behavior.
- Modify `orchestrator/internal/cli/factory.go` - owned by `BackendSessionFactory`; changes factory dispatch to require a tmux dependency and keeps unknown-backend behavior unchanged.
- Create `orchestrator/internal/cli/factory_test.go` - owned by `BackendSessionFactory`; verifies backend dispatch and tmux-factory injection without using real tmux.
- Modify `orchestrator/cmd/implement-with-reviewer/main.go` - owned by `ProductionSessionWiring`; binds a tmux-aware `NewSession` closure into `RunConfig` instead of passing the bare `cli.NewSession`.
- Modify `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `ProductionSessionWiring`; asserts the CLI entrypoint still builds a valid runner config and preserves current validation/output behavior.
- Modify `orchestrator/internal/implementwithreviewer/types.go` - owned by `ProductionSessionWiring`; remove the `if cfg.NewSession == nil { cfg.NewSession = cli.NewSession }` branch from `defaultRunConfig` (and rely on `validateRunConfig`’s existing `NewSession` requirement), or otherwise ensure no default `NewSession` is wired here with the wrong function type. Call sites (`main` and tests) already pass an explicit `NewSession` or should be updated to do so.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/tmux/tmux.go`
- `orchestrator/internal/tmux/tmux_test.go`
- `orchestrator/internal/cli/command.go`
- `orchestrator/internal/cli/command_test.go`
- `orchestrator/internal/cli/codex.go`
- `orchestrator/internal/cli/claude.go`
- `orchestrator/internal/cli/factory.go`
- `orchestrator/internal/cli/factory_test.go`
- `orchestrator/cmd/implement-with-reviewer/main.go`
- `orchestrator/cmd/implement-with-reviewer/main_test.go`
- `orchestrator/internal/implementwithreviewer/types.go`

**Incidental-only files:**
- None expected. If an extra file seems necessary, stop and update the plan first instead of expanding the refactor ad hoc.

## Task List

### Task 1: `TmuxLayer`

**Files:**
- Create: `orchestrator/internal/tmux/tmux.go`
- Test: `orchestrator/internal/tmux/tmux_test.go`

**Covers:** `R1`, `R2`, `E1`
**Owner:** `TmuxLayer`
**Why:** Establish the new abstraction boundary before touching session logic so the rest of the refactor can target stable tmux contracts instead of raw shell commands.

- [ ] **Step 1: Write the failing tmux package tests**

```go
func TestExecFactoryCreatesOwnedSessionAndPane(t *testing.T) {
	factory := &ExecFactory{}
	session, pane, err := factory.NewOwnedSession("iwr-run-id-implementer", "bash -lc 'codex'")
	if err != nil {
		t.Fatalf("NewOwnedSession returned error: %v", err)
	}
	if session.Name() != "iwr-run-id-implementer" {
		t.Fatalf("unexpected session name: %q", session.Name())
	}
	if pane.Target() == "" {
		t.Fatal("expected pane target")
	}
}

func TestTmuxSessionCloseSucceedsWhenAlreadyGone(t *testing.T) {
	session := &TmuxSession{name: "iwr-run-id-reviewer"}
	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}
```

- [ ] **Step 2: Run the tmux package tests to verify they fail**

Run: `cd orchestrator && go test ./internal/tmux -run 'TestExecFactoryCreatesOwnedSessionAndPane|TestTmuxSessionCloseSucceedsWhenAlreadyGone' -v`
Expected: FAIL because `orchestrator/internal/tmux` and its symbols do not exist yet.

- [ ] **Step 3: Implement the minimal tmux package**

```go
package tmux

type TmuxLike interface {
	SendText(text string) error
	Capture() (string, error)
	Reset() error
	Target() string
}

type Factory interface {
	NewOwnedSession(name string, launchCommand string) (*TmuxSession, TmuxLike, error)
}
```

Implement the rest of `tmux.go` in the same task: concrete `ExecFactory`, `TmuxSession`, `TmuxPane`, stub-friendly command execution helpers, `AttachTarget()`, and absent-session-aware `Close()`.

- [ ] **Step 4: Run the tmux package tests to verify they pass**

Run: `cd orchestrator && go test ./internal/tmux -v`
Expected: PASS for the new tmux unit tests with no real tmux dependency.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/tmux/tmux.go orchestrator/internal/tmux/tmux_test.go
git commit -m "refactor: add tmux session and pane abstraction"
```

### Task 2: `PersistentSessionCore` + `BackendSessionFactory` (single landing)

`newPersistentSession(...)` is only called from `codex.go` and `claude.go`. Changing its signature or adding a `tmux.Factory` parameter without updating those call sites (and `NewSession`) leaves the tree unbuildable. **Do not commit after updating only `command.go`**—land the following files together in one commit.

**Files:**
- Modify: `orchestrator/internal/cli/command.go`
- Modify: `orchestrator/internal/cli/command_test.go`
- Modify: `orchestrator/internal/cli/codex.go`
- Modify: `orchestrator/internal/cli/claude.go`
- Modify: `orchestrator/internal/cli/factory.go`
- Create: `orchestrator/internal/cli/factory_test.go`

**Covers:** `R3`, `R4`, `R6`, `E2`
**Owner:** `PersistentSessionCore` leads `command.go` / `command_test.go`; `BackendSessionFactory` leads `codex.go` / `claude.go` / `factory.go` / `factory_test.go` (pair program or single assignee covering both).
**Why:** Preserve startup/turn behavior and session-name semantics while moving transport to `TmuxLike` / `TmuxSession`, and keep backend prompts and dispatch in the right files.

- [ ] **Step 1: Rewrite `command_test.go` around fake tmux collaborators**

```go
type fakePane struct {
	captures []string
}

func (p *fakePane) SendText(text string) error { return nil }
func (p *fakePane) Capture() (string, error)   { return p.captures[0], nil }
func (p *fakePane) Reset() error               { return nil }
func (p *fakePane) Target() string             { return "%1" }
```

Update the existing `command_test.go` scenarios so they exercise `Start`, `RunTurn`, `SessionName`, timeout handling, and startup normalization through fake `TmuxLike` / `TmuxSession` collaborators instead of package-global tmux command stubs.

- [ ] **Step 2: Add failing `factory_test.go` tests for tmux-aware dispatch**

```go
func TestNewSessionDispatchesWithInjectedTmuxFactory(t *testing.T) {
	session, err := NewSession("codex", fakeTmuxFactory{}, SessionOptions{RunID: "run-id", Role: "implementer"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if session.SessionName() == "" {
		t.Fatal("expected session name")
	}
}
```

Also add an explicit unknown-backend assertion so the factory contract stays stable after the signature change.

- [ ] **Step 3: Run targeted tests to verify they fail**

Run: `cd orchestrator && go test ./internal/cli -run 'TestRunTurnWaitsForDoneAfterPromptBoundary|TestStartNormalizesResetFailureToStartup|TestCloseSucceedsWhenSessionAlreadyGone|TestNewSessionDispatchesWithInjectedTmuxFactory|TestNewSessionRejectsUnknownBackend' -v`
Expected: FAIL (compile error or failing tests) because `newPersistentSession` and `NewSession` are not yet tmux-aware.

- [ ] **Step 4: Implement the shared refactor in one pass**

- Refactor `persistentSession` to hold `*tmux.TmuxSession` and `tmux.TmuxLike`; update `newPersistentSession(...)` to accept `tmux.Factory` (or equivalent) and build the owning session plus pane from `Factory.NewOwnedSession` using the same launch string as today (`buildSourcedLauncher(command, args...)` and existing `command` / `args` from each backend), preserving `SessionName()` via the owning session.
- Update `NewCodexSession` / `NewClaudeSession` to accept the factory and pass it into `newPersistentSession` with unchanged startup prompts, ready matchers, and `skipPaneReset`.
- Change `NewSession` to:

```go
func NewSession(name string, tmuxFactory tmux.Factory, opts SessionOptions) (Session, error) {
	switch name {
	case "codex":
		return NewCodexSession(tmuxFactory, opts)
	case "claude":
		return NewClaudeSession(tmuxFactory, opts)
	default:
		return nil, fmt.Errorf("unknown backend: %s (expected codex or claude)", name)
	}
}
```

- [ ] **Step 5: Run `internal/cli` tests to verify they pass**

Run: `cd orchestrator && go test ./internal/cli -v`
Expected: PASS for session-core and factory tests, with unchanged error kinds and session-name semantics.

- [ ] **Step 6: Commit (one commit for this task)**

```bash
git add orchestrator/internal/cli/command.go orchestrator/internal/cli/command_test.go orchestrator/internal/cli/codex.go orchestrator/internal/cli/claude.go orchestrator/internal/cli/factory.go orchestrator/internal/cli/factory_test.go
git commit -m "refactor: inject tmux transport and tmux-aware cli session factory"
```

### Task 3: `ProductionSessionWiring`

**Files:**
- Modify: `orchestrator/cmd/implement-with-reviewer/main.go` (import `orchestrator/internal/tmux` for the default exec-backed factory; `implementwithreviewer` must not import it)
- Modify: `orchestrator/cmd/implement-with-reviewer/main_test.go`
- Modify: `orchestrator/internal/implementwithreviewer/types.go` (see R5: remove default `NewSession` assignment)

**Covers:** `R5`, `E3`
**Owner:** `ProductionSessionWiring`
**Why:** Bind the tmux-aware session factory into the existing runner seam without leaking tmux concepts into the `implementwithreviewer` package or widening the `RunConfig.NewSession` function type.

- [ ] **Step 0: Fix `defaultRunConfig` for the new `cli.NewSession` shape**

In `orchestrator/internal/implementwithreviewer/types.go`, remove `if cfg.NewSession == nil { cfg.NewSession = cli.NewSession }` from `defaultRunConfig`. The bare `cli.NewSession` function no longer has type `func(string, cli.SessionOptions) (cli.Session, error)` once it takes a `tmux.Factory`. Production (`main`) and `runner_test` already supply explicit `NewSession` functions; if any test relied on the implicit default, add an explicit fake there.

- [ ] **Step 1: Add a failing entrypoint test for the injected session-factory closure**

```go
func TestRunBuildsTmuxAwareSessionFactory(t *testing.T) {
	recorder := &runRecorder{}
	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:  strings.NewReader("task"),
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		getenv: func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run: recorder.run,
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if recorder.configs[0].NewSession == nil {
		t.Fatal("expected NewSession closure")
	}
}
```

- [ ] **Step 2: Run the entrypoint tests to verify they fail**

Run: `cd orchestrator && go test ./cmd/implement-with-reviewer -run 'TestRunBuildsTmuxAwareSessionFactory|TestRunMaxIterationPrecedenceAndRunnerConfig' -v`
Expected: FAIL because `main.go` still passes the old bare `cli.NewSession` symbol directly.

- [ ] **Step 3: Wire the tmux-aware session factory into `main.go`**

```go
import "github.com/Yongbeom-Kim/harness/orchestrator/internal/tmux"
// ...
NewSession: func(name string, opts cli.SessionOptions) (cli.Session, error) {
	return cli.NewSession(name, tmux.NewExecFactory(), opts)
},
```

`main` (this package) may import `orchestrator/internal/tmux` to close over the real factory. `orchestrator/internal/implementwithreviewer` must not import `tmux`; the runner only sees the two-argument `NewSession` closure.

- [ ] **Step 4: Run the full orchestrator regression suite**

Run: `cd orchestrator && go test ./...`
Expected: PASS. No new real-`tmux` integration suite should be added in this change; all new coverage should be unit-level and stub-driven.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/implement-with-reviewer/main.go orchestrator/cmd/implement-with-reviewer/main_test.go orchestrator/internal/implementwithreviewer/types.go
git commit -m "refactor: wire tmux abstraction into implement-with-reviewer"
```
