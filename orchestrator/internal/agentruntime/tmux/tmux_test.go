package tmux

import (
	"errors"
	"slices"
	"strings"
	"testing"
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

func TestTmuxPaneSendTextPastesWithoutImplicitEnter(t *testing.T) {
	originalRun := runTmuxCommand
	originalRunWithInput := runTmuxCommandWithInput
	defer func() {
		runTmuxCommand = originalRun
		runTmuxCommandWithInput = originalRunWithInput
	}()

	type inputCall struct {
		input string
		cmd   []string
	}

	var loadCalls []inputCall
	var commands [][]string
	runTmuxCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
		loadCalls = append(loadCalls, inputCall{
			input: input,
			cmd:   append([]string{name}, args...),
		})
		return commandResult{}, nil
	}
	runTmuxCommand = func(name string, args ...string) (commandResult, error) {
		commands = append(commands, append([]string{name}, args...))
		return commandResult{}, nil
	}

	pane := &TmuxPane{target: "%7", session: &TmuxSession{name: "codex"}}
	if err := pane.SendText("hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	if len(loadCalls) != 1 {
		t.Fatalf("loadCalls = %d, want 1", len(loadCalls))
	}
	loadCall := loadCalls[0]
	if loadCall.input != "hello" {
		t.Fatalf("load-buffer input = %q, want hello", loadCall.input)
	}
	if len(loadCall.cmd) != 5 || loadCall.cmd[0] != "tmux" || loadCall.cmd[1] != "load-buffer" || loadCall.cmd[2] != "-b" || loadCall.cmd[4] != "-" {
		t.Fatalf("load-buffer command = %v", loadCall.cmd)
	}
	bufferName := loadCall.cmd[3]
	if bufferName == "" {
		t.Fatal("expected buffer name")
	}

	wantPaste := []string{"tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", "%7"}
	if len(commands) != 1 || !slices.Equal(commands[0], wantPaste) {
		t.Fatalf("commands = %v, want only paste-buffer %v", commands, wantPaste)
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
