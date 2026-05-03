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
