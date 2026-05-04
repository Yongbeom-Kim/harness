package tmux

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTmuxCommandErrorsIncludeOperationName(t *testing.T) {
	err := &NewSessionError{SessionName: "taken", Err: errors.New("duplicate")}
	if !strings.Contains(err.Error(), "new-session") || !strings.Contains(err.Error(), `session "taken"`) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestOpenTmuxSessionRejectsEmptyName(t *testing.T) {
	_, err := OpenTmuxSession("")
	if err == nil {
		t.Fatal("expected empty session name error")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTmuxSessionImplementsInterfaces(t *testing.T) {
	var _ TmuxSessionLike = (*TmuxSession)(nil)
	var _ TmuxPaneLike = (*TmuxPane)(nil)
}

type loadBufferCall struct {
	input string
	cmd   []string
}

func TestTmuxPaneSendTextPastesBufferWithoutImplicitEnter(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7", session: &TmuxSession{name: "codex"}}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPaneSendTextPreservesLiteralPayloads(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		text := "--leading-dash\nsecond line"
		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText(text); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, text)
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPaneSendTextLoadsLargePayloadWithoutChunking(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		text := strings.Repeat("a", 4095) + "é" + strings.Repeat("b", 4094)
		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText(text); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, text)
		if len(commands) != 4 {
			t.Fatalf("commands = %d, want 4", len(commands))
		}
		if !slices.Equal(commands[0], displayMessageCommand("%7")) {
			t.Fatalf("first command = %v, want display-message", commands[0])
		}
		if !slices.Equal(commands[1], capturePaneCommand("%7")) {
			t.Fatalf("second command = %v, want capture-pane", commands[1])
		}
		if !slices.Equal(commands[2], loadBufferCommand(bufferName)) {
			t.Fatalf("third command = %v, want load-buffer", commands[2])
		}
		if !slices.Equal(commands[3], pasteBufferCommand(bufferName, "%7")) {
			t.Fatalf("fourth command = %v, want paste-buffer", commands[3])
		}
	})
}

func TestTmuxPaneSendTextRecoversDisabledInputBeforeSending(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		sleepForInteractiveRecovery = func(time.Duration) {}
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				if countCommandPrefix(commands, []string{"tmux", "display-message"}) == 1 {
					return commandResult{stdout: paneStateString(true, false, "", false, "bash")}, nil
				}
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isSelectPaneEnableCommand(cmd):
				return commandResult{}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			selectPaneEnableCommand("%7"),
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPaneSendTextExitsCopyModeBeforeSending(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		sleepForInteractiveRecovery = func(time.Duration) {}
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				if countCommandPrefix(commands, []string{"tmux", "display-message"}) == 1 {
					return commandResult{stdout: paneStateString(false, true, "copy-mode", false, "bash")}, nil
				}
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCopyModeQuitCommand(cmd):
				return commandResult{}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			copyModeQuitCommand("%7"),
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPaneSendTextFailsAfterBoundedRecoveryRetries(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		sleepForInteractiveRecovery = func(time.Duration) {}
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			t.Fatalf("unexpected input command: %v", cmd)
			return commandResult{}, nil
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: paneStateString(true, false, "", false, "bash")}, nil
			case isSelectPaneEnableCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		err := pane.SendText("hello")
		if err == nil {
			t.Fatal("SendText() error = nil, want non-interactive error")
		}

		var paneErr *NonInteractivePaneError
		if !errors.As(err, &paneErr) {
			t.Fatalf("error = %#v, want *NonInteractivePaneError", err)
		}
		if paneErr.Target != "%7" || paneErr.Operation != "send text" {
			t.Fatalf("error = %#v, want target %%7 and operation send text", paneErr)
		}
		if paneErr.Attempts != maxInteractiveRecoveryAttempts {
			t.Fatalf("Attempts = %d, want %d", paneErr.Attempts, maxInteractiveRecoveryAttempts)
		}
		if !paneErr.State.InputOff || paneErr.State.InMode || paneErr.State.Dead || paneErr.State.CurrentCommand != "bash" {
			t.Fatalf("State = %#v, want input_off snapshot for bash", paneErr.State)
		}
		if !strings.Contains(err.Error(), "pane_input_off=1") || !strings.Contains(err.Error(), `pane_current_command="bash"`) {
			t.Fatalf("error text = %q, want pane snapshot", err.Error())
		}
		if got := countCommandPrefix(commands, []string{"tmux", "display-message"}); got != maxInteractiveRecoveryAttempts {
			t.Fatalf("display-message count = %d, want %d", got, maxInteractiveRecoveryAttempts)
		}
		if got := countCommandPrefix(commands, []string{"tmux", "select-pane", "-e"}); got != maxInteractiveRecoveryAttempts-1 {
			t.Fatalf("select-pane count = %d, want %d", got, maxInteractiveRecoveryAttempts-1)
		}
		if got := len(loadCalls); got != 0 {
			t.Fatalf("load-buffer count = %d, want 0", got)
		}
		if got := countCommandPrefix(commands, []string{"tmux", "paste-buffer"}); got != 0 {
			t.Fatalf("paste-buffer count = %d, want 0", got)
		}
	})
}

func TestTmuxPanePressKeyWaitsOnceAfterSendText(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var events []string
		var loadCalls []loadBufferCall
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				events = append(events, "text")
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls == 1 {
					return commandResult{stdout: "before"}, nil
				}
				return commandResult{stdout: "after"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			case isSendKeysCommand(cmd):
				events = append(events, "key:"+cmd[len(cmd)-1])
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(d time.Duration) {
			events = append(events, "sleep:"+d.String())
		}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		if err := pane.PressKey("Tab"); err != nil {
			t.Fatalf("PressKey(Tab) error = %v", err)
		}
		if err := pane.PressKey("Enter"); err != nil {
			t.Fatalf("PressKey(Enter) error = %v", err)
		}

		assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := []string{"text", "sleep:25ms", "key:Tab", "key:Enter"}
		if !slices.Equal(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
}

func TestTmuxPanePressKeyVerifiesPendingTextBeforeEnter(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls == 1 {
					return commandResult{stdout: "before"}, nil
				}
				return commandResult{stdout: "after"}, nil
			case isPasteBufferCommand(cmd), isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		if err := pane.PressKey("Enter"); err != nil {
			t.Fatalf("PressKey(Enter) error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "Enter"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeyVerifiesPendingTextBeforeTab(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls == 1 {
					return commandResult{stdout: "before"}, nil
				}
				return commandResult{stdout: "after"}, nil
			case isPasteBufferCommand(cmd), isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		if err := pane.PressKey("Tab"); err != nil {
			t.Fatalf("PressKey(Tab) error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "Tab"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeySuppressesSubmitWhenCaptureNeverChanges(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {}
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			case isSendKeysCommand(cmd):
				if cmd[len(cmd)-1] == "Enter" || cmd[len(cmd)-1] == "Tab" {
					t.Fatalf("unexpected submit command: %v", cmd)
				}
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		err := pane.PressKey("Enter")
		if err == nil {
			t.Fatal("PressKey(Enter) error = nil, want delivery verification error")
		}

		var deliveryErr *DeliveryVerificationError
		if !errors.As(err, &deliveryErr) {
			t.Fatalf("error = %#v, want *DeliveryVerificationError", err)
		}
		if deliveryErr.Target != "%7" || deliveryErr.Operation != `press key "Enter"` {
			t.Fatalf("error = %#v, want target %%7 and Enter operation", deliveryErr)
		}
		if deliveryErr.Timeout != deliveryVerificationTimeout {
			t.Fatalf("Timeout = %v, want %v", deliveryErr.Timeout, deliveryVerificationTimeout)
		}
		if !strings.Contains(err.Error(), "pane_input_off=0") || !strings.Contains(err.Error(), `pane_current_command="bash"`) {
			t.Fatalf("error text = %q, want pane snapshot", err.Error())
		}
		assertSingleLoadBufferCall(t, loadCalls, "hello")
		if got := countExactCommand(commands, []string{"tmux", "send-keys", "-t", "%7", "Enter"}); got != 0 {
			t.Fatalf("Enter send count = %d, want 0", got)
		}
	})
}

func TestTmuxPanePressKeySkipsSubmitVerificationForNonSubmitKeys(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls > 1 {
					t.Fatalf("unexpected verification capture: %v", cmd)
				}
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd), isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		if err := pane.PressKey("C-c"); err != nil {
			t.Fatalf("PressKey(C-c) error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "C-c"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeyClearsPendingVerificationAfterNonSubmitKey(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls > 1 {
					t.Fatalf("unexpected verification capture: %v", cmd)
				}
				return commandResult{stdout: "before"}, nil
			case isPasteBufferCommand(cmd), isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		if err := pane.PressKey("C-c"); err != nil {
			t.Fatalf("PressKey(C-c) error = %v", err)
		}
		if err := pane.PressKey("Enter"); err != nil {
			t.Fatalf("PressKey(Enter) error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "C-c"},
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "Enter"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeyRetainsPendingVerificationWhenNonSubmitSendFails(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var loadCalls []loadBufferCall
		var commands [][]string
		var captureCalls int
		runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			loadCalls = append(loadCalls, loadBufferCall{input: input, cmd: cmd})
			switch {
			case isLoadBufferCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isCapturePaneCommand(cmd):
				captureCalls++
				if captureCalls == 1 {
					return commandResult{stdout: "before"}, nil
				}
				return commandResult{stdout: "after"}, nil
			case isPasteBufferCommand(cmd):
				return commandResult{}, nil
			case isSendKeysCommand(cmd):
				if cmd[len(cmd)-1] == "C-c" {
					return commandResult{}, errors.New("send failed")
				}
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}
		sleepBeforePressKey = func(time.Duration) {}
		sleepForDeliveryVerification = func(time.Duration) {
			t.Fatal("unexpected delivery verification sleep")
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.SendText("hello"); err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
		err := pane.PressKey("C-c")
		if err == nil {
			t.Fatal("PressKey(C-c) error = nil, want send-keys error")
		}

		var sendErr *SendKeysError
		if !errors.As(err, &sendErr) {
			t.Fatalf("error = %#v, want *SendKeysError", err)
		}
		if err := pane.PressKey("Enter"); err != nil {
			t.Fatalf("PressKey(Enter) error = %v", err)
		}

		bufferName := assertSingleLoadBufferCall(t, loadCalls, "hello")
		want := [][]string{
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			loadBufferCommand(bufferName),
			pasteBufferCommand(bufferName, "%7"),
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "C-c"},
			displayMessageCommand("%7"),
			capturePaneCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "Enter"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeyUsesSendKeys(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var commands [][]string
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.PressKey("Tab"); err != nil {
			t.Fatalf("PressKey() error = %v", err)
		}

		want := [][]string{
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "Tab"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPanePressKeyPassesArbitraryKeyThrough(t *testing.T) {
	withTmuxTestHooks(t, func() {
		var commands [][]string
		runTmuxCommand = func(name string, args ...string) (commandResult, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			switch {
			case isDisplayMessageCommand(cmd):
				return commandResult{stdout: interactivePaneState("bash")}, nil
			case isSendKeysCommand(cmd):
				return commandResult{}, nil
			default:
				t.Fatalf("unexpected command: %v", cmd)
				return commandResult{}, nil
			}
		}

		pane := &TmuxPane{target: "%7"}
		if err := pane.PressKey("C-c"); err != nil {
			t.Fatalf("PressKey() error = %v", err)
		}

		want := [][]string{
			displayMessageCommand("%7"),
			{"tmux", "send-keys", "-t", "%7", "C-c"},
		}
		if !slices.EqualFunc(commands, want, slices.Equal[[]string]) {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	})
}

func TestTmuxPaneCloseUsesKillPane(t *testing.T) {
	original := runTmuxCommand
	defer func() { runTmuxCommand = original }()

	var got []string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		got = append([]string{name}, args...)
		return commandResult{}, nil
	}

	pane := &TmuxPane{target: "%7"}
	if err := pane.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	want := []string{"tmux", "kill-pane", "-t", "%7"}
	if !slices.Equal(got, want) {
		t.Fatalf("command = %v, want %v", got, want)
	}
}

func withTmuxTestHooks(t *testing.T, fn func()) {
	t.Helper()

	originalRun := runTmuxCommand
	originalRunWithInput := runTmuxCommandWithInput
	originalSleepBeforePressKey := sleepBeforePressKey
	originalSleepForInteractiveRecovery := sleepForInteractiveRecovery
	originalSleepForDeliveryVerification := sleepForDeliveryVerification
	defer func() {
		runTmuxCommand = originalRun
		runTmuxCommandWithInput = originalRunWithInput
		sleepBeforePressKey = originalSleepBeforePressKey
		sleepForInteractiveRecovery = originalSleepForInteractiveRecovery
		sleepForDeliveryVerification = originalSleepForDeliveryVerification
	}()

	fn()
}

func displayMessageCommand(target string) []string {
	return []string{"tmux", "display-message", "-p", "-t", target, paneStateFormat}
}

func capturePaneCommand(target string) []string {
	return []string{"tmux", "capture-pane", "-p", "-J", "-S", captureHistoryStart, "-t", target}
}

func copyModeQuitCommand(target string) []string {
	return []string{"tmux", "copy-mode", "-q", "-t", target}
}

func selectPaneEnableCommand(target string) []string {
	return []string{"tmux", "select-pane", "-e", "-t", target}
}

func loadBufferCommand(bufferName string) []string {
	return []string{"tmux", "load-buffer", "-b", bufferName, "-"}
}

func pasteBufferCommand(bufferName string, target string) []string {
	return []string{"tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", target}
}

func isDisplayMessageCommand(cmd []string) bool {
	return slices.Equal(cmd, displayMessageCommand("%7")) || hasCommandPrefix(cmd, []string{"tmux", "display-message", "-p", "-t"})
}

func isCapturePaneCommand(cmd []string) bool {
	return hasCommandPrefix(cmd, []string{"tmux", "capture-pane", "-p", "-J", "-S", captureHistoryStart, "-t"})
}

func isCopyModeQuitCommand(cmd []string) bool {
	return hasCommandPrefix(cmd, []string{"tmux", "copy-mode", "-q", "-t"})
}

func isSelectPaneEnableCommand(cmd []string) bool {
	return hasCommandPrefix(cmd, []string{"tmux", "select-pane", "-e", "-t"})
}

func isLoadBufferCommand(cmd []string) bool {
	return len(cmd) == 5 && cmd[0] == "tmux" && cmd[1] == "load-buffer" && cmd[2] == "-b" && cmd[4] == "-"
}

func isPasteBufferCommand(cmd []string) bool {
	return hasCommandPrefix(cmd, []string{"tmux", "paste-buffer", "-d", "-p", "-b"})
}

func isSendKeysCommand(cmd []string) bool {
	return hasCommandPrefix(cmd, []string{"tmux", "send-keys", "-t"})
}

func hasCommandPrefix(cmd []string, prefix []string) bool {
	return len(cmd) >= len(prefix) && slices.Equal(cmd[:len(prefix)], prefix)
}

func countCommandPrefix(commands [][]string, prefix []string) int {
	var count int
	for _, cmd := range commands {
		if hasCommandPrefix(cmd, prefix) {
			count++
		}
	}
	return count
}

func countExactCommand(commands [][]string, want []string) int {
	var count int
	for _, cmd := range commands {
		if slices.Equal(cmd, want) {
			count++
		}
	}
	return count
}

func assertSingleLoadBufferCall(t *testing.T, calls []loadBufferCall, wantInput string) string {
	t.Helper()

	if len(calls) != 1 {
		t.Fatalf("loadCalls = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.input != wantInput {
		t.Fatalf("load-buffer input = %q, want %q", call.input, wantInput)
	}
	if !isLoadBufferCommand(call.cmd) {
		t.Fatalf("load-buffer command = %v", call.cmd)
	}
	bufferName := call.cmd[3]
	if bufferName == "" {
		t.Fatal("expected buffer name")
	}
	return bufferName
}

func interactivePaneState(command string) string {
	return paneStateString(false, false, "", false, command)
}

func paneStateString(inputOff bool, inMode bool, mode string, dead bool, command string) string {
	parts := []string{
		boolString(inputOff),
		boolString(inMode),
		mode,
		boolString(dead),
		command,
	}
	return strings.Join(parts, "\t")
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
