package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/tmux"
)

type fakeSession struct {
	name         string
	attachTarget string
	pane         tmux.TmuxPaneLike
	newPaneErr   error
	attachErr    error
	closeErr     error
	attachCalls  int
	closeCalls   int
	attachStdin  io.Reader
	attachStdout io.Writer
	attachStderr io.Writer
}

func (s *fakeSession) Name() string { return s.name }

func (s *fakeSession) AttachTarget() string {
	if s.attachTarget != "" {
		return s.attachTarget
	}
	return s.name
}

func (s *fakeSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	s.attachCalls++
	s.attachStdin = stdin
	s.attachStdout = stdout
	s.attachStderr = stderr
	return s.attachErr
}

func (s *fakeSession) Close() error {
	s.closeCalls++
	return s.closeErr
}

func (s *fakeSession) NewPane() (tmux.TmuxPaneLike, error) {
	if s.newPaneErr != nil {
		return nil, s.newPaneErr
	}
	return s.pane, nil
}

type fakePane struct {
	target  string
	sendErr error
	sent    []string
}

func (p *fakePane) SendText(text string) error {
	p.sent = append(p.sent, text)
	return p.sendErr
}

func (p *fakePane) Capture() (string, error) { return "", nil }

func (p *fakePane) Target() string { return p.target }

func TestParseArgsValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{name: "empty session", args: []string{"--session", " \t "}, expected: "invalid --session: must not be empty"},
		{name: "unexpected positional", args: []string{"extra"}, expected: "unexpected positional arguments: extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, exitCode, ok := parseArgs(tt.args, &stderr)
			if ok {
				t.Fatal("expected parse failure")
			}
			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d", exitCode)
			}
			if !strings.Contains(stderr.String(), tt.expected) {
				t.Fatalf("stderr %q did not contain %q", stderr.String(), tt.expected)
			}
		})
	}
}

func TestRunCodexLaunchesWithoutAttach(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	pane := &fakePane{target: "codex:0.0"}
	session := &fakeSession{name: "codex", pane: pane}

	exitCode := runCodex(NewRunnerConfig(
		WithStdout(&stdout),
		WithStderr(&stderr),
		WithOpenSession(func(name string) (tmux.TmuxSessionLike, error) {
			if name != "codex" {
				t.Fatalf("unexpected session name: %q", name)
			}
			return session, nil
		}),
		WithBuildLaunch(func(command string, args ...string) string {
			if command != "codex" {
				t.Fatalf("unexpected command: %q", command)
			}
			return "launch codex"
		}),
	), parsedArgs{sessionName: "codex"})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if session.attachCalls != 0 {
		t.Fatalf("attach should not be called, got %d call(s)", session.attachCalls)
	}
	if session.closeCalls != 0 {
		t.Fatalf("close should not be called, got %d call(s)", session.closeCalls)
	}
	if len(pane.sent) != 1 || pane.sent[0] != "launch codex" {
		t.Fatalf("unexpected sent launcher: %v", pane.sent)
	}
	if !strings.Contains(stdout.String(), `Launched Codex in tmux session "codex"`) {
		t.Fatalf("stdout missing launch message: %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunCodexAttachesWhenRequested(t *testing.T) {
	stdin := strings.NewReader("interactive input")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	pane := &fakePane{}
	session := &fakeSession{name: "codex-dev", pane: pane}

	exitCode := runCodex(NewRunnerConfig(
		WithStdin(stdin),
		WithStdout(&stdout),
		WithStderr(&stderr),
		WithOpenSession(func(name string) (tmux.TmuxSessionLike, error) {
			if name != "codex-dev" {
				t.Fatalf("unexpected session name: %q", name)
			}
			return session, nil
		}),
		WithBuildLaunch(func(string, ...string) string { return "launch codex" }),
	), parsedArgs{sessionName: "codex-dev", attach: true})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if session.attachCalls != 1 {
		t.Fatalf("expected one attach call, got %d", session.attachCalls)
	}
	if session.attachStdin != stdin || session.attachStdout != &stdout || session.attachStderr != &stderr {
		t.Fatalf("attach did not receive configured io")
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout when attaching, got %q", stdout.String())
	}
}

func TestRunCodexClosesSessionWhenPaneCreationFails(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &fakeSession{name: "codex", newPaneErr: errors.New("pane failed")}

	exitCode := runCodex(NewRunnerConfig(
		WithStdout(&stdout),
		WithStderr(&stderr),
		WithOpenSession(func(string) (tmux.TmuxSessionLike, error) {
			return session, nil
		}),
		WithBuildLaunch(func(string, ...string) string { return "launch codex" }),
	), parsedArgs{sessionName: "codex"})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if session.closeCalls != 1 {
		t.Fatalf("expected one close call, got %d", session.closeCalls)
	}
	if !strings.Contains(stderr.String(), "pane failed") {
		t.Fatalf("stderr missing pane failure: %q", stderr.String())
	}
}

func TestRunCodexClosesSessionWhenLaunchFails(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	pane := &fakePane{sendErr: errors.New("send failed")}
	session := &fakeSession{name: "codex", pane: pane}

	exitCode := runCodex(NewRunnerConfig(
		WithStdout(&stdout),
		WithStderr(&stderr),
		WithOpenSession(func(string) (tmux.TmuxSessionLike, error) {
			return session, nil
		}),
		WithBuildLaunch(func(string, ...string) string { return "launch codex" }),
	), parsedArgs{sessionName: "codex"})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if session.closeCalls != 1 {
		t.Fatalf("expected one close call, got %d", session.closeCalls)
	}
	if !strings.Contains(stderr.String(), "send failed") {
		t.Fatalf("stderr missing send failure: %q", stderr.String())
	}
}

func TestRunCodexPropagatesAttachFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &fakeSession{
		name:      "codex",
		pane:      &fakePane{},
		attachErr: errors.New("attach failed"),
	}

	exitCode := runCodex(NewRunnerConfig(
		WithStdin(strings.NewReader("interactive input")),
		WithStdout(&stdout),
		WithStderr(&stderr),
		WithOpenSession(func(string) (tmux.TmuxSessionLike, error) {
			return session, nil
		}),
		WithBuildLaunch(func(string, ...string) string { return "launch codex" }),
	), parsedArgs{sessionName: "codex", attach: true})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if session.attachCalls != 1 {
		t.Fatalf("expected one attach call, got %d", session.attachCalls)
	}
	if !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("stderr missing attach failure: %q", stderr.String())
	}
}
