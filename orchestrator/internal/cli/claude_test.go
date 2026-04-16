package cli

import (
	"errors"
	"io"
	"os/exec"
	"testing"
)

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestClaudeCliToolPassesPromptAsDistinctArgument(t *testing.T) {
	originalExecCommand := execCommand
	originalSessionIDReader := claudeSessionIDReader
	t.Cleanup(func() {
		execCommand = originalExecCommand
		claudeSessionIDReader = originalSessionIDReader
	})

	claudeSessionIDReader = io.LimitReader(zeroReader{}, 16)
	prompt := "line one\n$HOME `uname -a` $(whoami) \"quotes\""
	var callName string
	var callArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		callName = name
		callArgs = append([]string(nil), args...)
		return fakeCodexCommand("implementation", "", 0)
	}

	tool := NewClaudeCliTool(nil)
	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: prompt}); err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}

	if callName != "bash" {
		t.Fatalf("expected bash launcher, got %q", callName)
	}
	if len(callArgs) != 8 {
		t.Fatalf("expected 8 argv entries, got %d: %#v", len(callArgs), callArgs)
	}
	if callArgs[0] != "-lc" || callArgs[1] != `. "$HOME/.agentrc" && "$@"` || callArgs[2] != "bash" {
		t.Fatalf("unexpected bash wrapper argv: %#v", callArgs[:3])
	}
	if callArgs[3] != "claude" || callArgs[4] != "-p" || callArgs[5] != "--session-id" {
		t.Fatalf("unexpected claude argv prefix: %#v", callArgs[3:6])
	}
	if callArgs[6] != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("unexpected session id: %q", callArgs[6])
	}
	if callArgs[7] != prompt {
		t.Fatalf("expected prompt to be passed verbatim, got %q", callArgs[7])
	}
	if sessionID := tool.ExistingSessionID(); sessionID == nil || *sessionID != callArgs[6] {
		t.Fatalf("expected successful call to store session ID %q, got %v", callArgs[6], sessionID)
	}
	if callArgs[7] == "" {
		t.Fatal("expected non-empty prompt argument")
	}
	if callArgs[7] != "line one\n$HOME `uname -a` $(whoami) \"quotes\"" {
		t.Fatalf("special characters were not preserved verbatim: %q", callArgs[7])
	}
	if callArgs[7] == callArgs[1] {
		t.Fatal("prompt should not be interpolated into the shell command string")
	}
}

func TestClaudeCliToolSessionIDGenerationFailureReturnsError(t *testing.T) {
	originalExecCommand := execCommand
	originalSessionIDReader := claudeSessionIDReader
	t.Cleanup(func() {
		execCommand = originalExecCommand
		claudeSessionIDReader = originalSessionIDReader
	})

	claudeSessionIDReader = failingReader{}
	called := false
	execCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return nil
	}

	tool := NewClaudeCliTool(nil)
	stdout, stderr, err := tool.SendMessage(CliToolSendMessageOptions{Message: "prompt"})
	if err == nil {
		t.Fatal("expected session ID generation failure to return an error")
	}
	if err.Error() != "failed to generate Claude session ID: entropy unavailable" {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("expected empty output on session ID generation failure, got stdout=%q stderr=%q", stdout, stderr)
	}
	if called {
		t.Fatal("execCommand should not be called when session ID generation fails")
	}
	if tool.ExistingSessionID() != nil {
		t.Fatalf("expected no stored session ID, got %v", *tool.ExistingSessionID())
	}
}

func TestClaudeCliToolCreateNewSessionFailureDoesNotKeepOldSession(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	initialSessionID := "existing-session"
	var calls [][]string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))

		switch len(calls) {
		case 1:
			return fakeCodexCommand("partial output", "launch failed", 17)
		case 2:
			return fakeCodexCommand("implementation", "", 0)
		default:
			t.Fatalf("unexpected claude invocation %d", len(calls))
			return nil
		}
	}

	tool := NewClaudeCliTool(&initialSessionID)
	stdout, stderr, err := tool.SendMessage(CliToolSendMessageOptions{Message: "prompt", CreateNewSession: true})
	if err == nil {
		t.Fatal("expected command failure to return an error")
	}
	if stdout != "partial output" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "launch failed" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if tool.ExistingSessionID() != nil {
		t.Fatalf("expected stored session ID to be cleared after failed new-session attempt, got %v", *tool.ExistingSessionID())
	}
	if exitErr := new(exec.ExitError); !errors.As(err, &exitErr) {
		t.Fatalf("expected exec.ExitError, got %T", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 claude invocation after failed new-session attempt, got %d", len(calls))
	}
	if len(calls[0]) < 7 || calls[0][6] != "--session-id" {
		t.Fatalf("create-new-session call unexpectedly resumed a session: %#v", calls[0])
	}

	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "second prompt"}); err != nil {
		t.Fatalf("second SendMessage returned error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 claude invocations after retry, got %d", len(calls))
	}
	if len(calls[1]) < 7 || calls[1][6] != "--session-id" {
		t.Fatalf("retry unexpectedly resumed the stale session: %#v", calls[1])
	}
	if sessionID := tool.ExistingSessionID(); sessionID == nil || *sessionID != calls[1][7] {
		t.Fatalf("expected retry to store session ID %q, got %v", calls[1][7], sessionID)
	}
	if *tool.ExistingSessionID() == initialSessionID {
		t.Fatalf("expected retry to replace stale session ID %q", initialSessionID)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
