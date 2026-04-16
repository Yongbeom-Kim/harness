package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCodexCliToolExtractsSessionIDFromJSONMetadata(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))

		stderr := ""
		if len(calls) == 1 {
			stderr = `{"event":"session.created","session_id":"json-session-123"}`
		}

		return fakeCodexCommand("implementation", stderr, 0)
	}

	tool := NewCodexCliTool(nil)
	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "first prompt"}); err != nil {
		t.Fatalf("first SendMessage returned error: %v", err)
	}

	sessionID := tool.ExistingSessionID()
	if sessionID == nil || *sessionID != "json-session-123" {
		t.Fatalf("expected extracted session ID json-session-123, got %v", sessionID)
	}

	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "second prompt"}); err != nil {
		t.Fatalf("second SendMessage returned error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 codex invocations, got %d", len(calls))
	}
	if strings.Contains(calls[0], "--yolo") {
		t.Fatalf("first call unexpectedly included --yolo: %s", calls[0])
	}
	if strings.Contains(calls[0], " resume ") {
		t.Fatalf("first call unexpectedly resumed a session: %s", calls[0])
	}
	if !strings.Contains(calls[1], "resume json-session-123") {
		t.Fatalf("second call did not resume extracted session: %s", calls[1])
	}
}

func TestCodexCliToolExtractsSessionIDFromSpacedJSONMetadata(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))

		stderr := ""
		if len(calls) == 1 {
			stderr = `{"event":"session.created", "session_id": "json-session-456"}`
		}

		return fakeCodexCommand("implementation", stderr, 0)
	}

	tool := NewCodexCliTool(nil)
	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "first prompt"}); err != nil {
		t.Fatalf("first SendMessage returned error: %v", err)
	}

	sessionID := tool.ExistingSessionID()
	if sessionID == nil || *sessionID != "json-session-456" {
		t.Fatalf("expected extracted session ID json-session-456, got %v", sessionID)
	}

	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "second prompt"}); err != nil {
		t.Fatalf("second SendMessage returned error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 codex invocations, got %d", len(calls))
	}
	if !strings.Contains(calls[1], "resume json-session-456") {
		t.Fatalf("second call did not resume extracted session: %s", calls[1])
	}
}

func TestCodexCliToolUsesLastSessionIDFromMetadata(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))

		stderr := ""
		if len(calls) == 1 {
			stderr = strings.Join([]string{
				`{"event":"session.created","session_id":"stale-session"}`,
				"session id: newer-session",
				`{"event":"session.updated", "session_id": "latest-session"}`,
			}, "\n")
		}

		return fakeCodexCommand("implementation", stderr, 0)
	}

	tool := NewCodexCliTool(nil)
	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "first prompt"}); err != nil {
		t.Fatalf("first SendMessage returned error: %v", err)
	}

	sessionID := tool.ExistingSessionID()
	if sessionID == nil || *sessionID != "latest-session" {
		t.Fatalf("expected extracted session ID latest-session, got %v", sessionID)
	}

	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "second prompt"}); err != nil {
		t.Fatalf("second SendMessage returned error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 codex invocations, got %d", len(calls))
	}
	if !strings.Contains(calls[1], "resume latest-session") {
		t.Fatalf("second call did not resume extracted session: %s", calls[1])
	}
}

func TestCodexCliToolFailsWhenSessionIDMissingOnNewSession(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	execCommand = func(name string, args ...string) *exec.Cmd {
		return fakeCodexCommand("implementation", "metadata without a session identifier", 0)
	}

	tool := NewCodexCliTool(nil)
	stdout, stderr, err := tool.SendMessage(CliToolSendMessageOptions{Message: "prompt"})
	if err == nil {
		t.Fatal("expected missing session ID to return an error")
	}
	if err.Error() != "failed to extract Codex session ID" {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "implementation" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "metadata without a session identifier" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if tool.ExistingSessionID() != nil {
		t.Fatalf("expected no stored session ID, got %v", *tool.ExistingSessionID())
	}
}

func TestCodexCliToolDoesNotStoreSessionIDWhenCommandFails(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	execCommand = func(name string, args ...string) *exec.Cmd {
		return fakeCodexCommand("partial output", `{"event":"session.created","session_id":"json-session-789"}`, 17)
	}

	tool := NewCodexCliTool(nil)
	stdout, stderr, err := tool.SendMessage(CliToolSendMessageOptions{Message: "prompt"})
	if err == nil {
		t.Fatal("expected command failure to return an error")
	}
	if stdout != "partial output" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != `{"event":"session.created","session_id":"json-session-789"}` {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if tool.ExistingSessionID() != nil {
		t.Fatalf("expected no stored session ID after command failure, got %v", *tool.ExistingSessionID())
	}
	if exitErr := new(exec.ExitError); !errors.As(err, &exitErr) {
		t.Fatalf("expected exec.ExitError, got %T", err)
	}
}

func TestCodexCliToolCreateNewSessionFailureDoesNotKeepOldSession(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() {
		execCommand = originalExecCommand
	})

	initialSessionID := "existing-session"
	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))

		switch len(calls) {
		case 1:
			return fakeCodexCommand("implementation", "metadata without a session identifier", 0)
		case 2:
			return fakeCodexCommand("implementation", `{"event":"session.created","session_id":"fresh-session"}`, 0)
		default:
			t.Fatalf("unexpected codex invocation %d", len(calls))
			return nil
		}
	}

	tool := NewCodexCliTool(&initialSessionID)
	_, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "prompt", CreateNewSession: true})
	if err == nil {
		t.Fatal("expected missing session ID to return an error")
	}
	if err.Error() != "failed to extract Codex session ID" {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.ExistingSessionID() != nil {
		t.Fatalf("expected stored session ID to be cleared after failed new-session attempt, got %v", *tool.ExistingSessionID())
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 codex invocation after failed new-session attempt, got %d", len(calls))
	}
	if strings.Contains(calls[0], " resume ") {
		t.Fatalf("create-new-session call unexpectedly resumed a session: %s", calls[0])
	}

	if _, _, err := tool.SendMessage(CliToolSendMessageOptions{Message: "second prompt"}); err != nil {
		t.Fatalf("second SendMessage returned error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 codex invocations after retry, got %d", len(calls))
	}
	if strings.Contains(calls[1], " resume ") {
		t.Fatalf("retry unexpectedly resumed the stale session: %s", calls[1])
	}
	if sessionID := tool.ExistingSessionID(); sessionID == nil || *sessionID != "fresh-session" {
		t.Fatalf("expected retry to store the fresh session ID, got %v", sessionID)
	}
}

func fakeCodexCommand(stdout string, stderr string, exitCode int) *exec.Cmd {
	cmd := exec.Command("sh", "-c", `cat >/dev/null
printf '%s' "$FAKE_STDOUT"
printf '%s' "$FAKE_STDERR" >&2
exit "$FAKE_EXIT_CODE"`)
	cmd.Env = append(os.Environ(),
		"FAKE_STDOUT="+stdout,
		"FAKE_STDERR="+stderr,
		fmt.Sprintf("FAKE_EXIT_CODE=%d", exitCode),
	)
	return cmd
}
