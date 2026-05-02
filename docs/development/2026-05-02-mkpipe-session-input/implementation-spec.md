# mkpipe Session Input Implementation Plan

**Goal:** Add attached-session `--mkpipe` FIFO prompt injection to `tmux_codex` and `tmux_claude` without introducing a detached supervisor or changing the launcher-only product shape.

**Architecture:** Keep launcher orchestration duplicated in `tmux_codex` and `tmux_claude`, because that is the existing repository pattern and was explicitly chosen in implementation Q&A. Add one shared `internal/mkpipe` package that owns FIFO path resolution, FIFO creation, EOF-delimited reads, normalization, channel delivery, and deterministic teardown; each launcher consumes that listener through the widened `agent.Agent` interface, forwards prompts immediately, prints the pre-attach banner, and owns cleanup on startup failure, attach return, and interrupt.

**Tech Stack:** Go 1.26, standard library `flag`/`os`/`os/signal`/`syscall`, tmux-backed `internal/agent`, Unix named pipes (FIFOs).

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | `tmux_codex` must accept bare `--mkpipe` and `--mkpipe <path>` before or after `--session`/`--attach`; when the token after `--mkpipe` begins with `-`, bare `--mkpipe` remains enabled and that token is left for normal flag parsing. | `CodexLauncherCLI` | `LauncherMkpipeContractDocs` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md` | `extractMkpipeArgs(args)`<br>`parseArgs(args, stderr)` | `orchestrator/cmd/tmux_codex/main_test.go` parser table covering bare form before/after other flags, explicit path, and raw `-pipe` remaining a normal flag token. |
| E1 | `tmux_codex` must reject duplicate `--mkpipe` and reject `--mkpipe` without `--attach` as usage errors with exit code `2`. | `CodexLauncherCLI` | `LauncherMkpipeContractDocs` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md` | `extractMkpipeArgs(args)`<br>`parseArgs(args, stderr)` | `orchestrator/cmd/tmux_codex/main_test.go` validation cases for duplicate flag and missing `--attach`. |
| R2 | `tmux_claude` must support the same mkpipe syntax contract as Codex, including both flag forms and the same token-disambiguation rule when the token after `--mkpipe` starts with `-`. | `ClaudeLauncherCLI` | `LauncherMkpipeContractDocs` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `extractMkpipeArgs(args)`<br>`parseArgs(args, stderr)` | `orchestrator/cmd/tmux_claude/main_test.go` parser table mirroring Codex coverage. |
| E2 | `tmux_claude` must reject duplicate `--mkpipe` and reject `--mkpipe` without `--attach` as usage errors with exit code `2`. | `ClaudeLauncherCLI` | `LauncherMkpipeContractDocs` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `extractMkpipeArgs(args)`<br>`parseArgs(args, stderr)` | `orchestrator/cmd/tmux_claude/main_test.go` validation cases for duplicate flag and missing `--attach`. |
| R3 | Bare `--mkpipe` must resolve to `.<sanitized-session-name>.mkpipe` in the launch working directory; explicit relative paths resolve against the launch working directory; explicit absolute paths remain absolute. | `MkpipeListener` | `LauncherMkpipeContractDocs` | `orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `mkpipe.Start(mkpipe.Config)`<br>`Listener.Path() string` | `orchestrator/internal/mkpipe/listener_test.go` path-resolution tests for default, relative, and absolute paths. |
| E3 | Session-name sanitization must preserve ASCII letters, digits, `.`, `_`, and `-`; replace other characters with `-`; collapse repeated dashes when practical; and fall back to the backend default basename when sanitization would otherwise be empty. | `MkpipeListener` | None | `orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go` | `sanitizeSessionBasename(sessionName, fallback)` | `orchestrator/internal/mkpipe/listener_test.go` sanitization cases for `"reviewer 1"` and empty-after-sanitization input such as `"///"`. |
| E4 | Mkpipe startup must reject missing parent directories and any preexisting target path, including stale FIFOs from hard crashes; it must not create parent directories or silently remove stale paths. | `MkpipeListener` | `LauncherMkpipeContractDocs` | `orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `mkpipe.Start(mkpipe.Config)`<br>`validateTarget(path)` | `orchestrator/internal/mkpipe/listener_test.go` missing-parent, existing-file, and existing-FIFO cases. |
| R4 | On successful mkpipe-enabled Codex startup, the launcher must wait for backend readiness, then start the listener, then open the tmux session handle, then print exactly one concise pre-attach line with session name and absolute FIFO path, then attach. | `CodexLauncherCLI` | `MkpipeListener` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md` | `run(args, stdin, stdout, stderr, deps)`<br>`deps.startMkpipe(mkpipe.Config)`<br>`deps.openSession(sessionName)`<br>`tmux.TmuxSessionLike.Attach(...)` | `orchestrator/cmd/tmux_codex/main_test.go` startup-sequence test asserting readiness precedes mkpipe start, banner prints once, and attach starts only after the banner. |
| E5 | If Codex mkpipe setup fails after the agent session has started but before attach handoff, the launcher must close the agent/session, close any started listener, exit `1`, and avoid leaving a hidden success path behind. | `CodexLauncherCLI` | `MkpipeListener`<br>`AgentSessionContract` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go` | `agent.Close()`<br>`listener.Close()`<br>`cleanup()` | `orchestrator/cmd/tmux_codex/main_test.go` setup-failure cases for `startMkpipe` error and `openSession` error after listener creation. |
| R5 | On successful mkpipe-enabled Claude startup, the launcher must follow the same readiness-before-listener, open-session-before-banner, one-line status, then attach sequence as Codex. | `ClaudeLauncherCLI` | `MkpipeListener` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `run(args, stdin, stdout, stderr, deps)`<br>`deps.startMkpipe(mkpipe.Config)`<br>`deps.openSession(sessionName)`<br>`tmux.TmuxSessionLike.Attach(...)` | `orchestrator/cmd/tmux_claude/main_test.go` startup-sequence test mirroring Codex coverage. |
| E6 | If Claude mkpipe setup fails after the agent session has started but before attach handoff, the launcher must close the agent/session, close any started listener, exit `1`, and avoid leaving a hidden success path behind. | `ClaudeLauncherCLI` | `MkpipeListener`<br>`AgentSessionContract` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go` | `agent.Close()`<br>`listener.Close()`<br>`cleanup()` | `orchestrator/cmd/tmux_claude/main_test.go` setup-failure cases for `startMkpipe` error and `openSession` error after listener creation. |
| R6 | During mkpipe-enabled Codex attach runs, each normalized message must be forwarded immediately through `agent.SendPrompt` with no idle wait or queue; listener and delivery errors are logged to standard streams, failed messages are dropped, and attach-return plus interrupt cleanup reuse one idempotent listener-close path. | `CodexLauncherCLI` | `AgentSessionContract`<br>`MkpipeListener` | `orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_codex/main_test.go` | `listener.Messages() <-chan string`<br>`listener.Errors() <-chan error`<br>`agent.SendPrompt(prompt)`<br>`deps.signalContext() (context.Context, context.CancelFunc)`<br>`cleanup()` | `orchestrator/cmd/tmux_codex/main_test.go` prompt-forwarding, error-logging, attach-return cleanup, and injected-interrupt cleanup tests. |
| R7 | During mkpipe-enabled Claude attach runs, each normalized message must be forwarded immediately through `agent.SendPrompt` with the same drop-and-log behavior and shared cleanup semantics as Codex. | `ClaudeLauncherCLI` | `AgentSessionContract`<br>`MkpipeListener` | `orchestrator/cmd/tmux_claude/main.go`<br>`orchestrator/cmd/tmux_claude/main_test.go` | `listener.Messages() <-chan string`<br>`listener.Errors() <-chan error`<br>`agent.SendPrompt(prompt)`<br>`deps.signalContext() (context.Context, context.CancelFunc)`<br>`cleanup()` | `orchestrator/cmd/tmux_claude/main_test.go` prompt-forwarding, error-logging, attach-return cleanup, and injected-interrupt cleanup tests. |
| R8 | The FIFO protocol must treat one writer open/write/close cycle as one prompt, trim exactly one trailing newline sequence, ignore whitespace-only payloads, preserve internal newlines, and surface normalized payloads on Go channels. | `MkpipeListener` | `CodexLauncherCLI`<br>`ClaudeLauncherCLI` | `orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go` | `readLoop()`<br>`normalizeMessage(raw)`<br>`Listener.Messages() <-chan string` | `orchestrator/internal/mkpipe/listener_test.go` EOF-delimited delivery, whitespace suppression, and internal-newline preservation cases. |
| E7 | `Listener.Close()` must actively unblock blocked FIFO open/read operations, wait for the goroutine to exit, close channels exactly once, and remove only the FIFO created by that listener. | `MkpipeListener` | None | `orchestrator/internal/mkpipe/listener.go`<br>`orchestrator/internal/mkpipe/listener_test.go` | `Listener.Close() error`<br>`done chan struct{}` | `orchestrator/internal/mkpipe/listener_test.go` close-unblocks-reader, FIFO-removal, and idempotent-double-close cases. |
| R9 | `tmux_codex`’s standalone contract doc must describe mkpipe invocation, path rules, attach requirement, one-line pre-attach output, runtime logging to standard streams, startup/runtime failure split, and exit codes. | `LauncherMkpipeContractDocs` | `CodexLauncherCLI`<br>`MkpipeListener` | `orchestrator/cmd/tmux_codex/CONTRACT.md` | `## Invocation`<br>`## Validation Contract`<br>`## Runtime Model`<br>`## Output Contract`<br>`## Failure Semantics`<br>`## Exit Codes` | Task 5 doc checklist review plus `rg` verification against the required section names; no automated unit test expected. |
| R10 | `tmux_claude`’s standalone contract doc must describe the same mkpipe operator contract as Codex, adjusted for the Claude launcher. | `LauncherMkpipeContractDocs` | `ClaudeLauncherCLI`<br>`MkpipeListener` | `orchestrator/cmd/tmux_claude/CONTRACT.md` | `## Invocation`<br>`## Validation Contract`<br>`## Runtime Model`<br>`## Output Contract`<br>`## Failure Semantics`<br>`## Exit Codes` | Task 5 doc checklist review plus `rg` verification against the required section names; no automated unit test expected. |
| E8 | The launcher docs must explicitly call out unsupported raw `--mkpipe -pipe` spelling, concurrent writers as unsupported, manual typing as best effort only, no headless persistence, and no automatic stale-FIFO cleanup after hard crashes. | `LauncherMkpipeContractDocs` | `CodexLauncherCLI`<br>`ClaudeLauncherCLI` | `orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md` | `## Validation Contract`<br>`## Concurrency and Operator Expectations`<br>`## Failure Semantics` | Task 5 checklist review plus `rg` verification for the unsupported-behavior text. |
| R11 | FIFO prompt delivery must reuse the existing launcher-to-agent send path rather than reopening tmux sessions or introducing direct tmux prompt writes in the launcher layer. | `AgentSessionContract` | `CodexLauncherCLI`<br>`ClaudeLauncherCLI` | `orchestrator/internal/agent/agent.go`<br>`orchestrator/internal/agent/agent_test.go`<br>`orchestrator/cmd/tmux_codex/main.go`<br>`orchestrator/cmd/tmux_claude/main.go` | `agent.Agent.SendPrompt(prompt string) error` | `orchestrator/internal/agent/agent_test.go` interface regression test plus launcher main tests that use fake agents through the shared interface. |

## Component Responsibility Map

- `CodexLauncherCLI`: primary owner for Codex-specific mkpipe argument extraction, usage validation, exit-code mapping, readiness-to-listener sequencing, pre-attach banner output, prompt-forwarder goroutines, and interrupt/attach-return cleanup in `orchestrator/cmd/tmux_codex/main.go`. It collaborates with `MkpipeListener` via `mkpipe.Start(...)` and with `AgentSessionContract` via `agent.SendPrompt(...)`. It does not own FIFO path rules, message normalization, or stale-path validation.
- `ClaudeLauncherCLI`: primary owner for the same launcher responsibilities in `orchestrator/cmd/tmux_claude/main.go`. It mirrors `CodexLauncherCLI` behavior intentionally, rather than extracting a shared launcher runner, because that duplication was chosen during implementation Q&A. It does not own FIFO mechanics or shared agent contracts.
- `MkpipeListener`: primary owner for FIFO path sanitization, default/custom path resolution, target validation, FIFO creation, EOF-delimited reads, payload normalization, channel delivery, blocked-read shutdown, and FIFO removal in `orchestrator/internal/mkpipe/listener.go`. It collaborates with both launchers only through the exported listener handle. It does not own attach sequencing, `stderr` logging policy, exit-code mapping, or agent/session cleanup.
- `AgentSessionContract`: primary owner for the launcher-facing prompt-send capability exposed through `agent.Agent`. It keeps both launchers agent-oriented and prevents direct tmux writes from leaking into the command packages. It does not own FIFO parsing, signal handling, or user-facing docs.
- `LauncherMkpipeContractDocs`: primary owner for the operator-facing mkpipe contract in `orchestrator/cmd/tmux_codex/CONTRACT.md` and `orchestrator/cmd/tmux_claude/CONTRACT.md`, including the unsupported-behavior warnings. It collaborates with the code owners by mirroring the final implementation contract. It does not own runtime behavior.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `CodexLauncherCLI` | `MkpipeListener` | `mkpipe.Start(mkpipe.Config{WorkingDir, SessionName, DefaultBasename, RequestedPath}) (mkpipe.Listener, error)` | Called only after `agent.WaitUntilReady()` succeeds. `WorkingDir == ""` lets `internal/mkpipe` resolve the real process working directory. Any error here is a runtime startup failure that must trigger `agent.Close()` and exit `1`. |
| `ClaudeLauncherCLI` | `MkpipeListener` | `mkpipe.Start(mkpipe.Config{WorkingDir, SessionName, DefaultBasename, RequestedPath}) (mkpipe.Listener, error)` | Same lifecycle and failure mapping as Codex. |
| `MkpipeListener` | `CodexLauncherCLI` | `Path() string`, `Messages() <-chan string`, `Errors() <-chan error`, `Close() error` | `Path()` must already be absolute. `Messages()` emits normalized prompts only. `Errors()` emits runtime listener errors that should be logged and ignored. `Close()` must be idempotent and wait for goroutine shutdown before removing the FIFO. |
| `MkpipeListener` | `ClaudeLauncherCLI` | `Path() string`, `Messages() <-chan string`, `Errors() <-chan error`, `Close() error` | Same contract and teardown semantics as Codex. |
| `CodexLauncherCLI` | `AgentSessionContract` | `agent.SendPrompt(prompt string) error` | Called immediately for each prompt received from `listener.Messages()`. The launcher must not wait for backend idle state. On error, log to standard streams, drop the failed prompt, and keep consuming later prompts. |
| `ClaudeLauncherCLI` | `AgentSessionContract` | `agent.SendPrompt(prompt string) error` | Same contract and failure handling as Codex. |
| `CodexLauncherCLI` | `OS Signal Boundary` | `deps.signalContext() (context.Context, context.CancelFunc)` | Default implementation uses `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. Tests inject a cancellable context so interrupt cleanup can be verified without sending a real signal. |
| `ClaudeLauncherCLI` | `OS Signal Boundary` | `deps.signalContext() (context.Context, context.CancelFunc)` | Same contract and testability requirement as Codex. |
| `CodexLauncherCLI` | `tmux.TmuxSessionLike` | `deps.openSession(agent.SessionName())`, then `Attach(stdin, stdout, stderr) error` | Acquire the attach session handle after the listener is running but before the pre-attach banner is printed. Any `openSession` failure after listener creation must call `cleanup()` and `agent.Close()`. |
| `ClaudeLauncherCLI` | `tmux.TmuxSessionLike` | `deps.openSession(agent.SessionName())`, then `Attach(stdin, stdout, stderr) error` | Same sequencing and cleanup contract as Codex. |

## File Ownership Map

- Modify `orchestrator/internal/agent/agent.go` - owned by `AgentSessionContract`; adds `SendPrompt(prompt string) error` to the shared launcher-facing interface.
- Modify `orchestrator/internal/agent/agent_test.go` - owned by `AgentSessionContract`; adds interface regression coverage so the widened contract stays explicit.
- Create `orchestrator/internal/mkpipe/listener.go` - owned by `MkpipeListener`; defines `Config`, exported `Listener` handle, path resolution, stale-path validation, FIFO creation/read loop, normalization, and deterministic `Close()`.
- Create `orchestrator/internal/mkpipe/listener_test.go` - owned by `MkpipeListener`; covers path rules, sanitization fallback, EOF-delimited behavior, normalization, and close-unblocks-reader teardown.
- Modify `orchestrator/cmd/tmux_codex/main.go` - owned by `CodexLauncherCLI`; adds local mkpipe argument extraction, a testable signal-context dependency, mkpipe-enabled startup flow, prompt forwarding, and cleanup orchestration.
- Modify `orchestrator/cmd/tmux_codex/main_test.go` - owned by `CodexLauncherCLI`; extends fake agent/session scaffolding, adds a fake mkpipe listener, and covers parser/runtime/interrupt behavior.
- Modify `orchestrator/cmd/tmux_claude/main.go` - owned by `ClaudeLauncherCLI`; mirrors the Codex mkpipe flow with Claude-specific defaults and labels.
- Modify `orchestrator/cmd/tmux_claude/main_test.go` - owned by `ClaudeLauncherCLI`; mirrors Codex mkpipe test coverage with Claude-specific defaults, labels, and interrupt behavior.
- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `LauncherMkpipeContractDocs`; updates the Codex standalone launcher contract to include mkpipe behavior.
- Create `orchestrator/cmd/tmux_claude/CONTRACT.md` - owned by `LauncherMkpipeContractDocs`; adds a full standalone Claude launcher contract with mkpipe behavior.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/agent/agent.go`
- `orchestrator/internal/agent/agent_test.go`
- `orchestrator/internal/mkpipe/listener.go`
- `orchestrator/internal/mkpipe/listener_test.go`
- `orchestrator/cmd/tmux_codex/main.go`
- `orchestrator/cmd/tmux_codex/main_test.go`
- `orchestrator/cmd/tmux_claude/main.go`
- `orchestrator/cmd/tmux_claude/main_test.go`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`

**Incidental-only files:**
- None expected. Do not expand into `orchestrator/internal/agent/tmux/*`, `orchestrator/internal/agent/{codex,claude}.go`, or `orchestrator/go.mod` unless implementation proves the plan itself incomplete.

## Task List

All commands below assume the working directory is `orchestrator/`.

Baseline before feature work: `go test ./...` passes.

### Task 1: AgentSessionContract

**Files:**
- Modify: `orchestrator/internal/agent/agent.go`
- Modify: `orchestrator/internal/agent/agent_test.go`
- Test: `orchestrator/internal/agent/agent_test.go`

**Covers:** `R11`
**Owner:** `AgentSessionContract`
**Why:** The launcher already has concrete agents that can send prompts, but the shared `Agent` interface does not expose that capability. This task makes prompt forwarding available without coupling command packages directly to tmux internals.

- [ ] **Step 1: Write the failing interface regression test**

```go
func TestAgentInterfaceIncludesSendPrompt(t *testing.T) {
	iface := reflect.TypeOf((*Agent)(nil)).Elem()
	method, ok := iface.MethodByName("SendPrompt")
	if !ok {
		t.Fatal("expected Agent interface to expose SendPrompt")
	}
	if method.Type.NumIn() != 2 || method.Type.In(1).Kind() != reflect.String {
		t.Fatalf("unexpected SendPrompt signature: %v", method.Type)
	}
}
```

- [ ] **Step 2: Run the targeted test to verify it fails**

Run: `go test ./internal/agent -run TestAgentInterfaceIncludesSendPrompt -v`
Expected: FAIL with `expected Agent interface to expose SendPrompt`

- [ ] **Step 3: Add `SendPrompt` to the shared interface and pin concrete implementations**

```go
type Agent interface {
	Start() error
	WaitUntilReady() error
	SendPrompt(prompt string) error
	SessionName() string
	Close() error
}

var (
	_ Agent = (*CodexAgent)(nil)
	_ Agent = (*ClaudeAgent)(nil)
)
```

- [ ] **Step 4: Run targeted regression tests**

Run: `go test ./internal/agent -run 'TestAgentInterfaceIncludesSendPrompt|Test(Codex|Claude)AgentStartWaitSendCaptureClose' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "refactor: expose agent prompt sending"
```

### Task 2: MkpipeListener

**Files:**
- Create: `orchestrator/internal/mkpipe/listener.go`
- Create: `orchestrator/internal/mkpipe/listener_test.go`
- Test: `orchestrator/internal/mkpipe/listener_test.go`

**Covers:** `R3`, `E3`, `E4`, `R8`, `E7`
**Owner:** `MkpipeListener`
**Why:** This task creates the shared FIFO abstraction that both launchers depend on. It is the only place that should know about working-directory resolution, session-name sanitization, stale-path validation, EOF-delimited reads, payload normalization, and deterministic teardown.

- [ ] **Step 1: Write failing path-resolution, validation, and listener-behavior tests**

```go
func TestStartResolvesDefaultAndExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "absolute.pipe")

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "default",
			cfg: Config{WorkingDir: dir, SessionName: "reviewer 1", DefaultBasename: "codex"},
			want: filepath.Join(dir, ".reviewer-1.mkpipe"),
		},
		{
			name: "relative",
			cfg: Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: "./custom.pipe"},
			want: filepath.Join(dir, "custom.pipe"),
		},
		{
			name: "absolute",
			cfg: Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: absolute},
			want: absolute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := Start(tt.cfg)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer listener.Close()
			if got := listener.Path(); got != tt.want {
				t.Fatalf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeSessionBasenameFallsBackToDefault(t *testing.T) {
	if got := sanitizeSessionBasename("///", "codex"); got != "codex" {
		t.Fatalf("sanitizeSessionBasename fallback = %q, want %q", got, "codex")
	}
}

func TestStartRejectsMissingParentAndExistingTarget(t *testing.T) {
	dir := t.TempDir()
	existingFile := filepath.Join(dir, "already-there.pipe")
	if err := os.WriteFile(existingFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	existingFIFO := filepath.Join(dir, "stale.pipe")
	if err := syscall.Mkfifo(existingFIFO, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	tests := []Config{
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: filepath.Join(dir, "missing", "child.pipe")},
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: existingFile},
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: existingFIFO},
	}

	for _, cfg := range tests {
		if _, err := Start(cfg); err == nil {
			t.Fatalf("expected Start(%+v) to fail", cfg)
		}
	}
}

func TestListenerNormalizesMessagesPreservesInternalNewlinesAndSuppressesWhitespace(t *testing.T) {
	dir := t.TempDir()
	listener, err := Start(Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer listener.Close()

	go writeFIFO(t, listener.Path(), "line one\nline two\n\n")
	if got := <-listener.Messages(); got != "line one\nline two\n" {
		t.Fatalf("message = %q, want %q", got, "line one\\nline two\\n")
	}

	go writeFIFO(t, listener.Path(), " \t\n")
	select {
	case got := <-listener.Messages():
		t.Fatalf("unexpected whitespace-only message %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCloseUnblocksReaderRemovesFIFOAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	listener, err := Start(Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Let the read loop block waiting for a writer so Close must wake it.
	time.Sleep(50 * time.Millisecond)

	if err := listener.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := os.Stat(listener.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fifo removal, stat err = %v", err)
	}
	select {
	case _, ok := <-listener.Messages():
		if ok {
			t.Fatal("expected Messages channel to be closed")
		}
	default:
		t.Fatal("expected Messages channel to be closed after Close")
	}
	select {
	case _, ok := <-listener.Errors():
		if ok {
			t.Fatal("expected Errors channel to be closed")
		}
	default:
		t.Fatal("expected Errors channel to be closed after Close")
	}
}
```

- [ ] **Step 2: Run the targeted package tests to verify they fail**

Run: `go test ./internal/mkpipe -run 'Test(StartResolvesDefaultAndExplicitPaths|SanitizeSessionBasenameFallsBackToDefault|StartRejectsMissingParentAndExistingTarget|ListenerNormalizesMessagesPreservesInternalNewlinesAndSuppressesWhitespace|CloseUnblocksReaderRemovesFIFOAndIsIdempotent)' -v`
Expected: FAIL because the package does not exist yet

- [ ] **Step 3: Implement the channel-based listener**

```go
type Config struct {
	WorkingDir      string
	SessionName     string
	DefaultBasename string
	RequestedPath   string
}

type Listener interface {
	Path() string
	Messages() <-chan string
	Errors() <-chan error
	Close() error
}

type listener struct {
	path      string
	messages  chan string
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
}

func Start(cfg Config) (Listener, error) {
	// Resolve the absolute path, validate the parent + target,
	// create the FIFO, start the read-loop goroutine, and return the handle.
}

func normalizeMessage(raw string) (string, bool) {
	// Trim exactly one trailing newline sequence and reject whitespace-only payloads.
}
```

Implementation notes for this step:
- Use `syscall.Mkfifo` plus standard library file APIs; no new dependency is needed.
- If `cfg.WorkingDir` is blank, resolve it with `os.Getwd()` inside `Start`.
- Keep normalization inside `internal/mkpipe`; launchers should only see deliverable prompts on `Messages()`.
- `Close()` must wake any blocked FIFO open/read, wait for `done`, remove the FIFO path it created, and stay idempotent on repeated calls. Opening the FIFO writer-side during `Close()` is an acceptable wake-up strategy once shutdown has been marked.

- [ ] **Step 4: Run focused listener tests, then the package**

Run: `go test ./internal/mkpipe -run 'Test(StartResolvesDefaultAndExplicitPaths|SanitizeSessionBasenameFallsBackToDefault|StartRejectsMissingParentAndExistingTarget|ListenerNormalizesMessagesPreservesInternalNewlinesAndSuppressesWhitespace|CloseUnblocksReaderRemovesFIFOAndIsIdempotent)' -v`
Expected: PASS

Run: `go test ./internal/mkpipe -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mkpipe/listener.go internal/mkpipe/listener_test.go
git commit -m "feat: add mkpipe fifo listener"
```

### Task 3: CodexLauncherCLI

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/main.go`
- Modify: `orchestrator/cmd/tmux_codex/main_test.go`
- Test: `orchestrator/cmd/tmux_codex/main_test.go`

**Covers:** `R1`, `E1`, `R4`, `E5`, `R6`, `R11`
**Owner:** `CodexLauncherCLI`
**Why:** This task wires mkpipe into the Codex launcher without introducing a shared runner. It keeps parser logic local, starts mkpipe only after readiness, forwards normalized prompts immediately, maps setup failures to exit `1`, and makes attach/interrupt cleanup explicit and testable.

- [ ] **Step 1: Add failing Codex parser tests**

```go
func TestParseArgsSupportsCodexMkpipeForms(t *testing.T) {
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

func TestParseArgsRejectsCodexMkpipeUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
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
```

- [ ] **Step 2: Run the parser tests to verify they fail**

Run: `go test ./cmd/tmux_codex -run 'Test(ParseArgsSupportsCodexMkpipeForms|ParseArgsRejectsCodexMkpipeUsageErrors)' -v`
Expected: FAIL with missing mkpipe fields or missing usage validation

- [ ] **Step 3: Extend Codex args/deps and implement local mkpipe parsing**

```go
type codexLaunchArgs struct {
	sessionName   string
	attach        bool
	mkpipeEnabled bool
	mkpipePath    string
}

type codexDeps struct {
	newAgent      func(string) agentpkg.Agent
	newLock       func() (dirlock.Locker, error)
	openSession   func(string) (tmux.TmuxSessionLike, error)
	startMkpipe   func(mkpipe.Config) (mkpipe.Listener, error)
	signalContext func() (context.Context, context.CancelFunc)
}

func extractMkpipeArgs(args []string) (cleanArgs []string, mkpipeEnabled bool, mkpipePath string, err error) {
	// Scan raw args once, consume at most one optional mkpipe path, and leave
	// any following flag token in place for normal flag parsing.
}
```

Implementation notes for this step:
- Keep `parseArgs` local to `main.go`.
- Emit the exact usage strings `invalid --mkpipe: requires --attach` and `invalid --mkpipe: may be provided at most once`.
- Default `signalContext` to `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` when the dependency is nil so the interrupt path can be injected in tests.

- [ ] **Step 4: Add failing Codex runtime tests and the fake mkpipe listener**

```go
type fakeMkpipeListener struct {
	path       string
	messages   chan string
	errors     chan error
	closeCalls int
	closed     chan struct{}
}

func newFakeMkpipeListener(path string) *fakeMkpipeListener {
	return &fakeMkpipeListener{
		path:     path,
		messages: make(chan string, 8),
		errors:   make(chan error, 8),
		closed:   make(chan struct{}),
	}
}

func (l *fakeMkpipeListener) Path() string            { return l.path }
func (l *fakeMkpipeListener) Messages() <-chan string { return l.messages }
func (l *fakeMkpipeListener) Errors() <-chan error    { return l.errors }
func (l *fakeMkpipeListener) Close() error {
	l.closeCalls++
	select {
	case <-l.closed:
	default:
		close(l.closed)
		close(l.messages)
		close(l.errors)
	}
	return nil
}

func TestRunCodexMkpipeStartsListenerAfterReadinessAndPrintsStatus(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	var stdout bytes.Buffer
	wantBanner := "Attaching Codex tmux session \"codex-dev\" with mkpipe \"/tmp/.codex-dev.mkpipe\"\n"
	promptDelivered := make(chan struct{}, 1)
	agent.sendHook = func(prompt string) {
		if prompt == "hello from pipe" {
			promptDelivered <- struct{}{}
		}
	}
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		if got := stdout.String(); got != wantBanner {
			t.Fatalf("attach started before banner was printed: got %q want %q", got, wantBanner)
		}
		select {
		case <-promptDelivered:
			return nil
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for prompt delivery")
			return nil
		}
	}
	go func() { listener.messages <- "hello from pipe" }()

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, &stdout, io.Discard, codexDeps{
		newLock: func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(string) agentpkg.Agent { return agent },
		startMkpipe: func(cfg mkpipe.Config) (mkpipe.Listener, error) {
			if !agent.ready {
				t.Fatal("mkpipe started before readiness")
			}
			return listener, nil
		},
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	})

	if exitCode != 0 || session.attachCalls != 1 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d attachCalls=%d closeCalls=%d", exitCode, session.attachCalls, listener.closeCalls)
	}
	if got, want := stdout.String(), wantBanner; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunCodexMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, &stdout, &stderr, codexDeps{
		newLock:     func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:    func(string) agentpkg.Agent { return agent },
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession: func(string) (tmux.TmuxSessionLike, error) { return nil, errors.New("open session failed") },
		signalContext: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
	})

	if exitCode != 1 || !agent.closed || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closed=%v closeCalls=%d", exitCode, agent.closed, listener.closeCalls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no banner on openSession failure, got stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "open session failed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunCodexMkpipeSetupFailureClosesAgent(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, &stderr, codexDeps{
		newLock:  func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(string) agentpkg.Agent { return agent },
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) {
			return nil, errors.New("mkfifo failed")
		},
	})
	if exitCode != 1 || !agent.closed || !strings.Contains(stderr.String(), "mkfifo failed") {
		t.Fatalf("exit=%d closed=%v stderr=%q", exitCode, agent.closed, stderr.String())
	}
}

func TestRunCodexMkpipeLogsErrorsAndCleansUp(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev", sendErrs: []error{errors.New("pane busy"), nil}}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		listener.errors <- errors.New("fifo read failed")
		listener.messages <- "first"
		listener.messages <- "second"
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	var stderr bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, &stderr, codexDeps{
		newLock:       func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:      func(string) agentpkg.Agent { return agent },
		startMkpipe:   func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	})

	if exitCode != 0 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closeCalls=%d", exitCode, listener.closeCalls)
	}
	if !strings.Contains(stderr.String(), "mkpipe delivery failed: pane busy") ||
		!strings.Contains(stderr.String(), "mkpipe listener error: fifo read failed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunCodexMkpipeInterruptUsesSharedCleanup(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	ctx, cancel := context.WithCancel(context.Background())
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		cancel()
		<-listener.closed
		return nil
	}

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, io.Discard, codexDeps{
		newLock:       func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:      func(string) agentpkg.Agent { return agent },
		startMkpipe:   func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return ctx, func() {} },
	})

	if exitCode != 0 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closeCalls=%d", exitCode, listener.closeCalls)
	}
}
```

- Additional test-double changes required for this step:
- Extend `fakeCodexAgent` with `prompts []string`, `sendErrs []error`, and `sendHook func(string)`.
- Implement `SendPrompt(prompt string) error` on `fakeCodexAgent` so it appends to `prompts`, runs `sendHook`, and pops the next queued error from `sendErrs`.
- Extend `fakeCodexTmuxSession` with `attachFn func(io.Reader, io.Writer, io.Writer) error` so attach timing can be controlled in tests.

- [ ] **Step 5: Run the Codex runtime tests to verify they fail**

Run: `go test ./cmd/tmux_codex -run 'Test(RunCodexMkpipeStartsListenerAfterReadinessAndPrintsStatus|RunCodexMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner|RunCodexMkpipeSetupFailureClosesAgent|RunCodexMkpipeLogsErrorsAndCleansUp|RunCodexMkpipeInterruptUsesSharedCleanup)' -v`
Expected: FAIL with missing mkpipe runtime orchestration, missing fake-agent `SendPrompt`, or unmet sequencing assertions

- [ ] **Step 6: Implement mkpipe runtime orchestration in `run`**

```go
signalContext := deps.signalContext
if signalContext == nil {
	signalContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
}

var listener mkpipe.Listener
cleanup := func() {}

if parsed.mkpipeEnabled {
	listener, err = deps.startMkpipe(mkpipe.Config{
		SessionName:     agent.SessionName(),
		DefaultBasename: codexDefaultSessionName,
		RequestedPath:   parsed.mkpipePath,
	})
	if err != nil {
		_ = agent.Close()
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	ctx, stop := signalContext()
	defer stop()

	var cleanupOnce sync.Once
	cleanup = func() {
		cleanupOnce.Do(func() {
			if listener != nil {
				if err := listener.Close(); err != nil {
					fmt.Fprintf(stderr, "mkpipe cleanup failed: %v\n", err)
				}
			}
		})
	}

	go func() {
		<-ctx.Done()
		cleanup()
	}()
	go func() {
		for prompt := range listener.Messages() {
			if err := agent.SendPrompt(prompt); err != nil {
				fmt.Fprintf(stderr, "mkpipe delivery failed: %v\n", err)
			}
		}
	}()
	go func() {
		for err := range listener.Errors() {
			fmt.Fprintf(stderr, "mkpipe listener error: %v\n", err)
		}
	}()
}

session, err := deps.openSession(agent.SessionName())
if err != nil {
	cleanup()
	_ = agent.Close()
	fmt.Fprintln(stderr, err.Error())
	return 1
}

if parsed.mkpipeEnabled {
	fmt.Fprintf(stdout, "Attaching Codex tmux session %q with mkpipe %q\n", agent.SessionName(), listener.Path())
}

err = session.Attach(stdin, stdout, stderr)
cleanup()
if err != nil {
	fmt.Fprintln(stderr, err.Error())
	return 1
}
```

Implementation notes for this step:
- Only create the signal context in mkpipe mode.
- Do not print the pre-attach banner until `openSession` succeeds.
- Reuse the same `cleanup()` closure for `openSession` failure, attach return, and signal-triggered shutdown.
- Keep non-mkpipe behavior unchanged.

- [ ] **Step 7: Run focused Codex tests, then the package**

Run: `go test ./cmd/tmux_codex -run 'Test(ParseArgsSupportsCodexMkpipeForms|ParseArgsRejectsCodexMkpipeUsageErrors|RunCodexMkpipeStartsListenerAfterReadinessAndPrintsStatus|RunCodexMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner|RunCodexMkpipeSetupFailureClosesAgent|RunCodexMkpipeLogsErrorsAndCleansUp|RunCodexMkpipeInterruptUsesSharedCleanup)' -v`
Expected: PASS

Run: `go test ./cmd/tmux_codex -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/tmux_codex/main.go cmd/tmux_codex/main_test.go
git commit -m "feat: add mkpipe support to tmux_codex"
```

### Task 4: ClaudeLauncherCLI

**Files:**
- Modify: `orchestrator/cmd/tmux_claude/main.go`
- Modify: `orchestrator/cmd/tmux_claude/main_test.go`
- Test: `orchestrator/cmd/tmux_claude/main_test.go`

**Covers:** `R2`, `E2`, `R5`, `E6`, `R7`, `R11`
**Owner:** `ClaudeLauncherCLI`
**Why:** This task mirrors the Codex mkpipe integration in the Claude launcher while preserving the existing package-local orchestration pattern. The goal is behavioral parity, not a shared runner.

- [ ] **Step 1: Add failing Claude parser tests**

```go
func TestParseArgsSupportsClaudeMkpipeForms(t *testing.T) {
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

func TestParseArgsRejectsClaudeMkpipeUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
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
```

- [ ] **Step 2: Run the parser tests to verify they fail**

Run: `go test ./cmd/tmux_claude -run 'Test(ParseArgsSupportsClaudeMkpipeForms|ParseArgsRejectsClaudeMkpipeUsageErrors)' -v`
Expected: FAIL with missing mkpipe fields or missing usage validation

- [ ] **Step 3: Extend Claude args/deps and implement local mkpipe parsing**

```go
type claudeLaunchArgs struct {
	sessionName   string
	attach        bool
	mkpipeEnabled bool
	mkpipePath    string
}

type claudeDeps struct {
	newAgent      func(string) agentpkg.Agent
	newLock       func() (dirlock.Locker, error)
	openSession   func(string) (tmux.TmuxSessionLike, error)
	startMkpipe   func(mkpipe.Config) (mkpipe.Listener, error)
	signalContext func() (context.Context, context.CancelFunc)
}
```

Implementation notes for this step:
- Keep the parser duplicated locally instead of extracting shared launcher code.
- Reuse the same validation messages as Codex so the two binaries stay behaviorally aligned.
- Default `signalContext` exactly as in Codex.

- [ ] **Step 4: Add failing Claude runtime tests and the local fake mkpipe listener**

```go
func TestRunClaudeMkpipeStartsListenerAfterReadinessAndPrintsStatus(t *testing.T) {
	// Copy the Codex runtime test shape exactly, but assert both:
	// 1. stdout equals `Attaching Claude tmux session "claude-dev" with mkpipe "/tmp/.claude-dev.mkpipe"\n`
	// 2. `attachFn` sees that banner already written before attach proceeds.
}

func TestRunClaudeMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner(t *testing.T) {
	// Same shape as the Codex openSession-failure test, but using fakeClaudeAgent.
}

func TestRunClaudeMkpipeSetupFailureClosesAgent(t *testing.T) {
	// Same body as Codex, but using fakeClaudeAgent.
}

func TestRunClaudeMkpipeLogsErrorsAndCleansUp(t *testing.T) {
	// Same body as Codex, but using fakeClaudeAgent and fakeClaudeTmuxSession.
}

func TestRunClaudeMkpipeInterruptUsesSharedCleanup(t *testing.T) {
	// Same body as Codex, but using fakeClaudeAgent and fakeClaudeTmuxSession.
}
```

- Additional test-double changes required for this step:
- Add `SendPrompt`, `sendErrs`, `sendHook`, and `prompts` to `fakeClaudeAgent`, mirroring `fakeCodexAgent`.
- Add `attachFn` to `fakeClaudeTmuxSession`, mirroring `fakeCodexTmuxSession`.
- Duplicate `fakeMkpipeListener` locally in the Claude test file because the two launcher packages do not share test helpers.

- [ ] **Step 5: Run the Claude runtime tests to verify they fail**

Run: `go test ./cmd/tmux_claude -run 'Test(RunClaudeMkpipeStartsListenerAfterReadinessAndPrintsStatus|RunClaudeMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner|RunClaudeMkpipeSetupFailureClosesAgent|RunClaudeMkpipeLogsErrorsAndCleansUp|RunClaudeMkpipeInterruptUsesSharedCleanup)' -v`
Expected: FAIL with missing mkpipe runtime orchestration, missing fake-agent `SendPrompt`, or unmet sequencing assertions

- [ ] **Step 6: Implement mkpipe runtime orchestration in Claude `run`**

```go
// Apply the same runtime structure as Codex:
// 1. default `signalContext` when nil
// 2. create `cleanup()` only in mkpipe mode
// 3. start mkpipe after readiness
// 4. open the tmux session handle before printing the banner
// 5. start message + error goroutines
// 6. call `cleanup()` on openSession failure, attach return, and signal cancellation
// 7. print `Attaching Claude tmux session ... with mkpipe ...`
```

Implementation notes for this step:
- Match the Codex lifecycle exactly: prompt-forward goroutine, error-log goroutine, idempotent cleanup, and unchanged non-mkpipe attach behavior.
- Do not extract a shared launcher runner in this task.

- [ ] **Step 7: Run focused Claude tests, then the package**

Run: `go test ./cmd/tmux_claude -run 'Test(ParseArgsSupportsClaudeMkpipeForms|ParseArgsRejectsClaudeMkpipeUsageErrors|RunClaudeMkpipeStartsListenerAfterReadinessAndPrintsStatus|RunClaudeMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner|RunClaudeMkpipeSetupFailureClosesAgent|RunClaudeMkpipeLogsErrorsAndCleansUp|RunClaudeMkpipeInterruptUsesSharedCleanup)' -v`
Expected: PASS

Run: `go test ./cmd/tmux_claude -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/tmux_claude/main.go cmd/tmux_claude/main_test.go
git commit -m "feat: add mkpipe support to tmux_claude"
```

### Task 5: LauncherMkpipeContractDocs

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Create: `orchestrator/cmd/tmux_claude/CONTRACT.md`
- Test: doc review plus full repository tests

**Covers:** `R9`, `R10`, `E8`
**Owner:** `LauncherMkpipeContractDocs`
**Why:** The mkpipe feature changes the operator-facing launcher contract. This task makes the new syntax, path behavior, cleanup expectations, and unsupported-concurrency warnings explicit in both standalone launcher docs.

- [ ] **Step 1: Write the documentation checklist**

```markdown
- Invocation includes `--mkpipe` and `--mkpipe <path>`
- Validation states `--mkpipe` requires `--attach`
- Validation states raw `--mkpipe -pipe` is unsupported; operators must use `./-pipe` or an absolute path
- Default/custom path rules use absolute resolved paths
- Runtime Model explains readiness-before-listener startup, one-line pre-attach output timing, and attach-only lifetime
- Success output includes the one-line pre-attach mkpipe banner
- Runtime logging stays on standard streams
- Exit code `2` is usage-only; other mkpipe startup failures use `1`
- Concurrent writers are unsupported
- Manual typing during pipe-driven sends is best effort only
- Hard-crash stale FIFOs are not removed automatically
- The feature remains attach-only; no headless persistence or detached helper process
```

- [ ] **Step 2: Update the Codex contract and add the full standalone Claude contract**

```markdown
## Invocation

tmux_codex [--session <name>] [--attach] [--mkpipe [<path>]]

## Validation Contract

- `--mkpipe` without `--attach` exits with code `2`
- duplicate `--mkpipe` is a usage error
- raw `--mkpipe -pipe` is unsupported; use `./-pipe` or an absolute path

## Runtime Model

- the launcher waits for backend readiness before creating and starting the FIFO listener
- the launcher opens the tmux attach handle before printing the mkpipe banner
- the mkpipe listener exists only for the lifetime of the attached launcher process; there is no detached helper or headless persistence

## Output Contract

- successful `--attach --mkpipe` launch prints exactly one pre-attach line containing the resolved tmux session name and the absolute FIFO path

## Failure Semantics

- existing paths, missing parent directories, FIFO creation failures, and listener-setup failures exit with code `1`
- runtime listener or delivery failures are logged to standard streams and dropped without terminating the attached session

## Exit Codes

- `0`: success, including successful attach return
- `1`: mkpipe startup or runtime launch failure
- `2`: usage or validation error

## Concurrency and Operator Expectations

- concurrent writers are unsupported
- manual typing while pipe-driven sends are active is best effort only
- stale FIFOs from hard crashes must be removed manually before the next mkpipe launch
```

- [ ] **Step 3: Verify both contract files cover the checklist topics**

Run: `rg -n "mkpipe|--attach|Runtime Model|standard streams|concurrent writers|manual typing|stale FIFOs|headless persistence|Exit Codes|Failure Semantics|Output Contract|Validation Contract" cmd/tmux_codex/CONTRACT.md cmd/tmux_claude/CONTRACT.md`
Expected: Both contract files contain the mkpipe-specific sections and warnings from the checklist; if a topic is missing, fix the doc before moving on.

- [ ] **Step 4: Run the full repository verification**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/tmux_codex/CONTRACT.md cmd/tmux_claude/CONTRACT.md
git commit -m "docs: document mkpipe launcher contract"
```
