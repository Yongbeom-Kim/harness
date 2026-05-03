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

func TestTmuxPaneSendTextUsesLiteralSendKeysWithoutImplicitEnter(t *testing.T) {
	originalRun := runTmuxCommand
	defer func() { runTmuxCommand = originalRun }()

	var commands [][]string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		commands = append(commands, append([]string{name}, args...))
		return commandResult{}, nil
	}

	pane := &TmuxPane{target: "%7", session: &TmuxSession{name: "codex"}}
	if err := pane.SendText("hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	want := []string{"tmux", "send-keys", "-l", "-t", "%7", "--", "hello"}
	if len(commands) != 1 || !slices.Equal(commands[0], want) {
		t.Fatalf("commands = %v, want only send-keys %v", commands, want)
	}
}

func TestTmuxPaneSendTextPreservesLiteralPayloads(t *testing.T) {
	originalRun := runTmuxCommand
	defer func() { runTmuxCommand = originalRun }()

	var got []string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		got = append([]string{name}, args...)
		return commandResult{}, nil
	}

	text := "--leading-dash\nsecond line"
	pane := &TmuxPane{target: "%7"}
	if err := pane.SendText(text); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	want := []string{"tmux", "send-keys", "-l", "-t", "%7", "--", text}
	if !slices.Equal(got, want) {
		t.Fatalf("command = %v, want %v", got, want)
	}
}

func TestTmuxPaneSendTextChunksLargePayloadsOnRuneBoundaries(t *testing.T) {
	originalRun := runTmuxCommand
	defer func() { runTmuxCommand = originalRun }()

	var commands [][]string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		commands = append(commands, append([]string{name}, args...))
		return commandResult{}, nil
	}

	text := strings.Repeat("a", maxSendKeysChunkBytes-1) + "é" + strings.Repeat("b", maxSendKeysChunkBytes-2)
	pane := &TmuxPane{target: "%7"}
	if err := pane.SendText(text); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}

	var combined strings.Builder
	for _, cmd := range commands {
		if len(cmd) != 7 {
			t.Fatalf("command = %v, want 7 args", cmd)
		}
		wantPrefix := []string{"tmux", "send-keys", "-l", "-t", "%7", "--"}
		if !slices.Equal(cmd[:6], wantPrefix) {
			t.Fatalf("command prefix = %v, want %v", cmd[:6], wantPrefix)
		}
		if len(cmd[6]) > maxSendKeysChunkBytes {
			t.Fatalf("chunk length = %d, want <= %d", len(cmd[6]), maxSendKeysChunkBytes)
		}
		combined.WriteString(cmd[6])
	}
	if combined.String() != text {
		t.Fatalf("combined payload = %q, want original text", combined.String())
	}
}

func TestTmuxPanePressKeyWaitsOnceAfterSendText(t *testing.T) {
	originalRun := runTmuxCommand
	originalSleep := sleepBeforePressKey
	defer func() {
		runTmuxCommand = originalRun
		sleepBeforePressKey = originalSleep
	}()

	var events []string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		switch {
		case len(args) >= 2 && args[0] == "send-keys" && args[1] == "-l":
			events = append(events, "text")
		case len(args) >= 1 && args[0] == "send-keys":
			events = append(events, "key:"+args[len(args)-1])
		default:
			events = append(events, "other")
		}
		return commandResult{}, nil
	}
	sleepBeforePressKey = func(d time.Duration) {
		events = append(events, "sleep:"+d.String())
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

	want := []string{"text", "sleep:25ms", "key:Tab", "key:Enter"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestTmuxPanePressKeyUsesSendKeys(t *testing.T) {
	original := runTmuxCommand
	defer func() { runTmuxCommand = original }()

	var got []string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		got = append([]string{name}, args...)
		return commandResult{}, nil
	}

	pane := &TmuxPane{target: "%7"}
	if err := pane.PressKey("Tab"); err != nil {
		t.Fatalf("PressKey() error = %v", err)
	}

	want := []string{"tmux", "send-keys", "-t", "%7", "Tab"}
	if !slices.Equal(got, want) {
		t.Fatalf("command = %v, want %v", got, want)
	}
}

func TestTmuxPanePressKeyPassesArbitraryKeyThrough(t *testing.T) {
	original := runTmuxCommand
	defer func() { runTmuxCommand = original }()

	var got []string
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		got = append([]string{name}, args...)
		return commandResult{}, nil
	}

	pane := &TmuxPane{target: "%7"}
	if err := pane.PressKey("C-c"); err != nil {
		t.Fatalf("PressKey() error = %v", err)
	}

	want := []string{"tmux", "send-keys", "-t", "%7", "C-c"}
	if !slices.Equal(got, want) {
		t.Fatalf("command = %v, want %v", got, want)
	}
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
