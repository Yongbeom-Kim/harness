# Tmux Input Delivery Guard Implementation Plan

**Goal:** Prevent false-success tmux sends by making pane input delivery fail fast when the target pane is non-interactive and by suppressing submit keys when typed text never becomes visibly delivered.

**Architecture:** Keep the fix inside `orchestrator/internal/agentruntime/tmux` so launch sends, direct prompt sends, and mkpipe prompt sends all inherit the same behavior without changing backend or runtime APIs. Extend `TmuxPane` with private pane-state recovery and pending-delivery verification state, surface new typed tmux errors with pane snapshots, and add one runtime regression test to prove the existing wrapped error boundary still holds.

**Tech Stack:** Go 1.26, standard library `time` helpers, tmux CLI (`display-message`, `copy-mode`, `select-pane`, `send-keys`, `capture-pane`), existing `agentruntime` unit test suites.

---

## Requirement Coverage Matrix

| ID | Requirement / Edge Case | Primary Owner | Collaborators | Files | Interface Points | Planned Tests |
| --- | --- | --- | --- | --- | --- | --- |
| R1 | Before any text or key injection, tmux pane operations must detect whether the pane is currently interactive by reading tmux pane state rather than assuming `send-keys` success implies delivery. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | `(*TmuxPane).SendText(text string) error`<br>`(*TmuxPane).PressKey(key string) error`<br>private pane-state read helper using `tmux display-message -p` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` coverage for state reads that classify interactive vs non-interactive panes before send attempts. |
| R2 | When a pane is non-interactive because input is disabled or the pane is in a tmux mode, the tmux layer must attempt bounded tmux-local recovery (`copy-mode -q`, `select-pane -e`) and retry up to five times before failing. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | private recovery helper invoked by both `SendText` and `PressKey` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` tests for recovery from `pane_input_off=1`, recovery from `pane_in_mode=1`, and failure after the bounded retry budget is exhausted. |
| R3 | `SendText` must keep private pending state inside `TmuxPane` so a later `Enter` or `Tab` can verify that the just-sent text became visibly delivered before the submit key is allowed through. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | private pending baseline capture state on `TmuxPane` set by `SendText`, consumed by `PressKey` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` tests that `SendText` records a baseline and that `PressKey("Enter")` / `PressKey("Tab")` consult pending verification before sending the key. |
| R4 | Submit-time delivery verification must use a bounded capture-poll loop that treats any visible capture change from the pre-send snapshot as sufficient evidence, suppresses `Enter`/`Tab` if no change appears, and never retries the text injection itself. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | private verification helper called from `PressKey` for pending `Enter`/`Tab` sends | `orchestrator/internal/agentruntime/tmux/tmux_test.go` tests that visible capture change permits submit, unchanged capture suppresses submit and returns a typed verification error, and text chunks are not re-sent on verification failure. |
| R5 | Only `Enter` and `Tab` should trigger submit-time verification. Standalone `SendText` and non-submit `PressKey` calls must still use pre-send interactivity checks but must not require mandatory post-send verification. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | `(*TmuxPane).PressKey(key string) error` submit-key branch for `Enter` and `Tab` only | `orchestrator/internal/agentruntime/tmux/tmux_test.go` coverage that `PressKey("C-c")` after `SendText` bypasses submit verification while still going through pane interactivity checks. |
| R6 | Recovery and verification timing must remain private tmux-package defaults with local test seams rather than new runtime/backend/CLI config. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | private retry-count / poll-interval / timeout constants and sleep vars inside `tmux.go` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` uses stubbed sleep seams so recovery and verification tests run deterministically without real waits. |
| R7 | When the pane remains non-interactive or delivery verification fails, the tmux layer must return typed errors that include the pane target, attempted operation, and a pane-state snapshot (`pane_input_off`, `pane_in_mode`, `pane_mode`, `pane_dead`, `pane_current_command`). | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | new typed tmux errors in `errors.go`; private pane-state snapshot formatting in `tmux.go` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` type assertions and error-string assertions for both non-interactive and delivery-verification failures. |
| E1 | `pane_input_off=1` must be recoverable through `select-pane -e` without widening backend/runtime APIs. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | pane-state recovery branch for disabled input | `orchestrator/internal/agentruntime/tmux/tmux_test.go` asserts `select-pane -e` is invoked and the later send succeeds once the pane becomes interactive. |
| E2 | `pane_in_mode=1` must be recoverable through `copy-mode -q` before the send proceeds. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | pane-state recovery branch for pane modes | `orchestrator/internal/agentruntime/tmux/tmux_test.go` asserts `copy-mode -q` is invoked and the later send succeeds once the pane exits mode. |
| E3 | `pane_dead=1` or any pane that remains non-interactive after the retry budget must fail with the new typed non-interactive error rather than silently attempting more sends. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | typed non-interactive error path | `orchestrator/internal/agentruntime/tmux/tmux_test.go` asserts the retry cap, error type, and snapshot contents for unrecoverable panes. |
| E4 | If capture polling never shows a visible change after `SendText`, `PressKey("Enter")` / `PressKey("Tab")` must not emit the submit key. | `TmuxPaneDeliveryGuard` | None | `orchestrator/internal/agentruntime/tmux/tmux.go`<br>`orchestrator/internal/agentruntime/tmux/errors.go`<br>`orchestrator/internal/agentruntime/tmux/tmux_test.go` | verification failure branch in `PressKey` | `orchestrator/internal/agentruntime/tmux/tmux_test.go` asserts no `send-keys Enter`/`Tab` command is issued when verification fails. |
| E5 | The reusable runtime send surface must keep the existing wrapped `ErrorKindCapture` boundary even when tmux now returns more specific typed send failures. | `RuntimeSendErrorEnvelopeRegression` | `TmuxPaneDeliveryGuard` | `orchestrator/internal/agentruntime/runtime_test.go`<br>`orchestrator/internal/agentruntime/runtime.go` | `(*Runtime).SendPromptNow(prompt string) error`<br>`(*Runtime).SendPromptQueued(prompt string) error`<br>`newError(ErrorKindCapture, ...)` | `orchestrator/internal/agentruntime/runtime_test.go` regression asserting a tmux-layer typed send failure is still wrapped as the same runtime capture error kind; `runtime.go` should remain unchanged unless the test proves otherwise. |

## Component Responsibility Map

- `TmuxPaneDeliveryGuard`: primary owner for all new behavior in the tmux layer. It reads pane state, performs bounded tmux-local recovery, tracks pending text-delivery verification state inside `TmuxPane`, decides which follow-up keys (`Enter`, `Tab`) require verification, and returns typed tmux errors with pane snapshots. It collaborates with backend send sequences only through the existing `SendText` then `PressKey` contract. It does not own backend-specific prompt choreography or runtime-level error taxonomy.
- `RuntimeSendErrorEnvelopeRegression`: primary owner for proving the existing runtime error boundary stays intact once tmux returns richer typed failures. It collaborates with `TmuxPaneDeliveryGuard` only by consuming its returned errors through existing runtime send methods. It does not own any new tmux recovery or verification logic.

## Component Interactions and Contracts

| From | To | Contract | Notes |
| --- | --- | --- | --- |
| `backend.sendTextAndKeys(...)` | `TmuxPaneDeliveryGuard` | `SendText(text)` followed by `PressKey("Enter" \| "Tab" \| other)` | Existing backend choreography remains unchanged. The tmux layer must therefore infer "verify before submit" from its own pending state plus the follow-up key value. |
| `TmuxPaneDeliveryGuard` | tmux CLI | `display-message -p`, `copy-mode -q`, `select-pane -e`, `send-keys -l`, `send-keys`, `capture-pane -p -J` | `display-message -p` is the source of truth for pane state. Recovery is limited to tmux-local actions that can safely re-enable interactivity without restarting the session. |
| `TmuxPaneDeliveryGuard` | `TmuxPane` private state | pre-send capture baseline plus "pending verification" flag | `SendText` records the baseline before text injection. The next `Enter` or `Tab` consumes that pending state after successful verification or on terminal failure. A later `SendText` overwrites any stale pending baseline. |
| `TmuxPaneDeliveryGuard` | `TmuxPaneErrorSurface` (same package) | typed non-interactive and delivery-verification errors in `errors.go` | Error values must include target, operation, and pane snapshot so higher layers can report useful diagnostics without knowing tmux internals. |
| `RuntimeSendErrorEnvelopeRegression` | `TmuxPaneDeliveryGuard` | runtime send methods wrap returned tmux errors with `ErrorKindCapture` | The runtime API surface and error kind must stay stable. The regression test should fail if richer tmux errors accidentally escape the existing runtime envelope. |

## File Ownership Map

- Modify `orchestrator/internal/agentruntime/tmux/tmux.go` - owned by `TmuxPaneDeliveryGuard`; add pane-state reads, bounded recovery, pending delivery-verification state, submit-key verification, and private timing/test seams.
- Modify `orchestrator/internal/agentruntime/tmux/errors.go` - owned by `TmuxPaneDeliveryGuard`; add typed non-interactive and delivery-verification errors with pane target, operation, and pane-state snapshot formatting.
- Modify `orchestrator/internal/agentruntime/tmux/tmux_test.go` - owned by `TmuxPaneDeliveryGuard`; add deterministic tests for pane-state classification, auto-recovery, retry exhaustion, submit-key verification, non-submit key behavior, typed errors, and no-reinject-on-failure behavior.
- Modify `orchestrator/internal/agentruntime/runtime_test.go` - owned by `RuntimeSendErrorEnvelopeRegression`; add a regression test showing the new tmux-layer typed errors are still wrapped as `ErrorKindCapture`.
- Modify `orchestrator/internal/agentruntime/runtime.go` only if the new regression test demonstrates a real wrapper gap - owned by `RuntimeSendErrorEnvelopeRegression`; keep any change narrowly limited to preserving the existing wrapped error behavior. The preferred outcome is no production change in this file.

## Implementation File Allowlist

**Primary files:**
- `orchestrator/internal/agentruntime/tmux/tmux.go`
- `orchestrator/internal/agentruntime/tmux/errors.go`
- `orchestrator/internal/agentruntime/tmux/tmux_test.go`
- `orchestrator/internal/agentruntime/runtime_test.go`

**Incidental-only files:**
- `orchestrator/internal/agentruntime/runtime.go` - only if the new runtime regression test proves the existing wrapped capture-error behavior no longer holds.

## Task List

All commands below assume the working directory is `.../.workspace/harness/orchestrator`.

### Task 1: TmuxPaneDeliveryGuard Recovery Tests

**Files:**
- Modify: `orchestrator/internal/agentruntime/tmux/tmux_test.go`

**Covers:** `R1`, `R2`, `E1`, `E2`, `E3`
**Owner:** `TmuxPaneDeliveryGuard`
**Why:** Lock in the failure modes first so the implementation cannot regress back to "tmux accepted the command but nothing happened."

- [ ] **Step 1: Write failing tests for pane-state classification and recovery**

```go
func TestTmuxPaneSendTextRecoversDisabledInputBeforeSending(t *testing.T) {}
func TestTmuxPaneSendTextExitsCopyModeBeforeSending(t *testing.T) {}
func TestTmuxPaneSendTextFailsAfterBoundedRecoveryRetries(t *testing.T) {}
```

- [ ] **Step 2: Stub `runTmuxCommand` so each test drives `display-message`, `copy-mode -q`, `select-pane -e`, and the later `send-keys -l` calls explicitly**

```go
runTmuxCommand = func(name string, args ...string) (commandResult, error) {
	// return non-interactive pane-state snapshots until recovery succeeds
}
```

- [ ] **Step 3: Run the targeted tmux tests and verify they fail against the current implementation**

Run: `go test ./internal/agentruntime/tmux -run 'TestTmuxPaneSendText(RecoversDisabledInputBeforeSending|ExitsCopyModeBeforeSending|FailsAfterBoundedRecoveryRetries)$' -v`

Expected: FAIL because `SendText` currently goes straight to `tmux send-keys -l` without pane-state reads or recovery.

- [ ] **Step 4: Commit the red tests**

```bash
git add internal/agentruntime/tmux/tmux_test.go
git commit -m "test: cover tmux pane recovery before sends"
```

### Task 2: TmuxPaneDeliveryGuard Recovery Implementation

**Files:**
- Modify: `orchestrator/internal/agentruntime/tmux/tmux.go`
- Modify: `orchestrator/internal/agentruntime/tmux/errors.go`
- Modify: `orchestrator/internal/agentruntime/tmux/tmux_test.go`

**Covers:** `R1`, `R2`, `R6`, `R7`, `E1`, `E2`, `E3`
**Owner:** `TmuxPaneDeliveryGuard`
**Why:** This task adds the actual interactivity guard and the typed failure surface without widening any higher-level API.

- [ ] **Step 1: Add typed tmux errors for unrecoverable non-interactive panes**

```go
type NonInteractivePaneError struct {
	Target    string
	Operation string
	State     paneStateSnapshot
	Attempts  int
}
```

- [ ] **Step 2: Add private pane-state snapshot parsing and retry timing defaults in `tmux.go`**

```go
const (
	maxInteractiveRecoveryAttempts = 5
	recoveryRetryDelay             = 10 * time.Millisecond
	deliveryVerificationTimeout    = 200 * time.Millisecond
	deliveryVerificationPollInterval = 10 * time.Millisecond
)

var (
	sleepForInteractiveRecovery = time.Sleep
	sleepForDeliveryVerification = time.Sleep
)
```

- [ ] **Step 3: Implement one shared helper that reads pane state, attempts `copy-mode -q` / `select-pane -e` repairs, and returns the final snapshot or typed error**

```go
func (p *TmuxPane) ensureInteractive(target string, operation string) (paneStateSnapshot, error) {
	// display-message -> repair -> retry up to five times
}
```

- [ ] **Step 4: Call the shared interactivity helper from both `SendText` and `PressKey` before any tmux send command**

```go
func (p *TmuxPane) SendText(text string) error {
	_, err := p.ensureInteractive(target, "send text")
	// existing send-keys -l chunk loop after guard
}
```

- [ ] **Step 5: Re-run the targeted tmux tests and make them pass**

Run: `go test ./internal/agentruntime/tmux -run 'TestTmuxPaneSendText(RecoversDisabledInputBeforeSending|ExitsCopyModeBeforeSending|FailsAfterBoundedRecoveryRetries)$' -v`

Expected: PASS

- [ ] **Step 6: Commit the recovery implementation**

```bash
git add internal/agentruntime/tmux/tmux.go internal/agentruntime/tmux/errors.go internal/agentruntime/tmux/tmux_test.go
git commit -m "fix: guard tmux sends with pane recovery"
```

### Task 3: TmuxPaneDeliveryGuard Submit Verification

**Files:**
- Modify: `orchestrator/internal/agentruntime/tmux/tmux.go`
- Modify: `orchestrator/internal/agentruntime/tmux/errors.go`
- Modify: `orchestrator/internal/agentruntime/tmux/tmux_test.go`

**Covers:** `R3`, `R4`, `R5`, `R7`, `E4`
**Owner:** `TmuxPaneDeliveryGuard`
**Why:** The recovery guard fixes known tmux state loss, but it still needs a second gate that stops `Enter` or `Tab` when the text never became visibly delivered.

- [ ] **Step 1: Write failing tests for pending verification and submit suppression**

```go
func TestTmuxPanePressKeyVerifiesPendingTextBeforeEnter(t *testing.T) {}
func TestTmuxPanePressKeyVerifiesPendingTextBeforeTab(t *testing.T) {}
func TestTmuxPanePressKeySuppressesSubmitWhenCaptureNeverChanges(t *testing.T) {}
func TestTmuxPanePressKeySkipsSubmitVerificationForNonSubmitKeys(t *testing.T) {}
```

- [ ] **Step 2: Make the tests assert that unchanged capture means no `send-keys Enter` or `send-keys Tab` command is issued**

```go
for _, cmd := range commands {
	if slices.Equal(cmd, []string{"tmux", "send-keys", "-t", "%7", "Enter"}) ||
		slices.Equal(cmd, []string{"tmux", "send-keys", "-t", "%7", "Tab"}) {
		t.Fatalf("unexpected submit command: %v", cmd)
	}
}
```

- [ ] **Step 3: Run the verification-focused tmux tests and verify they fail against the recovery-only implementation**

Run: `go test ./internal/agentruntime/tmux -run 'TestTmuxPanePressKey(VerifiesPendingTextBeforeEnter|VerifiesPendingTextBeforeTab|SuppressesSubmitWhenCaptureNeverChanges|SkipsSubmitVerificationForNonSubmitKeys)$' -v`

Expected: FAIL because `PressKey` currently sends keys after only a fixed settle delay.

- [ ] **Step 4: Add private pending baseline state to `TmuxPane` and record the pre-send capture in `SendText` before injecting non-empty text**

```go
type pendingDeliveryCheck struct {
	baseline string
	active   bool
}
```

- [ ] **Step 5: Implement bounded capture polling in `PressKey` for pending `Enter` and `Tab`, return a typed verification error on timeout, and do not re-send text**

```go
func (p *TmuxPane) verifyPendingDelivery(target string, key string) error {
	// poll Capture until capture != baseline or timeout
}
```

- [ ] **Step 6: Re-run the verification tests and the full tmux package suite**

Run: `go test ./internal/agentruntime/tmux -v`

Expected: PASS

- [ ] **Step 7: Commit the submit-verification implementation**

```bash
git add internal/agentruntime/tmux/tmux.go internal/agentruntime/tmux/errors.go internal/agentruntime/tmux/tmux_test.go
git commit -m "fix: verify tmux text delivery before submit keys"
```

### Task 4: RuntimeSendErrorEnvelopeRegression

**Files:**
- Modify: `orchestrator/internal/agentruntime/runtime_test.go`
- Modify (only if required): `orchestrator/internal/agentruntime/runtime.go`

**Covers:** `E5`
**Owner:** `RuntimeSendErrorEnvelopeRegression`
**Why:** The tmux package is gaining richer send failures, but callers of `Runtime.SendPromptNow` / `SendPromptQueued` still expect the current wrapped runtime error boundary.

- [ ] **Step 1: Add a runtime regression test that injects a tmux-layer typed send failure through a fake backend or fake pane**

```go
func TestRuntimeSendPromptWrapsTypedTmuxSendFailures(t *testing.T) {
	// assert *Error with Kind == ErrorKindCapture still wraps the tmux error
}
```

- [ ] **Step 2: Run the targeted runtime test**

Run: `go test ./internal/agentruntime -run 'TestRuntimeSendPromptWrapsTypedTmuxSendFailures$' -v`

Expected: PASS if the existing wrapper already behaves correctly; FAIL only if the new tmux errors accidentally bypass the current runtime envelope.

- [ ] **Step 3: If the test fails, make the smallest possible change in `runtime.go` to preserve the existing wrapped `ErrorKindCapture` behavior**

```go
return newError(ErrorKindCapture, r.SessionName(), "", err)
```

- [ ] **Step 4: Run the focused regression suites**

Run: `go test ./internal/agentruntime/tmux ./internal/agentruntime -v`

Expected: PASS

- [ ] **Step 5: Commit the regression coverage (and any minimal runtime fix if required)**

```bash
git add internal/agentruntime/runtime_test.go internal/agentruntime/runtime.go
git commit -m "test: preserve runtime error wrapping for tmux send failures"
```

### Task 5: Final Verification

**Files:**
- No new files; verification only

**Covers:** `R1`, `R2`, `R3`, `R4`, `R5`, `R6`, `R7`, `E1`, `E2`, `E3`, `E4`, `E5`
**Owner:** `TmuxPaneDeliveryGuard`
**Why:** Prove the fix is isolated to the tmux layer and did not disturb existing backend/runtime behavior.

- [ ] **Step 1: Run the full `agentruntime` package tests**

Run: `go test ./internal/agentruntime/... -v`

Expected: PASS

- [ ] **Step 2: Run the full repository test suite if the branch baseline is green**

Run: `go test ./...`

Expected: PASS

- [ ] **Step 3: Review the diff against the allowlist**

Run: `git diff --stat -- internal/agentruntime/tmux internal/agentruntime/runtime.go internal/agentruntime/runtime_test.go`

Expected: only the allowlisted files changed, unless a justified incidental edit was required.

- [ ] **Step 4: Manual smoke-check note for later execution**

Run after implementation if a real tmux session is available:

```bash
go test ./internal/agentruntime/tmux -run 'TestTmuxPane' -v
```

Then manually reproduce:
- disable pane input with `tmux select-pane -d`
- verify a harness send now recovers or fails with a typed non-interactive error
- enter tmux copy mode
- verify a harness send exits mode or fails with the same typed diagnostic

- [ ] **Step 5: Final commit if any verification follow-up changes were required**

```bash
git add -A
git commit -m "chore: finalize tmux input delivery guard verification"
```
