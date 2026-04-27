package tmux

import (
	"bytes"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestNewTmuxSessionAndNewPane(t *testing.T) {
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	const wantName = "iwr-run-id-implementer"
	splitCalls := 0
	runCommand = func(name string, args ...string) (commandResult, error) {
		if name == "tmux" && len(args) == 4 && args[0] == "new-session" && args[1] == "-d" && args[2] == "-s" && args[3] == wantName {
			return commandResult{}, nil
		}
		if name == "tmux" && len(args) == 7 && args[0] == "split-window" && args[1] == "-d" && args[2] == "-P" && args[3] == "-F" && args[4] == "#{pane_id}" && args[5] == "-t" && args[6] == wantName+":0.0" {
			splitCalls++
			return commandResult{stdout: "%1\n"}, nil
		}
		t.Fatalf("unexpected command %q %q", name, args)
		return commandResult{}, nil
	}

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
	if splitCalls != 1 {
		t.Fatalf("expected 1 split-window call, got %d", splitCalls)
	}
}

func TestTmuxSessionCloseSucceedsWhenAlreadyGone(t *testing.T) {
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	const absent = "no server running on /tmp/xyz"
	call := 0
	runCommand = func(name string, args ...string) (commandResult, error) {
		if name != "tmux" {
			t.Fatalf("expected tmux, got %q", name)
		}
		call++
		switch args[0] {
		case "has-session":
			return commandResult{}, errors.New("exit status 1: " + absent)
		}
		t.Fatalf("unexpected: %q", args)
		return commandResult{}, nil
	}

	s := &TmuxSession{
		name:   "iwr-run-id-reviewer",
		target: "iwr-run-id-reviewer:0.0",
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if call != 1 {
		t.Fatalf("expected 1 has-session, got %d", call)
	}
}

func TestTmuxSessionCloseSucceedsOnKillWhenAbsent(t *testing.T) {
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(name string, args ...string) (commandResult, error) {
		switch {
		case len(args) >= 1 && args[0] == "has-session":
			return commandResult{}, nil
		case len(args) >= 1 && args[0] == "kill-session":
			return commandResult{}, errors.New("exit status 1: server exited unexpectedly")
		default:
			return commandResult{}, nil
		}
	}
	s := &TmuxSession{name: "iwr-s", target: "iwr-s:0.0"}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTmuxSessionCloseFailsOnUnexpected(t *testing.T) {
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(name string, args ...string) (commandResult, error) {
		if len(args) > 0 && args[0] == "has-session" {
			return commandResult{}, errors.New("exit status 2: disk full")
		}
		return commandResult{}, nil
	}
	s := &TmuxSession{name: "iwr-s", target: "iwr-s:0.0"}
	if err := s.Close(); err == nil {
		t.Fatal("expected error")
	}
}

func TestTmuxPaneOperatesOnStubbedTmux(t *testing.T) {
	orig, origIn := runCommand, runCommandWithInput
	t.Cleanup(func() {
		runCommand, runCommandWithInput = orig, origIn
	})
	var invocations [][]string
	runCommand = func(name string, args ...string) (commandResult, error) {
		invocations = append(invocations, append([]string{name}, args...))
		switch {
		case len(args) == 0:
			return commandResult{}, nil
		case args[0] == "load-buffer" || args[0] == "send-keys" || args[0] == "paste-buffer" || args[0] == "capture-pane":
			if args[0] == "capture-pane" {
				return commandResult{stdout: "captured\n"}, nil
			}
			return commandResult{}, nil
		}
		return commandResult{}, nil
	}
	runCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
		invocations = append(invocations, append([]string{"stdin:" + input, name}, args...))
		return runCommand(name, args...)
	}

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
	if !slicesContainsAny(invocations, "load-buffer", "paste-buffer", "capture-pane", "send-keys") {
		t.Fatalf("missing expected invocations: %v", invocations)
	}
}

func TestNewTmuxSessionRejectsEmptyName(t *testing.T) {
	_, err := NewTmuxSession("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestTmuxSessionAttachUsesAttachSession(t *testing.T) {
	orig := execCommandStdio
	t.Cleanup(func() { execCommandStdio = orig })

	var gotName string
	var gotArgs []string
	input := strings.NewReader("in")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execCommandStdio = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.Command("true")
	}

	s := &TmuxSession{name: "iwr-s"}
	if err := s.Attach(input, &stdout, &stderr); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if gotName != "tmux" {
		t.Fatalf("expected tmux command, got %q", gotName)
	}
	wantArgs := []string{"attach-session", "-t", "iwr-s"}
	if slices.Compare(gotArgs, wantArgs) != 0 {
		t.Fatalf("attach args: want %v got %v", wantArgs, gotArgs)
	}
}

func TestTmuxSessionAttachWrapsError(t *testing.T) {
	orig := execCommandStdio
	t.Cleanup(func() { execCommandStdio = orig })

	execCommandStdio = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

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
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(name string, args ...string) (commandResult, error) {
		return commandResult{}, errors.New("boom")
	}

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
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(name string, args ...string) (commandResult, error) {
		if len(args) >= 1 && args[0] == "split-window" {
			return commandResult{}, errors.New("split failed")
		}
		return commandResult{}, nil
	}

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
	orig, origIn := runCommand, runCommandWithInput
	t.Cleanup(func() {
		runCommand, runCommandWithInput = orig, origIn
	})
	runCommandWithInput = func(input string, name string, args ...string) (commandResult, error) {
		return commandResult{}, errors.New("load failed")
	}

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
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(name string, args ...string) (commandResult, error) {
		return commandResult{}, errors.New("capture failed")
	}

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

func slicesContainsAny(commands [][]string, needles ...string) bool {
	for _, c := range commands {
		if len(c) < 1 {
			continue
		}
		for i := 0; i < len(c); i++ {
			if slices.Contains(needles, c[i]) {
				return true
			}
		}
	}
	return false
}
