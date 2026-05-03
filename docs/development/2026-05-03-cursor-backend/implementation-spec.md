# Cursor Backend Implementation Plan

**Goal:** Add Cursor as the third supported tmux-backed harness backend and launcher without changing the shared session lifecycle model.

**Architecture:** Extend the existing `orchestrator/session` plus `orchestrator/internal/session/backend` split rather than reopening launcher architecture. Add one new public constructor, one new backend implementation, one new thin command adapter, one new contract doc, and the minimal build/doc updates needed to make Cursor a first-class supported launcher.

**Tech Stack:** Go 1.26, standard library `flag`/`fmt`/`io`, existing `orchestrator/session` lifecycle package, tmux CLI, existing launcher contract docs, repo `Makefile`.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Expose `session.NewCursor(config)` from the public session package, returning the same `*session.Session` handle type as Codex and Claude and defaulting the session name to `cursor` when `Config.SessionName` is empty. | `SessionLifecycle` | `CursorBackend` | `orchestrator/session/session.go`<br>`orchestrator/session/session_test.go` | `NewCursor(Config) *Session`<br>`newSession(backend.Cursor{}, config)` | `orchestrator/session/session_test.go` constructor/default-name tests plus the existing session package regression suite. |
| R2 | Implement a private `backend.Cursor` backend that launches bare `agent` through the existing launch-command builder, injects no Cursor-specific startup flags, and reuses the shared tmux send path for prompts. | `CursorBackend` | `SessionLifecycle` | `orchestrator/internal/session/backend/cursor.go`<br>`orchestrator/internal/session/backend/backend_test.go` | `DefaultSessionName() string`<br>`Launch(pane, buildLaunchCommand)`<br>`SendPrompt(pane, prompt)` | `orchestrator/internal/session/backend/backend_test.go` launch-command and prompt-send regression tests. |
| E1 | Cursor readiness succeeds when the shared quiet-period loop observes a stable capture containing the literal `Cursor Agent`. | `CursorBackend` | `SessionLifecycle` | `orchestrator/internal/session/backend/cursor.go`<br>`orchestrator/internal/session/backend/backend_test.go` | `WaitUntilReady(pane, ReadinessOptions)`<br>`cursorReady(capture string) bool` | `orchestrator/internal/session/backend/backend_test.go` ready-state test using repeated stable captures and the existing fake time helpers. |
| E2 | Cursor readiness must treat representative login, sign-in, authenticate, trust, setup, and `press enter to continue` interstitials as not-ready so startup expires through the standard readiness timeout path. | `CursorBackend` | `SessionLifecycle` | `orchestrator/internal/session/backend/cursor.go`<br>`orchestrator/internal/session/backend/backend_test.go` | `cursorReady(capture string) bool`<br>`WaitUntilReady(pane, ReadinessOptions)` | `orchestrator/internal/session/backend/backend_test.go` rejection table for representative interstitial captures plus timeout assertion via `ReadinessError`. |
| R3 | Add `tmux_cursor` as a standalone thin launcher with the same CLI surface, attach-only mkpipe contract, validation rules, and exit-code mapping as `tmux_codex` and `tmux_claude`. | `CursorLauncherCLI` | `SessionLifecycle` | `orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `run(args, stdin, stdout, stderr, deps)`<br>`parseArgs(args, stderr)`<br>`extractMkpipeArgs(args)` | `orchestrator/cmd/tmux_cursor/main_test.go` parser/validation/help tests mirroring the existing launcher pattern. |
| E3 | `tmux_cursor` must fail with exit code `1` when the session constructor dependency is nil or when it returns nil, rather than panicking or printing a success banner. | `CursorLauncherCLI` | None | `orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `cursorDeps.newSession` nil guard path inside `run(...)` | `orchestrator/cmd/tmux_cursor/main_test.go` nil-constructor and nil-session guard tests. |
| R4 | `tmux_cursor` success output must match the design exactly: detached launch prints `Launched Cursor in tmux session "<session-name>"`, attached launch without mkpipe prints no launch banner, and attached launch with mkpipe prints exactly one pre-attach line with the absolute FIFO path. | `CursorLauncherCLI` | `SessionLifecycle` | `orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `Start()`<br>`Attach(session.AttachOptions{BeforeAttach: ...})`<br>`session.AttachInfo{SessionName, MkpipePath}` | `orchestrator/cmd/tmux_cursor/main_test.go` detached banner, attach path, and mkpipe banner tests. |
| R5 | Cursor sessions must reuse the existing session lifecycle, lock policy, attach flow, mkpipe behavior, and cleanup semantics unchanged; the new launcher should only wire `session.NewCursor`, `session.CurrentDirectoryLockPolicy()`, and optional `session.MkpipeConfig`. | `SessionLifecycle` | `CursorLauncherCLI` | `orchestrator/session/session.go`<br>`orchestrator/session/session_test.go`<br>`orchestrator/cmd/tmux_cursor/main.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | `session.NewCursor(session.Config{SessionName, Mkpipe, LockPolicy})`<br>`(*Session).Start()`<br>`(*Session).Attach(...)` | `orchestrator/session/session_test.go` constructor/default-name coverage plus `orchestrator/cmd/tmux_cursor/main_test.go` attach and mkpipe wiring assertions. |
| R6 | Product contract docs must represent the supported launcher surface as exactly `tmux_codex`, `tmux_claude`, and `tmux_cursor`, and add a dedicated `tmux_cursor` contract doc covering invocation, validation, runtime model, output, failure semantics, and exit codes. | `LauncherSurfaceDocs` | `CursorLauncherCLI`<br>`CursorBackend`<br>`BuildSurface` | `orchestrator/cmd/tmux_cursor/CONTRACT.md`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `## Product Surface`<br>`## Invocation`<br>`## Validation Contract`<br>`## Runtime Model`<br>`## Output Contract`<br>`## Failure Semantics`<br>`## Exit Codes` | Manual doc verification with `rg` across the three contract docs; no Go unit test expected. |
| R7 | `make build` must produce `bin/tmux_cursor` alongside the two existing launcher binaries, while `make setup` remains unchanged and Cursor resolution stays operator-managed through the existing shell environment. | `BuildSurface` | None | `Makefile` | `build:` target recipe for `./cmd/tmux_cursor`<br>`setup:` target unchanged | Root-level verification with `make build`, `test -x bin/tmux_cursor`, and review that `setup:` is untouched. |

## Component Responsibility Map

- `SessionLifecycle`: primary owner for `R1` and `R5`. It adds only the new public constructor and keeps Cursor on the existing `Session` lifecycle path. It collaborates with `CursorBackend` through `newSession(backend.Cursor{}, config)` and with `CursorLauncherCLI` through `Start()` / `Attach()`. It does not own Cursor-specific readiness logic, CLI wording, or doc text.
- `CursorBackend`: primary owner for `R2`, `E1`, and `E2`. It owns the Cursor-specific backend differences: default session name, launch command, prompt sending, and the non-brittle ready matcher. It collaborates with `SessionLifecycle` via `Launch`, `WaitUntilReady`, and `SendPrompt`. It does not own lock/mkpipe/cleanup sequencing or user-facing command output.
- `CursorLauncherCLI`: primary owner for `R3`, `E3`, and `R4`. It owns `tmux_cursor` flag parsing, validation, exit-code mapping, detached success wording, and attach-time mkpipe banner wording. It collaborates with `SessionLifecycle` only through `session.NewCursor`, `session.CurrentDirectoryLockPolicy()`, `Start()`, and `Attach()`. It does not own backend readiness internals or session cleanup behavior.
- `LauncherSurfaceDocs`: primary owner for `R6`. It owns the new Cursor contract doc and the truthfulness of the shared launcher-surface wording in the existing Codex and Claude contract docs. It does not own runtime behavior.
- `BuildSurface`: primary owner for `R7`. It owns only the repo build output surface in `Makefile`. It does not own `scripts/.agentrc`, `scripts/bin`, or any new setup flow.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CursorLauncherCLI` | `SessionLifecycle` | `session.NewCursor(session.Config{SessionName, Mkpipe, LockPolicy}) *session.Session` | `tmux_cursor` remains a thin adapter. It passes only the requested session name, optional mkpipe path, and the standard current-directory lock policy into the shared session layer. |
| `SessionLifecycle` | `CursorBackend` | `backend.Cursor{}` wired through `newSession(...)` | `NewCursor` must only choose the backend implementation. No Cursor-specific lifecycle branches belong in `orchestrator/session/session.go`. |
| `CursorBackend` | `LaunchCommandBuilder` | `buildLaunchCommand("agent")` | Keeps the shared `.agentrc` / `~/.agent-bin` / `stty -echo` environment model unchanged and adds no Cursor-specific startup flags. |
| `CursorBackend` | shared readiness loop in `backend.go` | `waitUntilReady(pane, cursorReady, ReadinessOptions)` | The shared quiet-period and timeout logic remain untouched. `cursorReady` should only classify captures as ready or not-ready. |
| `CursorLauncherCLI` | `SessionLifecycle` | `Start()` / `Attach(session.AttachOptions{BeforeAttach: ...})` | Detached runs print the launch banner only after `Start()` succeeds. Attached runs rely on `BeforeAttach` for the mkpipe banner and otherwise hand control directly to tmux attach. |
| `CursorLauncherCLI` | `AttachOptions.BeforeAttach` callback | `session.AttachInfo{SessionName, MkpipePath}` | `MkpipePath` is printed only when `--mkpipe` was requested. The banner must be identical to the design document wording. |
| `BuildSurface` | `cmd/tmux_cursor` package | `go build -o ../bin/tmux_cursor ./cmd/tmux_cursor` | The `Makefile` change is limited to `build:`. `setup:` stays exactly as-is. |
| `LauncherSurfaceDocs` | `CursorLauncherCLI` / `CursorBackend` / `BuildSurface` | CONTRACT section text | The three contract docs must agree on the launcher surface and Cursor-specific operator contract after implementation lands. |

## File Ownership Map

- Modify `orchestrator/session/session.go` - owned by `SessionLifecycle`; add `NewCursor(config)` and keep all other lifecycle behavior untouched.
- Modify `orchestrator/session/session_test.go` - owned by `SessionLifecycle`; add Cursor constructor/default-name coverage without widening into new lifecycle cases.
- Create `orchestrator/internal/session/backend/cursor.go` - owned by `CursorBackend`; implement Cursor-specific default name, launch command, prompt send path, and readiness matcher.
- Modify `orchestrator/internal/session/backend/backend_test.go` - owned by `CursorBackend`; add Cursor backend regression tests alongside the existing Codex and Claude coverage.
- Create `orchestrator/cmd/tmux_cursor/main.go` - owned by `CursorLauncherCLI`; implement the new thin launcher adapter mirroring current launcher structure and wording patterns.
- Create `orchestrator/cmd/tmux_cursor/main_test.go` - owned by `CursorLauncherCLI`; cover parser behavior, detached/attach output, mkpipe banner, and constructor guards.
- Create `orchestrator/cmd/tmux_cursor/CONTRACT.md` - owned by `LauncherSurfaceDocs`; document the Cursor launcher contract in the same structure as the existing launcher docs.
- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `LauncherSurfaceDocs`; update any shared launcher-surface wording that would otherwise still imply only two binaries.
- Modify `orchestrator/cmd/tmux_claude/CONTRACT.md` - owned by `LauncherSurfaceDocs`; apply the same launcher-surface wording fixes for the three-launcher product surface.
- Modify `Makefile` - owned by `BuildSurface`; add the `tmux_cursor` build line and leave `setup:` unchanged.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/session/session.go`
- `orchestrator/session/session_test.go`
- `orchestrator/internal/session/backend/cursor.go`
- `orchestrator/internal/session/backend/backend_test.go`
- `orchestrator/cmd/tmux_cursor/main.go`
- `orchestrator/cmd/tmux_cursor/main_test.go`
- `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`
- `Makefile`

**Incidental-only files:**
- None expected. Do not widen into `orchestrator/internal/session/backend/backend.go`, `orchestrator/internal/session/env/env.go`, `scripts/.agentrc`, `scripts/bin/*`, `orchestrator/cmd/tmux_codex/*.go`, or `orchestrator/cmd/tmux_claude/*.go` unless implementation exposes a compile-breaking gap in the current constructor pattern.

## Task List

All commands below assume the working directory is repo root `.../.workspace/harness`.

Baseline before feature work: `cd orchestrator && go test ./...` passes as of 2026-05-03.

Current branch context: the session-package refactor files referenced below already exist in the worktree. Stay inside the allowlist and do not resurrect the deleted legacy `orchestrator/internal/agent/*` paths.

### Task 1: CursorBackend

**Files:**
- Create: `orchestrator/internal/session/backend/cursor.go`
- Modify: `orchestrator/internal/session/backend/backend_test.go`
- Test: `orchestrator/internal/session/backend/backend_test.go`

**Covers:** `R2`, `E1`, `E2`
**Owner:** `CursorBackend`
**Why:** This task establishes all Cursor-specific backend differences in one place: default session name, launch command, prompt send path, and the minimal-but-safe readiness matcher.

- [ ] **Step 1: Write the failing backend regression tests**

```go
func TestCursorDefaultsLaunchPromptAndReadyMatcher(t *testing.T) {
	var b Backend = Cursor{}
	if got := b.DefaultSessionName(); got != "cursor" {
		t.Fatalf("DefaultSessionName() = %q, want cursor", got)
	}

	pane := &recordingPane{}
	err := b.Launch(pane, func(command string, args ...string) (string, error) {
		if command != "agent" || len(args) != 0 {
			t.Fatalf("build command = %q %v, want agent []", command, args)
		}
		return "launch agent", nil
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if err := b.SendPrompt(pane, "hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got := pane.sent; len(got) != 2 || got[0] != "launch agent" || got[1] != "hello" {
		t.Fatalf("pane sent = %v, want launch then prompt", got)
	}

	now := time.Unix(0, 0)
	pane.captures = []string{"Cursor Agent", "Cursor Agent"}
	if err := b.WaitUntilReady(pane, testReadinessOptions(&now)); err != nil {
		t.Fatalf("WaitUntilReady() error = %v", err)
	}
}

func TestCursorReadyRejectsRepresentativeInterstitials(t *testing.T) {
	cases := []string{
		"Log in to continue",
		"Sign in",
		"Authentication required",
		"Do you trust this folder?",
		"Setup your environment",
		"Press Enter to continue",
	}
	for _, capture := range cases {
		if cursorReady(capture) {
			t.Fatalf("cursorReady(%q) = true, want false", capture)
		}
	}
}

func TestCursorInterstitialsStayNotReadyUntilTimeout(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &recordingPane{captures: []string{"Sign in to Cursor"}}

	err := Cursor{}.WaitUntilReady(pane, shortReadinessOptions(&now))
	if err == nil {
		t.Fatal("expected timeout")
	}
	var readinessErr *ReadinessError
	if !errors.As(err, &readinessErr) || readinessErr.Capture != "Sign in to Cursor" {
		t.Fatalf("error = %#v, want ReadinessError with latest interstitial capture", err)
	}
}
```

- [ ] **Step 2: Run the targeted backend tests to verify they fail**

Run: `cd orchestrator && go test ./internal/session/backend -run 'TestCursorDefaultsLaunchPromptAndReadyMatcher|TestCursorReadyRejectsRepresentativeInterstitials|TestCursorInterstitialsStayNotReadyUntilTimeout' -v`
Expected: FAIL with `undefined: Cursor` and `undefined: cursorReady`.

- [ ] **Step 3: Write the minimal Cursor backend implementation**

```go
package backend

import (
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/tmux"
)

type Cursor struct{}

func (Cursor) DefaultSessionName() string { return "cursor" }

func (Cursor) Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error {
	return launchCommand(pane, buildLaunchCommand, "agent")
}

func (Cursor) WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error {
	return waitUntilReady(pane, cursorReady, opts)
}

func cursorReady(capture string) bool {
	lower := strings.ToLower(capture)
	if strings.Contains(lower, "log in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "authenticate") ||
		strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "trust") ||
		strings.Contains(lower, "setup") ||
		strings.Contains(lower, "press enter to continue") {
		return false
	}
	return strings.Contains(capture, "Cursor Agent")
}

func (Cursor) SendPrompt(pane tmux.TmuxPaneLike, prompt string) error {
	return sendPrompt(pane, prompt)
}
```

- [ ] **Step 4: Run the Cursor backend regression suite**

Run: `cd orchestrator && go test ./internal/session/backend -run 'TestCursorDefaultsLaunchPromptAndReadyMatcher|TestCursorReadyRejectsRepresentativeInterstitials|TestCursorInterstitialsStayNotReadyUntilTimeout|TestWaitUntilReadyReturnsLatestCaptureOnTimeout' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/internal/session/backend/cursor.go orchestrator/internal/session/backend/backend_test.go
git commit -m "feat: add cursor backend"
```

### Task 2: SessionLifecycle

**Files:**
- Modify: `orchestrator/session/session.go`
- Modify: `orchestrator/session/session_test.go`
- Test: `orchestrator/session/session_test.go`

**Covers:** `R1`, `R5`
**Owner:** `SessionLifecycle`
**Why:** The public session package should change only enough to expose Cursor through the existing lifecycle handle and constructor pattern.

- [ ] **Step 1: Write the failing session constructor tests**

```go
func TestNewCursorUsesBackendDefaultSessionName(t *testing.T) {
	s := NewCursor(Config{})
	if got := s.SessionName(); got != "cursor" {
		t.Fatalf("SessionName() = %q, want cursor", got)
	}
}

func TestNewCursorHonorsExplicitSessionName(t *testing.T) {
	s := NewCursor(Config{SessionName: "review"})
	if got := s.SessionName(); got != "review" {
		t.Fatalf("SessionName() = %q, want review", got)
	}
}
```

- [ ] **Step 2: Run the targeted session tests to verify they fail**

Run: `cd orchestrator && go test ./session -run 'TestNewCursorUsesBackendDefaultSessionName|TestNewCursorHonorsExplicitSessionName' -v`
Expected: FAIL with `undefined: NewCursor`.

- [ ] **Step 3: Add the public Cursor constructor**

```go
func NewCursor(config Config) *Session {
	return newSession(backend.Cursor{}, config)
}
```

- [ ] **Step 4: Run the targeted session regression tests**

Run: `cd orchestrator && go test ./session -run 'TestNewCursorUsesBackendDefaultSessionName|TestNewCursorHonorsExplicitSessionName|TestNewCodexUsesBackendDefaultSessionName' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/session/session.go orchestrator/session/session_test.go
git commit -m "feat: add cursor session constructor"
```

### Task 3: CursorLauncherCLI

**Files:**
- Create: `orchestrator/cmd/tmux_cursor/main.go`
- Create: `orchestrator/cmd/tmux_cursor/main_test.go`
- Test: `orchestrator/cmd/tmux_cursor/main_test.go`

**Covers:** `R3`, `E3`, `R4`, `R5`
**Owner:** `CursorLauncherCLI`
**Why:** This task adds the new operator-facing launcher while preserving the current thin-adapter command pattern and wiring Cursor into the existing lock-policy, attach, and mkpipe lifecycle without inventing launcher-specific behavior.

- [ ] **Step 1: Write the failing command tests for the new launcher package**

```go
type fakeCursorSession struct {
	name       string
	config     session.Config
	startErr   error
	attachErr  error
	started    bool
	attached   bool
	attachOpts session.AttachOptions
}

func (s *fakeCursorSession) SessionName() string { return s.name }
func (s *fakeCursorSession) Start() error {
	s.started = true
	return s.startErr
}
func (s *fakeCursorSession) Attach(opts session.AttachOptions) error {
	s.attached = true
	s.attachOpts = opts
	if opts.BeforeAttach != nil {
		opts.BeforeAttach(session.AttachInfo{SessionName: s.name, MkpipePath: "/tmp/.cursor-dev.mkpipe"})
	}
	return s.attachErr
}

func TestRunUsesDefaultCursorSessionName(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-main"}
	var stdout bytes.Buffer
	exitCode := run(nil, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.SessionName != "cursor" {
				t.Fatalf("unexpected default session name: %q", config.SessionName)
			}
			if config.LockPolicy == nil {
				t.Fatal("expected lock policy")
			}
			fake.config = config
			return fake
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stdout.String(), `Launched Cursor in tmux session "cursor-main"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunLaunchesCursorAndPrintsBanner(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--session", "dev"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.SessionName != "dev" {
				t.Fatalf("unexpected session name: %q", config.SessionName)
			}
			if config.LockPolicy == nil {
				t.Fatal("expected lock policy")
			}
			return fake
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, `Launched Cursor in tmux session "cursor-dev"`) {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunAttachesCursorSessionWithoutBanner(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession { return fake },
	})
	if exitCode != 0 || fake.started || !fake.attached {
		t.Fatalf("exit=%d started=%v attached=%v", exitCode, fake.started, fake.attached)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no pre-attach banner", stdout.String())
	}
}

func TestRunCursorMkpipePassesConfigAndPrintsStatusFromHook(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe", "./custom.pipe"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.Mkpipe == nil || config.Mkpipe.Path != "./custom.pipe" {
				t.Fatalf("unexpected mkpipe config: %+v", config.Mkpipe)
			}
			return fake
		},
	})
	if exitCode != 0 || !fake.attached {
		t.Fatalf("exit=%d attached=%v", exitCode, fake.attached)
	}
	want := "Attaching Cursor tmux session \"cursor-dev\" with mkpipe \"/tmp/.cursor-dev.mkpipe\"\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReturnsErrorWhenCursorSessionConstructorIsNil(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, cursorDeps{})
	if exitCode != 1 || !strings.Contains(stderr.String(), "cursor session constructor is not configured") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunReturnsErrorWhenCursorSessionConstructorReturnsNil(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession { return nil },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "cursor session constructor returned nil") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunCursorAttachFailureReturnsError(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev", attachErr: errors.New("attach failed")}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, io.Discard, &stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession { return fake },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestParseArgsSupportsCursorMkpipeForms(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{name: "bare_before_attach", args: []string{"--mkpipe", "--attach"}, wantPath: ""},
		{name: "bare_after_attach", args: []string{"--attach", "--mkpipe"}, wantPath: ""},
		{name: "explicit_relative", args: []string{"--session", "reviewer", "--mkpipe", "./custom.pipe", "--attach"}, wantPath: "./custom.pipe"},
		{name: "next_flag_not_consumed", args: []string{"--mkpipe", "--session", "named", "--attach"}, wantPath: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, exitCode, ok := parseArgs(tt.args, io.Discard)
			if !ok || exitCode != 0 {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d", tt.args, ok, exitCode)
			}
			if !parsed.mkpipeEnabled || parsed.mkpipePath != tt.wantPath {
				t.Fatalf("mkpipeEnabled=%v mkpipePath=%q", parsed.mkpipeEnabled, parsed.mkpipePath)
			}
		})
	}
}

func TestParseArgsRejectsCursorUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
		{name: "positional", args: []string{"extra"}, wantSubstring: "unexpected positional arguments"},
		{name: "blank_session", args: []string{"--session", "  "}, wantSubstring: "invalid --session"},
		{name: "duplicate", args: []string{"--attach", "--mkpipe", "--mkpipe"}, wantSubstring: "invalid --mkpipe: may be provided at most once"},
		{name: "missing_attach", args: []string{"--mkpipe"}, wantSubstring: "invalid --mkpipe: requires --attach"},
		{name: "raw_dash_path", args: []string{"--mkpipe", "-pipe", "--attach"}, wantSubstring: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, exitCode, ok := parseArgs(tt.args, &stderr)
			if ok || exitCode != 2 || !strings.Contains(stderr.String(), tt.wantSubstring) {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d stderr=%q", tt.args, ok, exitCode, stderr.String())
			}
		})
	}
}

func TestRunHelpReturnsSuccess(t *testing.T) {
	exitCode := run([]string{"-h"}, nil, io.Discard, io.Discard, cursorDeps{})
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}
```

- [ ] **Step 2: Run the launcher package tests to verify the package is missing**

Run: `cd orchestrator && go test ./cmd/tmux_cursor -v`
Expected: FAIL with compile errors such as `undefined: run`, `undefined: cursorDeps`, and `undefined: cursorSession` until `main.go` is added.

- [ ] **Step 3: Implement `tmux_cursor` by mirroring the existing launcher adapter pattern**

```go
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/session"
)

const (
	cursorProgramName        = "tmux_cursor"
	cursorDefaultSessionName = "cursor"
	cursorSuccessLabel       = "Cursor"
)

type cursorLaunchArgs struct {
	sessionName   string
	attach        bool
	mkpipeEnabled bool
	mkpipePath    string
}

type cursorDeps struct {
	newSession func(session.Config) cursorSession
}

type cursorSession interface {
	SessionName() string
	Start() error
	Attach(session.AttachOptions) error
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			return session.NewCursor(config)
		},
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps cursorDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.newSession == nil {
		fmt.Fprintln(stderr, "cursor session constructor is not configured")
		return 1
	}
	config := session.Config{
		SessionName: parsed.sessionName,
		LockPolicy:  session.CurrentDirectoryLockPolicy(),
	}
	if parsed.mkpipeEnabled {
		config.Mkpipe = &session.MkpipeConfig{Path: parsed.mkpipePath}
	}
	sess := deps.newSession(config)
	if sess == nil {
		fmt.Fprintln(stderr, "cursor session constructor returned nil")
		return 1
	}
	if parsed.attach {
		err := sess.Attach(session.AttachOptions{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
			BeforeAttach: func(info session.AttachInfo) {
				if parsed.mkpipeEnabled {
					fmt.Fprintf(stdout, "Attaching Cursor tmux session %q with mkpipe %q\n", info.SessionName, info.MkpipePath)
				}
			},
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		return 0
	}

	if err := sess.Start(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintf(stdout, "Launched %s in tmux session %q\n", cursorSuccessLabel, sess.SessionName())
	return 0
}

func parseArgs(args []string, stderr io.Writer) (cursorLaunchArgs, int, bool) {
	cleanArgs, mkpipeEnabled, mkpipePath, err := extractMkpipeArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return cursorLaunchArgs{}, 2, false
	}

	flagSet := flag.NewFlagSet(cursorProgramName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	sessionName := flagSet.String("session", cursorDefaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(cleanArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cursorLaunchArgs{}, 0, false
		}
		return cursorLaunchArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return cursorLaunchArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(stderr, "invalid --session: must not be empty")
		return cursorLaunchArgs{}, 2, false
	}
	if mkpipeEnabled && !*attach {
		fmt.Fprintln(stderr, "invalid --mkpipe: requires --attach")
		return cursorLaunchArgs{}, 2, false
	}

	return cursorLaunchArgs{
		sessionName:   *sessionName,
		attach:        *attach,
		mkpipeEnabled: mkpipeEnabled,
		mkpipePath:    mkpipePath,
	}, 0, true
}

func extractMkpipeArgs(args []string) ([]string, bool, string, error) {
	cleanArgs := make([]string, 0, len(args))
	mkpipeEnabled := false
	mkpipePath := ""

	for i := 0; i < len(args); i++ {
		if args[i] != "--mkpipe" {
			cleanArgs = append(cleanArgs, args[i])
			continue
		}
		if mkpipeEnabled {
			return nil, false, "", fmt.Errorf("invalid --mkpipe: may be provided at most once")
		}
		mkpipeEnabled = true
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			mkpipePath = args[i+1]
			i++
		}
	}

	return cleanArgs, mkpipeEnabled, mkpipePath, nil
}
```

- [ ] **Step 4: Run the full Cursor launcher regression suite**

Run: `cd orchestrator && go test ./cmd/tmux_cursor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/tmux_cursor/main.go orchestrator/cmd/tmux_cursor/main_test.go
git commit -m "feat: add tmux_cursor launcher"
```

### Task 4: LauncherSurfaceDocs

**Files:**
- Create: `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_claude/CONTRACT.md`
- Test: contract docs via `rg`

**Covers:** `R6`
**Owner:** `LauncherSurfaceDocs`
**Why:** The contract docs need to stay truthful about the supported launcher surface and give operators a standalone `tmux_cursor` reference.

- [ ] **Step 1: Write the contract checklist to drive the doc edits**

```markdown
# tmux_cursor Product Contract

## Product Surface
## Purpose
## Invocation
## Inputs
## Validation Contract
## Runtime Model
## Output Contract
## Failure Semantics
## Exit Codes
```

- [ ] **Step 2: Run the pre-edit doc check**

Run: `rg -n "tmux_cursor|supported operator-facing harness binaries|both launchers" orchestrator/cmd/tmux_cursor/CONTRACT.md orchestrator/cmd/tmux_codex/CONTRACT.md orchestrator/cmd/tmux_claude/CONTRACT.md`
Expected: `orchestrator/cmd/tmux_cursor/CONTRACT.md` is missing and the existing docs still reflect a two-launcher surface.

- [ ] **Step 3: Write the new Cursor contract doc and update the existing launcher-surface wording**

```markdown
The supported operator-facing harness binaries are exactly:

- `tmux_codex`
- `tmux_claude`
- `tmux_cursor`
```

- [ ] **Step 4: Run the post-edit doc verification**

Run: `bash -lc 'rg -n "tmux_cursor|supported operator-facing harness binaries|## Invocation|## Validation Contract|## Runtime Model|## Output Contract|## Failure Semantics|## Exit Codes" orchestrator/cmd/tmux_cursor/CONTRACT.md orchestrator/cmd/tmux_codex/CONTRACT.md orchestrator/cmd/tmux_claude/CONTRACT.md && ! rg -n "both launchers|tmux_codex and tmux_claude" orchestrator/cmd/tmux_codex/CONTRACT.md orchestrator/cmd/tmux_claude/CONTRACT.md'`
Expected: PASS, with the new Cursor doc containing the full section set and the Codex/Claude docs no longer containing stale two-launcher wording.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/cmd/tmux_cursor/CONTRACT.md orchestrator/cmd/tmux_codex/CONTRACT.md orchestrator/cmd/tmux_claude/CONTRACT.md
git commit -m "docs: add cursor launcher contract"
```

### Task 5: BuildSurface

**Files:**
- Modify: `Makefile`
- Test: root `make build` plus full Go suite

**Covers:** `R7`
**Owner:** `BuildSurface`
**Why:** The product surface is not complete until `make build` emits the new launcher binary and the unchanged setup contract is preserved.

- [ ] **Step 1: Write the failing build assertion**

```bash
make build
test -x bin/tmux_cursor
```

- [ ] **Step 2: Run the build assertion to verify it fails before the `Makefile` change**

Run: `bash -lc 'make build && test -x bin/tmux_cursor'`
Expected: FAIL at `test -x bin/tmux_cursor` because the build target does not exist yet.

- [ ] **Step 3: Add the new build output and leave `setup:` untouched**

```make
build:
	@mkdir -p "$(ROOT)/bin"
	rm -f "$(ROOT)/bin"/*
	cd orchestrator && go build -o ../bin/tmux_codex ./cmd/tmux_codex
	cd orchestrator && go build -o ../bin/tmux_claude ./cmd/tmux_claude
	cd orchestrator && go build -o ../bin/tmux_cursor ./cmd/tmux_cursor
	@rmdir "$(ROOT)/orchestrator/cmd/tmux_agent" 2>/dev/null || true
```

- [ ] **Step 4: Run the final verification**

Run: `bash -lc 'make build && test -x bin/tmux_cursor && (cd orchestrator && go test ./...) && sed -n "1,20p" Makefile'`
Expected: PASS, and the printed `Makefile` excerpt still shows the original `setup:` symlink commands unchanged plus the new `go build -o ../bin/tmux_cursor ./cmd/tmux_cursor` line in `build:`.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "build: add tmux_cursor target"
```
