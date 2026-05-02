package tmux

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNewTmuxSessionAndNewPane(t *testing.T) {
	const wantName = "iwr-run-id-implementer"
	logPath := installFakeTmux(t)
	t.Setenv("FAKE_TMUX_SPLIT_WINDOW_STDOUT", "%1\n")

	session, err := NewTmuxSession(wantName)
	if err != nil {
		t.Fatalf("NewTmuxSession: %v", err)
	}
	if session == nil {
		t.Fatal("nil session")
	}
	if session.Name() != wantName {
		t.Fatalf("name: want %q got %q", wantName, session.Name())
	}
	if session.AttachTarget() != wantName {
		t.Fatalf("attach: want %q got %q", wantName, session.AttachTarget())
	}
	pane, err := session.NewPane()
	if err != nil {
		t.Fatalf("NewPane: %v", err)
	}
	if pane == nil {
		t.Fatal("nil pane")
	}
	if pane.Target() == "" {
		t.Fatal("expected non-empty Target()")
	}
	if pp, ok := pane.(*TmuxPane); !ok || pp.session != session {
		t.Fatalf("pane session back-reference mismatch: ok=%v", ok)
	}

	secondPane, err := session.NewPane()
	if err != nil {
		t.Fatalf("second NewPane: %v", err)
	}
	if secondPane == nil {
		t.Fatal("nil second pane")
	}
	if secondPane.Target() != "%1" {
		t.Fatalf("second pane target: want %q got %q", "%1", secondPane.Target())
	}
	logLines := mustReadLines(t, logPath)
	wantLogLines := []string{
		"new-session -d -s " + wantName,
		"split-window -d -P -F #{pane_id} -t " + wantName + ":0.0",
	}
	if slices.Compare(logLines, wantLogLines) != 0 {
		t.Fatalf("log lines: want %v got %v", wantLogLines, logLines)
	}
}

func TestTmuxSessionCloseSucceedsWhenAlreadyGone(t *testing.T) {
	logPath := installFakeTmux(t)
	const absent = "no server running on /tmp/xyz"
	t.Setenv("FAKE_TMUX_HAS_SESSION_RC", "1")
	t.Setenv("FAKE_TMUX_HAS_SESSION_STDERR", absent)

	s := &TmuxSession{
		name:   "iwr-run-id-reviewer",
		target: "iwr-run-id-reviewer:0.0",
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	logLines := mustReadLines(t, logPath)
	wantLogLines := []string{"has-session -t iwr-run-id-reviewer"}
	if slices.Compare(logLines, wantLogLines) != 0 {
		t.Fatalf("log lines: want %v got %v", wantLogLines, logLines)
	}
}

func TestTmuxSessionCloseSucceedsOnKillWhenAbsent(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_KILL_SESSION_RC", "1")
	t.Setenv("FAKE_TMUX_KILL_SESSION_STDERR", "server exited unexpectedly")
	s := &TmuxSession{name: "iwr-s", target: "iwr-s:0.0"}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTmuxSessionCloseFailsOnUnexpected(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_HAS_SESSION_RC", "2")
	t.Setenv("FAKE_TMUX_HAS_SESSION_STDERR", "disk full")
	s := &TmuxSession{name: "iwr-s", target: "iwr-s:0.0"}
	if err := s.Close(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunCommandReturnsTypedAbsentError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_HAS_SESSION_RC", "1")
	t.Setenv("FAKE_TMUX_HAS_SESSION_STDERR", "no server running on /tmp/xyz")

	_, err := runCommand("tmux", "has-session", "-t", "iwr-s")
	if err == nil {
		t.Fatal("expected error")
	}
	var absentErr *TmuxSessionAbsentError
	if !errors.As(err, &absentErr) {
		t.Fatalf("expected TmuxSessionAbsentError, got %T", err)
	}
	if absentErr.Message != "no server running on /tmp/xyz" {
		t.Fatalf("unexpected message: %q", absentErr.Message)
	}
}

func TestTmuxPaneOperatesOnStubbedTmux(t *testing.T) {
	logPath := installFakeTmux(t)
	stdinPath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("FAKE_TMUX_STDIN_FILE", stdinPath)
	t.Setenv("FAKE_TMUX_CAPTURE_PANE_STDOUT", "captured\n")

	p := &TmuxPane{
		target:  "iwr-1:0.0",
		session: &TmuxSession{name: "iwr-1", target: "iwr-1:0.0"},
	}
	if err := p.SendText("hi"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	cap, err := p.Capture()
	if err != nil || cap != "captured\n" {
		t.Fatalf("Capture: %v %q", err, cap)
	}
	if got := mustReadFile(t, stdinPath); got != "hi" {
		t.Fatalf("stdin: want %q got %q", "hi", got)
	}
	logLines := mustReadLines(t, logPath)
	if len(logLines) != 4 {
		t.Fatalf("expected 4 tmux invocations, got %d: %v", len(logLines), logLines)
	}
	if !containsAllSubstrings(logLines, "load-buffer -b ", "paste-buffer -d -p -b ", "send-keys -t iwr-1:0.0 Enter", "capture-pane -p -J -S -32768 -t iwr-1:0.0") {
		t.Fatalf("missing expected invocations: %v", logLines)
	}
}

func TestNewTmuxSessionRejectsEmptyName(t *testing.T) {
	_, err := NewTmuxSession("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestTmuxSessionAttachUsesAttachSession(t *testing.T) {
	logPath := installFakeTmux(t)
	input := strings.NewReader("in")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	s := &TmuxSession{name: "iwr-s"}
	if err := s.Attach(input, &stdout, &stderr); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	logLines := mustReadLines(t, logPath)
	wantLogLines := []string{"attach-session -t iwr-s"}
	if slices.Compare(logLines, wantLogLines) != 0 {
		t.Fatalf("attach log: want %v got %v", wantLogLines, logLines)
	}
}

func TestTmuxSessionAttachWrapsError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_ATTACH_SESSION_RC", "1")

	s := &TmuxSession{name: "iwr-s"}
	err := s.Attach(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var attachErr *AttachSessionError
	if !errors.As(err, &attachErr) {
		t.Fatalf("expected AttachSessionError, got %T", err)
	}
	if attachErr.Command() != "attach-session" {
		t.Fatalf("unexpected command: %q", attachErr.Command())
	}
}

func TestNewTmuxSessionWrapsCommandError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_NEW_SESSION_RC", "1")
	t.Setenv("FAKE_TMUX_NEW_SESSION_STDERR", "boom")

	_, err := NewTmuxSession("iwr-s")
	if err == nil {
		t.Fatal("expected error")
	}
	var commandErr *NewSessionError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected NewSessionError, got %T", err)
	}
	if commandErr.Command() != "new-session" {
		t.Fatalf("unexpected command: %q", commandErr.Command())
	}
}

func TestNewPaneWrapsSplitWindowError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_SPLIT_WINDOW_RC", "1")
	t.Setenv("FAKE_TMUX_SPLIT_WINDOW_STDERR", "split failed")

	s := &TmuxSession{name: "iwr-s", target: "iwr-s:0.0", defaultPaneReturned: true}
	_, err := s.NewPane()
	if err == nil {
		t.Fatal("expected error")
	}
	var commandErr *SplitWindowError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected SplitWindowError, got %T", err)
	}
	if commandErr.Command() != "split-window" {
		t.Fatalf("unexpected command: %q", commandErr.Command())
	}
}

func TestSendTextWrapsLoadBufferError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_LOAD_BUFFER_RC", "1")
	t.Setenv("FAKE_TMUX_LOAD_BUFFER_STDERR", "load failed")

	p := &TmuxPane{target: "iwr-s:0.0", session: &TmuxSession{name: "iwr-s"}}
	err := p.SendText("hi")
	if err == nil {
		t.Fatal("expected error")
	}
	var commandErr *LoadBufferError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected LoadBufferError, got %T", err)
	}
	if commandErr.Command() != "load-buffer" {
		t.Fatalf("unexpected command: %q", commandErr.Command())
	}
}

func TestCaptureWrapsCapturePaneError(t *testing.T) {
	installFakeTmux(t)
	t.Setenv("FAKE_TMUX_CAPTURE_PANE_RC", "1")
	t.Setenv("FAKE_TMUX_CAPTURE_PANE_STDERR", "capture failed")

	p := &TmuxPane{target: "iwr-s:0.0", session: &TmuxSession{name: "iwr-s"}}
	_, err := p.Capture()
	if err == nil {
		t.Fatal("expected error")
	}
	var commandErr *CapturePaneError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CapturePaneError, got %T", err)
	}
	if commandErr.Command() != "capture-pane" {
		t.Fatalf("unexpected command: %q", commandErr.Command())
	}
}

func installFakeTmux(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tmux")
	logPath := filepath.Join(dir, "tmux.log")
	if err := os.WriteFile(scriptPath, []byte(fakeTmuxScript), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	return logPath
}

func mustReadLines(t *testing.T, path string) []string {
	t.Helper()

	content := mustReadFile(t, path)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func containsAllSubstrings(lines []string, wants ...string) bool {
	for _, want := range wants {
		matched := false
		for _, line := range lines {
			if strings.Contains(line, want) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

const fakeTmuxScript = `#!/bin/sh
set -eu

if [ -n "${FAKE_TMUX_LOG:-}" ]; then
	printf '%s\n' "$*" >>"${FAKE_TMUX_LOG}"
fi

emit_and_exit() {
	stdout_value="$1"
	stderr_value="$2"
	rc="$3"
	if [ -n "$stdout_value" ]; then
		printf '%s' "$stdout_value"
	fi
	if [ -n "$stderr_value" ]; then
		printf '%s' "$stderr_value" >&2
	fi
	exit "$rc"
}

cmd="${1:-}"
case "$cmd" in
	new-session)
		emit_and_exit "${FAKE_TMUX_NEW_SESSION_STDOUT:-}" "${FAKE_TMUX_NEW_SESSION_STDERR:-}" "${FAKE_TMUX_NEW_SESSION_RC:-0}"
		;;
	has-session)
		emit_and_exit "${FAKE_TMUX_HAS_SESSION_STDOUT:-}" "${FAKE_TMUX_HAS_SESSION_STDERR:-}" "${FAKE_TMUX_HAS_SESSION_RC:-0}"
		;;
	kill-session)
		emit_and_exit "${FAKE_TMUX_KILL_SESSION_STDOUT:-}" "${FAKE_TMUX_KILL_SESSION_STDERR:-}" "${FAKE_TMUX_KILL_SESSION_RC:-0}"
		;;
	split-window)
		emit_and_exit "${FAKE_TMUX_SPLIT_WINDOW_STDOUT:-}" "${FAKE_TMUX_SPLIT_WINDOW_STDERR:-}" "${FAKE_TMUX_SPLIT_WINDOW_RC:-0}"
		;;
	load-buffer)
		if [ -n "${FAKE_TMUX_STDIN_FILE:-}" ]; then
			cat >"${FAKE_TMUX_STDIN_FILE}"
		else
			cat >/dev/null
		fi
		emit_and_exit "${FAKE_TMUX_LOAD_BUFFER_STDOUT:-}" "${FAKE_TMUX_LOAD_BUFFER_STDERR:-}" "${FAKE_TMUX_LOAD_BUFFER_RC:-0}"
		;;
	paste-buffer)
		emit_and_exit "${FAKE_TMUX_PASTE_BUFFER_STDOUT:-}" "${FAKE_TMUX_PASTE_BUFFER_STDERR:-}" "${FAKE_TMUX_PASTE_BUFFER_RC:-0}"
		;;
	send-keys)
		emit_and_exit "${FAKE_TMUX_SEND_KEYS_STDOUT:-}" "${FAKE_TMUX_SEND_KEYS_STDERR:-}" "${FAKE_TMUX_SEND_KEYS_RC:-0}"
		;;
	capture-pane)
		emit_and_exit "${FAKE_TMUX_CAPTURE_PANE_STDOUT:-}" "${FAKE_TMUX_CAPTURE_PANE_STDERR:-}" "${FAKE_TMUX_CAPTURE_PANE_RC:-0}"
		;;
	attach-session)
		emit_and_exit "${FAKE_TMUX_ATTACH_SESSION_STDOUT:-}" "${FAKE_TMUX_ATTACH_SESSION_STDERR:-}" "${FAKE_TMUX_ATTACH_SESSION_RC:-0}"
		;;
	*)
		emit_and_exit "" "" 0
		;;
esac
`
