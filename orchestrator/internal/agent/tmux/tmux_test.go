package tmux

import (
	"errors"
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
