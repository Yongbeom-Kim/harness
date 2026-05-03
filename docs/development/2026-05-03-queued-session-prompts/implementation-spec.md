# Queued Session Prompts Implementation Plan

**Goal:** Replace the ambiguous prompt-send path with explicit immediate-vs-queued semantics across `agentruntime`, backend adapters, and tmux panes while preserving the current mkpipe lifecycle and operator CLI surface.

**Architecture:** Keep `orchestrator/internal/agentruntime.Runtime` as the semantic switchboard and deliberately preserve the current `Config.Mkpipe` plus `StartInfo.Mkpipe.Path` lifecycle. Move text-plus-key choreography into backend adapters on top of an honest tmux pane contract where `SendText` only pastes and `PressKey` is explicit. Keep command changes narrow: `implement-with-reviewer` uses `SendPromptNow` for seeded bootstrap prompts, and contract docs describe queued mkpipe behavior plus Claude's cooperative emulation limit.

**Tech Stack:** Go 1.26, standard library `sync` / `time`, tmux CLI, existing `agentruntime` / `mkpipe` / command test suites, Markdown contract docs.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Replace the reusable runtime prompt surface with explicit `SendPromptNow` and `SendPromptQueued`, and remove the old `SendPrompt` method rather than preserving it as an alias. | `RuntimePromptSemantics` | `WorkflowBootstrapImmediateSends` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go` | `(*Runtime).SendPromptNow(prompt string) error`<br>`(*Runtime).SendPromptQueued(prompt string) error`<br>`type workflowRuntime interface { SendPromptNow(string) error }` | `orchestrator/internal/agentruntime/runtime_test.go` state/error tests for both methods, plus `orchestrator/cmd/implement-with-reviewer/main_test.go` coverage that bootstrap calls the immediate method only and no live command/runtime interface still depends on `SendPrompt`. |
| R2 | Preserve the current runtime-owned mkpipe lifecycle and `StartInfo.Mkpipe.Path` surface while making runtime-owned mkpipe forwarding call `SendPromptQueued` immediately for each normalized message, with no harness-owned pending queue, retry, reorder, or idle-wait layer. | `RuntimePromptSemantics` | `BackendGestureAdapters` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go` | `Start() (StartInfo, error)`<br>`StartInfo.Mkpipe.Path`<br>`startMkpipeForwarders(listener)`<br>`MkpipeErrors() <-chan error` | `orchestrator/internal/agentruntime/runtime_test.go` coverage that `StartInfo.Mkpipe.Path` stays intact, mkpipe forwarding routes each message directly through `SendPromptQueued` in arrival order without local buffering or retry, and listener/delivery failures still surface via `MkpipeErrors()`. |
| R3 | Make the tmux pane abstraction honest by splitting paste-only text injection from explicit keypresses. | `TmuxPaneIO` | `BackendGestureAdapters` | `orchestrator/internal/agentruntime/tmux/interfaces.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | `TmuxPaneLike.SendText(text string) error`<br>`TmuxPaneLike.PressKey(key string) error`<br>`(*TmuxPane).SendText(text string) error`<br>`(*TmuxPane).PressKey(key string) error` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` tests that `SendText` no longer issues `send-keys`, and `PressKey` sends exactly the requested tmux key. |
| R4 | Make the backend adapter surface explicit by replacing backend-level `SendPrompt` with `SendPromptNow` and `SendPromptQueued`, and keep launch ownership in backend adapters with launch now pasting text and then pressing `Enter`. | `BackendGestureAdapters` | `TmuxPaneIO` | `orchestrator/internal/agentruntime/backend/backend.go`<br>`orchestrator/internal/agentruntime/backend/backend_test.go` | `type Backend interface { ... SendPromptNow(...); SendPromptQueued(...) }`<br>`Backend.Launch(pane, buildLaunchCommand)`<br>`launchCommand(...)` | `orchestrator/internal/agentruntime/backend/backend_test.go` compile-and-sequence coverage that the interface exposes only the explicit send methods and that launch asserts `SendText("launch ...")` followed by `PressKey("Enter")`. |
| R5 | Map Codex and Cursor `Now` versus `Queued` semantics to their native CLI gestures inside backend adapters. | `BackendGestureAdapters` | `TmuxPaneIO` | `orchestrator/internal/agentruntime/backend/codex.go`<br>`orchestrator/internal/agentruntime/backend/cursor.go`<br>`orchestrator/internal/agentruntime/backend/backend_test.go` | `Codex.SendPromptNow(...)`<br>`Codex.SendPromptQueued(...)`<br>`Cursor.SendPromptNow(...)`<br>`Cursor.SendPromptQueued(...)` | `orchestrator/internal/agentruntime/backend/backend_test.go` sequence assertions for Codex `Enter` vs `Tab`, and Cursor single-`Enter` queue vs double-`Enter` immediate send. |
| R6 | Map Claude `Now` versus `Queued` semantics explicitly, with queued delivery implemented as the documented cooperative wrapper rather than true CLI queueing, and keep the limitation comment adjacent to that code path. | `BackendGestureAdapters` | `PromptSendContractDocs` | `orchestrator/internal/agentruntime/backend/claude.go`<br>`orchestrator/internal/agentruntime/backend/backend_test.go` | `Claude.SendPromptNow(...)`<br>`Claude.SendPromptQueued(...)`<br>private Claude queued-wrapper helper/constant in `claude.go` | `orchestrator/internal/agentruntime/backend/backend_test.go` sequence assertions that `SendPromptNow` pastes the raw prompt and presses `Enter`, and that `SendPromptQueued` pastes exactly `Do this after all your pending tasks:\n\n<prompt>` before pressing `Enter`; manual source review in `claude.go` confirms the limitation comment sits beside `SendPromptQueued`. |
| R7 | `implement-with-reviewer` seeded bootstrap prompts must remain immediate and must still wait until both runtimes are started and both resolved mkpipe paths are known. | `WorkflowBootstrapImmediateSends` | `RuntimePromptSemantics` | `orchestrator/cmd/implement-with-reviewer/main.go`<br>`orchestrator/cmd/implement-with-reviewer/main_test.go` | `workflowRuntime.SendPromptNow(prompt string) error`<br>seeded prompt call sites after `StartInfo.Mkpipe.Path` checks | `orchestrator/cmd/implement-with-reviewer/main_test.go` sequencing tests asserting both starts complete, both mkpipe paths exist, then the two seeded prompts call the immediate send method while the workflow fake pane stays conformant with `TmuxPaneLike` via a no-op `PressKey`. |
| R8 | Single-agent launcher product contracts must stay operator-stable while documenting that mkpipe traffic is now queued and backend-specific, with Claude explicitly called out as emulated. | `PromptSendContractDocs` | `RuntimePromptSemantics` | `orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md`<br>`orchestrator/cmd/tmux_cursor/CONTRACT.md` | Contract `## Attach behavior`, `## Output Contract`, and `## Failure Semantics` sections | Manual doc verification with `rg` over the three contract files plus final repo review. |
| R9 | `implement-with-reviewer` contract docs must distinguish immediate seeded bootstrap sends from later queued mkpipe traffic and must document Claude's cooperative limitation. | `PromptSendContractDocs` | `WorkflowBootstrapImmediateSends` | `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | Contract `## Runtime Model` section for `Seeded protocol` and `Mkpipe lifetime` | Manual doc verification with `rg` for `immediate`, `queued`, and `Claude` language in the workflow contract. |
| E1 | Prompt sending before successful startup or after close must keep the current wrapped runtime error shape rather than inventing a new error taxonomy. | `RuntimePromptSemantics` | None | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go` | `SendPromptNow(...)`<br>`SendPromptQueued(...)`<br>`newError(ErrorKindCapture, ...)` | `orchestrator/internal/agentruntime/runtime_test.go` state-table coverage for pre-start, started, and closed behavior of both methods. |
| E2 | The runtime send mutex must serialize the full multi-step `SendText` plus `PressKey` sequence so queued and immediate sends cannot interleave. | `RuntimePromptSemantics` | `BackendGestureAdapters`<br>`TmuxPaneIO` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go` | `sendMu` inside `SendPromptNow(...)` and `SendPromptQueued(...)` | `orchestrator/internal/agentruntime/runtime_test.go` fake-backend concurrency test that blocks mid-send and asserts the second send cannot enter until the first sequence finishes. |
| E3 | `SendText` must no longer append `Enter` or `Tab`; only `PressKey` may submit keys, and `PressKey` must stay generic with no local allowlist. | `TmuxPaneIO` | None | `orchestrator/internal/agentruntime/tmux/interfaces.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | `PressKey(key string) error` generic passthrough | `orchestrator/internal/agentruntime/tmux/tmux_test.go` coverage for `PressKey("Enter")`, `PressKey("Tab")`, and an arbitrary passthrough key such as `PressKey("C-c")`, plus a `SendText` test that fails if `send-keys` is invoked. |
| E4 | Claude queued-send wrapper text must be exact, with no extra quoting, fences, markers, or instruction text beyond the standardized prefix and blank line. | `BackendGestureAdapters` | None | `orchestrator/internal/agentruntime/backend/claude.go`<br>`orchestrator/internal/agentruntime/backend/backend_test.go` | Claude queued-wrapper helper in `claude.go` | `orchestrator/internal/agentruntime/backend/backend_test.go` exact-string assertion over the pasted text. |
| E5 | Single-agent launcher test doubles that satisfy `TmuxPaneLike` must absorb the new `PressKey` method without changing launcher behavior or widening command code scope. | `TmuxPaneIO` | None | `orchestrator/cmd/tmux_codex/main_test.go`<br>`orchestrator/cmd/tmux_claude/main_test.go`<br>`orchestrator/cmd/tmux_cursor/main_test.go` | fake launcher panes implementing `PressKey(key string) error` | Targeted `go test ./cmd/tmux_codex ./cmd/tmux_claude ./cmd/tmux_cursor -v` after the pane-interface change to keep compile-time and attach-flow coverage green. |
| E6 | The mkpipe bootstrap-versus-attached failure boundary must remain unchanged: failures are fatal before attach handoff and logged/dropped after attach begins. | `RuntimePromptSemantics` | `PromptSendContractDocs` | `orchestrator/internal/agentruntime/runtime.go`<br>`orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/cmd/tmux_codex/CONTRACT.md`<br>`orchestrator/cmd/tmux_claude/CONTRACT.md`<br>`orchestrator/cmd/tmux_cursor/CONTRACT.md`<br>`orchestrator/cmd/implement-with-reviewer/CONTRACT.md` | `MkpipeErrors() <-chan error`<br>`startMkpipeForwarders(listener)` | `orchestrator/internal/agentruntime/runtime_test.go` delivery-error surfacing coverage, plus final verification of existing launcher/workflow suites that already enforce pre-attach fatal and post-attach logging behavior. |

## Component Responsibility Map

- `RuntimePromptSemantics`: primary owner for the reusable runtime prompt-send API, the choice between immediate and queued semantics, mkpipe forwarding behavior, preserved `StartInfo.Mkpipe.Path` reporting, wrapped state/error behavior, and the runtime send mutex. It collaborates with backend adapters only through explicit `SendPromptNow` / `SendPromptQueued` methods. It does not own low-level tmux key choreography or operator docs.
- `BackendGestureAdapters`: primary owner for backend-specific launch and prompt-delivery choreography. It decides how each backend translates `Now` and `Queued` semantics into `SendText` plus `PressKey` sequences, including Claude's cooperative wrapper. It does not own mkpipe lifecycle, runtime state enforcement, or command bootstrap sequencing.
- `TmuxPaneIO`: primary owner for direct tmux pane operations. It defines the honest pane boundary where `SendText` pastes only and `PressKey` sends one explicit tmux key. It does not decide whether a given prompt should be immediate or queued.
- `WorkflowBootstrapImmediateSends`: primary owner for the `implement-with-reviewer` bootstrap call sites and command-local runtime interface update. It ensures both seeded prompts remain immediate and that they are not sent until both start infos expose peer mkpipe paths. It does not own the protocol text itself or backend-specific delivery gestures.
- `PromptSendContractDocs`: primary owner for the four product contract docs. It explains queued mkpipe behavior, the unchanged CLI surface, the immediate workflow bootstrap sends, and Claude's lack of native queue support. It does not own runtime or backend code.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `WorkflowBootstrapImmediateSends` | `RuntimePromptSemantics` | `SendPromptNow(prompt string) error` | `implement-with-reviewer` calls this only after both runtimes have started and both `StartInfo.Mkpipe.Path` values are non-empty. There is no queued send from command code in this feature. |
| `RuntimePromptSemantics` | `BackendGestureAdapters` | `SendPromptNow(pane, prompt)` / `SendPromptQueued(pane, prompt)` | Runtime chooses the semantic. Direct bootstrap/orchestration calls use `Now`; runtime-owned mkpipe forwarders always use `Queued`. |
| `RuntimePromptSemantics` | `BackendGestureAdapters` | `Launch(pane, buildLaunchCommand)` | `Runtime.Start()` still delegates backend launch and readiness to backend adapters. The launch contract now requires explicit `PressKey("Enter")` after pasting launch text. |
| `BackendGestureAdapters` | `TmuxPaneIO` | `SendText(text string)` then one or more `PressKey(key string)` calls | Codex: `Enter` for now, `Tab` for queued. Cursor: one `Enter` for queued, two `Enter` presses for now. Claude: `Enter` for now, `Enter` after the cooperative wrapper for queued. |
| `RuntimePromptSemantics` | `TmuxPaneIO` | runtime-level `sendMu` around backend send calls | The mutex boundary must cover the entire backend send sequence so `SendText` and follow-up keypresses never interleave across concurrent sends. |
| `RuntimePromptSemantics` | `mkpipe` listener channels | `startMkpipeForwarders(listener)` forwards `listener.Messages()` into `SendPromptQueued(prompt)` and forwards listener/delivery failures into `MkpipeErrors()` | This preserves the current no-retry, no in-memory-queue, no idle-wait, and no reorder contract while changing only the semantic send target. |
| `BackendGestureAdapters` | `PromptSendContractDocs` | Claude queued-send limitation and backend-specific queue behavior | The code comment in `claude.go` and the contract docs must agree that Claude queued delivery is cooperative emulation, not a native CLI queue. |

## File Ownership Map

- Modify `orchestrator/internal/agentruntime/runtime.go` - owned by `RuntimePromptSemantics`; replace `SendPrompt` with `SendPromptNow` and `SendPromptQueued`, keep `StartInfo` / mkpipe startup unchanged, route mkpipe forwarding through queued sends, and preserve current wrapped error behavior plus runtime send serialization.
- Modify `orchestrator/internal/agentruntime/runtime_test.go` - owned by `RuntimePromptSemantics`; add fake-backend coverage for immediate-vs-queued method selection, preserved error wrapping, preserved `StartInfo.Mkpipe.Path`, queued mkpipe forwarding, and send-mutex serialization.

- Modify `orchestrator/internal/agentruntime/backend/backend.go` - owned by `BackendGestureAdapters`; change the backend interface, keep launch ownership in adapters, and add reusable text-plus-key helpers that backend implementations call.
- Modify `orchestrator/internal/agentruntime/backend/codex.go` - owned by `BackendGestureAdapters`; encode Codex `Enter` immediate send and `Tab` queued send.
- Modify `orchestrator/internal/agentruntime/backend/cursor.go` - owned by `BackendGestureAdapters`; encode Cursor single-`Enter` queued send and double-`Enter` immediate send.
- Modify `orchestrator/internal/agentruntime/backend/claude.go` - owned by `BackendGestureAdapters`; encode Claude immediate send plus the exact cooperative queued wrapper with the explanatory limitation comment.
- Modify `orchestrator/internal/agentruntime/backend/backend_test.go` - owned by `BackendGestureAdapters`; record `SendText` and `PressKey` calls and assert launch plus backend-specific prompt sequences.

- Modify `orchestrator/internal/agentruntime/tmux/interfaces.go` - owned by `TmuxPaneIO`; add `PressKey(key string)` and document that `SendText` is paste-only.
- Modify `orchestrator/internal/agentruntime/tmux/tmux.go` - owned by `TmuxPaneIO`; remove implicit `Enter` from `SendText` and implement `PressKey` via tmux `send-keys`.
- Modify `orchestrator/internal/agentruntime/tmux/tmux_test.go` - owned by `TmuxPaneIO`; verify paste-only `SendText` and generic `PressKey`.

- Modify `orchestrator/cmd/implement-with-reviewer/main.go` - owned by `WorkflowBootstrapImmediateSends`; update the command-local runtime interface to `SendPromptNow` and keep the existing two-stage bootstrap sequence intact.
- Modify `orchestrator/cmd/implement-with-reviewer/main_test.go` - owned by `WorkflowBootstrapImmediateSends`; update the fake runtime and fake pane to the new interfaces and keep the sequencing assertions focused on immediate bootstrap sends after both mkpipe paths exist.

- Modify `orchestrator/cmd/tmux_codex/main_test.go` - owned by `TmuxPaneIO`; add a no-op `PressKey` method to the fake pane so the package still compiles after the pane interface grows.
- Modify `orchestrator/cmd/tmux_claude/main_test.go` - owned by `TmuxPaneIO`; same compile-support fake-pane update as Codex.
- Modify `orchestrator/cmd/tmux_cursor/main_test.go` - owned by `TmuxPaneIO`; same compile-support fake-pane update as Codex.

- Modify `orchestrator/cmd/tmux_codex/CONTRACT.md` - owned by `PromptSendContractDocs`; document queued mkpipe delivery, backend-specific behavior, and Claude's emulation note in the shared launcher contract language.
- Modify `orchestrator/cmd/tmux_claude/CONTRACT.md` - owned by `PromptSendContractDocs`; same contract update for the Claude launcher.
- Modify `orchestrator/cmd/tmux_cursor/CONTRACT.md` - owned by `PromptSendContractDocs`; same contract update for the Cursor launcher.
- Modify `orchestrator/cmd/implement-with-reviewer/CONTRACT.md` - owned by `PromptSendContractDocs`; document immediate seeded prompts, later queued peer traffic, and Claude's cooperative limitation.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/agentruntime/runtime.go`
- `orchestrator/internal/agentruntime/runtime_test.go`
- `orchestrator/internal/agentruntime/backend/backend.go`
- `orchestrator/internal/agentruntime/backend/codex.go`
- `orchestrator/internal/agentruntime/backend/cursor.go`
- `orchestrator/internal/agentruntime/backend/claude.go`
- `orchestrator/internal/agentruntime/backend/backend_test.go`
- `orchestrator/internal/agentruntime/tmux/interfaces.go`
- `orchestrator/internal/agentruntime/tmux/tmux.go`
- `orchestrator/internal/agentruntime/tmux/tmux_test.go`
- `orchestrator/cmd/implement-with-reviewer/main.go`
- `orchestrator/cmd/implement-with-reviewer/main_test.go`
- `orchestrator/cmd/tmux_codex/CONTRACT.md`
- `orchestrator/cmd/tmux_claude/CONTRACT.md`
- `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`

**Incidental-only files:**
- `orchestrator/cmd/tmux_codex/main_test.go` - fake-pane interface conformance only.
- `orchestrator/cmd/tmux_claude/main_test.go` - fake-pane interface conformance only.
- `orchestrator/cmd/tmux_cursor/main_test.go` - fake-pane interface conformance only.

## Task List

All commands below assume the working directory is `.../.workspace/harness/orchestrator`.

Baseline on 2026-05-03:

- `go test ./...` passes before feature work.
- Dirty worktree context already exists outside feature code: `bin/tmux_codex`, `bin/tmux_claude`, and `bin/tmux_cursor` are modified, and the new development-doc folder is untracked. Do not treat those `bin/` changes as part of this feature.

### Task 1: TmuxPaneIO

**Files:**
- Modify: `orchestrator/internal/agentruntime/tmux/interfaces.go`
- Modify: `orchestrator/internal/agentruntime/tmux/tmux.go`
- Test: `orchestrator/internal/agentruntime/tmux/tmux_test.go`
- Test (incidental compile support only): `orchestrator/cmd/tmux_codex/main_test.go`
- Test (incidental compile support only): `orchestrator/cmd/tmux_claude/main_test.go`
- Test (incidental compile support only): `orchestrator/cmd/tmux_cursor/main_test.go`

**Covers:** `R3`, `E3`, `E5`
**Owner:** `TmuxPaneIO`
**Why:** Backend and runtime work cannot be correct until the pane contract stops hiding an implicit `Enter`. This task establishes the honest boundary first.

- [ ] **Step 1: Write the failing tmux tests**

```go
func TestTmuxPaneSendTextPastesWithoutImplicitEnter(t *testing.T) {
	// stub runTmuxCommand / runTmuxCommandWithInput and assert only
	// load-buffer + paste-buffer are invoked for SendText("hello")
}

func TestTmuxPanePressKeyUsesSendKeys(t *testing.T) {
	// assert PressKey("Tab") issues: tmux send-keys -t <target> Tab
}

func TestTmuxPanePressKeyPassesArbitraryKeyThrough(t *testing.T) {
	// assert PressKey("C-c") issues: tmux send-keys -t <target> C-c
}
```

- [ ] **Step 2: Run the tmux package tests to verify the old behavior fails**

Run: `go test ./internal/agentruntime/tmux -run 'TestTmuxPane(SendTextPastesWithoutImplicitEnter|PressKeyUsesSendKeys|PressKeyPassesArbitraryKeyThrough)$' -v`
Expected: FAIL because `SendText` still calls `send-keys Enter` and `PressKey` does not exist yet.

- [ ] **Step 3: Implement the minimal pane-contract change**

```go
type TmuxPaneLike interface {
	SendText(text string) error
	PressKey(key string) error
	Capture() (string, error)
	Close() error
}

func (p *TmuxPane) SendText(text string) error {
	// load buffer, paste buffer, stop there
	return nil
}

func (p *TmuxPane) PressKey(key string) error {
	_, err := runTmuxCommand("tmux", "send-keys", "-t", target, key)
	return err
}

```

- [ ] **Step 4: Update the single-agent launcher fake panes for `PressKey` conformance**

```go
func (p *fakeCodexPane) PressKey(string) error { return nil }
func (p *fakeClaudePane) PressKey(string) error { return nil }
func (p *fakeCursorPane) PressKey(string) error { return nil }
```

- [ ] **Step 5: Run the tmux and single-agent command package suites**

Run: `go test ./internal/agentruntime/tmux ./cmd/tmux_codex ./cmd/tmux_claude ./cmd/tmux_cursor -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentruntime/tmux/interfaces.go internal/agentruntime/tmux/tmux.go internal/agentruntime/tmux/tmux_test.go cmd/tmux_codex/main_test.go cmd/tmux_claude/main_test.go cmd/tmux_cursor/main_test.go
git commit -m "refactor: make tmux prompt keys explicit"
```

### Task 2: BackendGestureAdapters

**Files:**
- Modify: `orchestrator/internal/agentruntime/backend/backend.go`
- Modify: `orchestrator/internal/agentruntime/backend/codex.go`
- Modify: `orchestrator/internal/agentruntime/backend/cursor.go`
- Modify: `orchestrator/internal/agentruntime/backend/claude.go`
- Test: `orchestrator/internal/agentruntime/backend/backend_test.go`

**Covers:** `R4`, `R5`, `R6`, `E4`
**Owner:** `BackendGestureAdapters`
**Why:** The runtime can choose `Now` versus `Queued`, but only the backend adapters know how that choice maps onto each CLI's real key semantics.

- [ ] **Step 1: Write the failing backend tests for launch and prompt sequences**

```go
func TestCodexSendPromptMethodsUseNativeKeys(t *testing.T) {
	// now: SendText("hello"), PressKey("Enter")
	// queued: SendText("hello"), PressKey("Tab")
}

func TestCursorSendPromptMethodsUseEnterSemantics(t *testing.T) {
	// queued: one Enter
	// now: two Enters
}

func TestClaudeSendPromptMethodsUseImmediateAndWrappedQueuedSemantics(t *testing.T) {
	// now: SendText("hello"), PressKey("Enter")
	// queued pasted text == "Do this after all your pending tasks:\n\nhello"
	// queued still ends with PressKey("Enter")
}
```

- [ ] **Step 2: Run the backend package tests to confirm the old interface and helpers fail**

Run: `go test ./internal/agentruntime/backend -v`
Expected: FAIL because `Backend` still exposes only `SendPrompt`, launch does not press `Enter` explicitly, and the pane fake cannot record keypresses yet.

- [ ] **Step 3: Implement the minimal backend-surface change**

```go
type Backend interface {
	DefaultSessionName() string
	Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error
	WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error
	SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error
	SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error
}

func sendTextAndKeys(pane tmux.TmuxPaneLike, text string, keys ...string) error {
	if err := pane.SendText(text); err != nil {
		return err
	}
	for _, key := range keys {
		if err := pane.PressKey(key); err != nil {
			return err
		}
	}
	return nil
}

func launchCommand(...) error {
	return sendTextAndKeys(pane, launchText, "Enter")
}

func (Codex) SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, prompt, "Tab")
}

// Claude Code has no native queued-send gesture; keep this comment next to the
// wrapped send path so callers do not mistake the cooperative wrapper for true queueing.
func (Claude) SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, "Do this after all your pending tasks:\n\n"+prompt, "Enter")
}
```

- [ ] **Step 4: Run the backend package suite and verify the Claude limitation comment placement**

Run: `go test ./internal/agentruntime/backend -v`
Expected: PASS, and `claude.go` keeps the queue-emulation comment adjacent to `SendPromptQueued`.

- [ ] **Step 5: Commit**

```bash
git add internal/agentruntime/backend/backend.go internal/agentruntime/backend/codex.go internal/agentruntime/backend/cursor.go internal/agentruntime/backend/claude.go internal/agentruntime/backend/backend_test.go
git commit -m "feat: add explicit queued prompt backends"
```

### Task 3: RuntimePromptSemantics

**Files:**
- Modify: `orchestrator/internal/agentruntime/runtime.go`
- Test: `orchestrator/internal/agentruntime/runtime_test.go`

**Covers:** `R1`, `R2`, `E1`, `E2`, `E6`
**Owner:** `RuntimePromptSemantics`
**Why:** This task is the semantic center of the feature. It preserves current mkpipe lifecycle shape while making the runtime choose immediate versus queued behavior explicitly.

- [ ] **Step 1: Write the failing runtime tests**

```go
func TestRuntimeSendPromptMethodsKeepStateErrors(t *testing.T) {
	// both SendPromptNow and SendPromptQueued fail before Start()
}

func TestRuntimeMkpipeForwardersUseQueuedSemanticsWithoutLocalBuffering(t *testing.T) {
	// listener.Messages() -> backend.SendPromptQueued(...) immediately, in order
	// StartInfo.Mkpipe.Path remains unchanged and failures still surface via MkpipeErrors()
}

func TestRuntimeSendMutexSerializesFullPromptSequence(t *testing.T) {
	// fake backend blocks mid-send; second send must wait
}
```

- [ ] **Step 2: Run the runtime package tests to confirm the old API fails**

Run: `go test ./internal/agentruntime -v`
Expected: FAIL because `SendPromptNow` / `SendPromptQueued` do not exist and mkpipe forwarding still targets the removed ambiguous path.

- [ ] **Step 3: Implement the minimal runtime change**

```go
func (r *Runtime) SendPromptNow(prompt string) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	if r.state != stateStarted || r.pane == nil {
		return newError(ErrorKindCapture, r.SessionName(), "", fmt.Errorf("runtime has not started"))
	}
	if err := r.backend.SendPromptNow(r.pane, prompt); err != nil {
		return newError(ErrorKindCapture, r.SessionName(), "", err)
	}
	return nil
}

func (r *Runtime) SendPromptQueued(prompt string) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	if r.state != stateStarted || r.pane == nil {
		return newError(ErrorKindCapture, r.SessionName(), "", fmt.Errorf("runtime has not started"))
	}
	if err := r.backend.SendPromptQueued(r.pane, prompt); err != nil {
		return newError(ErrorKindCapture, r.SessionName(), "", err)
	}
	return nil
}

func (r *Runtime) startMkpipeForwarders(listener mkpipe.Listener) {
	// each listener.Messages() value is handed straight to r.SendPromptQueued(prompt)
	// there is no local pending queue, retry loop, reorder step, or idle wait
}
```

- [ ] **Step 4: Run the runtime package suite**

Run: `go test ./internal/agentruntime -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentruntime/runtime.go internal/agentruntime/runtime_test.go
git commit -m "feat: split runtime prompt sending semantics"
```

### Task 4: WorkflowBootstrapImmediateSends

**Files:**
- Modify: `orchestrator/cmd/implement-with-reviewer/main.go`
- Test: `orchestrator/cmd/implement-with-reviewer/main_test.go`

**Covers:** `R7`
**Owner:** `WorkflowBootstrapImmediateSends`
**Why:** The workflow command is the only current direct prompt sender. It must switch to the explicit immediate method without widening the rest of the launcher surface.

- [ ] **Step 1: Write the failing workflow-command test updates**

```go
type workflowRuntime interface {
	SessionName() string
	Start() (agentruntime.StartInfo, error)
	MkpipeErrors() <-chan error
	StopMkpipe() error
	SendPromptNow(string) error
}

func (p *fakeWorkflowPane) PressKey(string) error { return nil }

func TestRunBootstrapsSharedSessionStartsBothRuntimeMkpipesThenSeedsPrompts(t *testing.T) {
	// assert the two seeded prompts are recorded through SendPromptNow
}
```

- [ ] **Step 2: Run the workflow command package tests**

Run: `go test ./cmd/implement-with-reviewer -v`
Expected: FAIL because `implement-with-reviewer` still calls `SendPrompt`, and the workflow runtime interface in `main.go` still exposes the old method.

- [ ] **Step 3: Implement the narrow command changes**

```go
if err := implementerRuntime.SendPromptNow(buildImplementerPrompt(...)); err != nil {
	return 1
}
if err := reviewerRuntime.SendPromptNow(buildReviewerPrompt(...)); err != nil {
	return 1
}

func (p *fakeWorkflowPane) PressKey(string) error { return nil }
```

- [ ] **Step 4: Run the workflow command package suite**

Run: `go test ./cmd/implement-with-reviewer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/implement-with-reviewer/main.go cmd/implement-with-reviewer/main_test.go
git commit -m "refactor: make workflow bootstrap sends explicit"
```

### Task 5: PromptSendContractDocs

**Files:**
- Modify: `orchestrator/cmd/tmux_codex/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_claude/CONTRACT.md`
- Modify: `orchestrator/cmd/tmux_cursor/CONTRACT.md`
- Modify: `orchestrator/cmd/implement-with-reviewer/CONTRACT.md`

**Covers:** `R8`, `R9`
**Owner:** `PromptSendContractDocs`
**Why:** The operator CLI surface does not change, so the contract docs must carry the semantic distinction and the Claude limitation explicitly.

- [ ] **Step 1: Edit the four contract docs**

```md
- mkpipe traffic is queued to the backend rather than sent immediately
- backend behavior differs by CLI
- Claude queued delivery is cooperative emulation via:
  "Do this after all your pending tasks:\n\n<prompt>"
- implement-with-reviewer seeded prompts are immediate; later peer mkpipe traffic is queued
```

- [ ] **Step 2: Verify the new contract language is present**

Run: `rg -n "queued|immediate|pending tasks|Claude" cmd/tmux_codex/CONTRACT.md cmd/tmux_claude/CONTRACT.md cmd/tmux_cursor/CONTRACT.md cmd/implement-with-reviewer/CONTRACT.md`
Expected: matches in all four files describing queued mkpipe behavior and the workflow bootstrap distinction.

- [ ] **Step 3: Run final integrated verification**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/tmux_codex/CONTRACT.md cmd/tmux_claude/CONTRACT.md cmd/tmux_cursor/CONTRACT.md cmd/implement-with-reviewer/CONTRACT.md
git commit -m "docs: clarify queued prompt semantics"
```
