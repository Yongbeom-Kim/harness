package session

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	launcherpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

type fakeLock struct {
	acquireErr error
	released   bool
}

func (f *fakeLock) Acquire() error {
	return f.acquireErr
}

func (f *fakeLock) Release() error {
	f.released = true
	return nil
}

type standaloneFakeSessionOpener struct {
	sessionName string
	session     *standaloneFakeSession
	err         error
}

func (f *standaloneFakeSessionOpener) Open(sessionName string) (tmux.TmuxSessionLike, error) {
	f.sessionName = sessionName
	if f.err != nil {
		return nil, f.err
	}
	if f.session == nil {
		f.session = &standaloneFakeSession{name: sessionName, pane: &standaloneFakePane{}}
	}
	f.session.name = sessionName
	return f.session, nil
}

type standaloneFakeSession struct {
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

func (s *standaloneFakeSession) Name() string {
	return s.name
}

func (s *standaloneFakeSession) AttachTarget() string {
	if s.attachTarget != "" {
		return s.attachTarget
	}
	return s.name
}

func (s *standaloneFakeSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	s.attachCalls++
	s.attachStdin = stdin
	s.attachStdout = stdout
	s.attachStderr = stderr
	return s.attachErr
}

func (s *standaloneFakeSession) NewPane() (tmux.TmuxPaneLike, error) {
	if s.newPaneErr != nil {
		return nil, s.newPaneErr
	}
	return s.pane, nil
}

func (s *standaloneFakeSession) Close() error {
	s.closeCalls++
	return s.closeErr
}

type standaloneFakePane struct {
	sendErr error
	sent    []string
}

func (p *standaloneFakePane) SendText(text string) error {
	p.sent = append(p.sent, text)
	return p.sendErr
}

func (p *standaloneFakePane) Capture() (string, error) {
	return "", nil
}

func (p *standaloneFakePane) Target() string {
	return "%1"
}

type standaloneFakeLauncher struct {
	commands [][]string
	result   string
}

func (l *standaloneFakeLauncher) Build(command string, args ...string) string {
	entry := []string{command}
	entry = append(entry, args...)
	l.commands = append(l.commands, entry)
	return l.result
}

func TestParseStandaloneArgsValidationErrors(t *testing.T) {
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
			_, exitCode, ok := parseStandaloneArgs(tt.args, StandaloneConfig{
				ProgramName:        "codex",
				DefaultSessionName: "codex",
				Stderr:             &stderr,
			})
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

func TestRunStandaloneLaunchesWithoutAttach(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &standaloneFakeSession{name: "codex", pane: &standaloneFakePane{}}

	exitCode := RunStandalone([]string{}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stdout:             &stdout,
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{session: session}).Open,
		Launcher:           &standaloneFakeLauncher{result: "launch codex"},
		Lock:               &fakeLock{},
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if session.attachCalls != 0 {
		t.Fatalf("attach should not be called, got %d call(s)", session.attachCalls)
	}
	pane := session.pane.(*standaloneFakePane)
	if len(pane.sent) != 1 || pane.sent[0] != "launch codex" {
		t.Fatalf("unexpected sent launcher: %v", pane.sent)
	}
	if !strings.Contains(stdout.String(), `Launched Codex in tmux session "codex"`) {
		t.Fatalf("stdout missing launch message: %q", stdout.String())
	}
}

func TestRunStandaloneAttachesWhenRequested(t *testing.T) {
	stdin := strings.NewReader("interactive input")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &standaloneFakeSession{name: "codex-dev", pane: &standaloneFakePane{}}

	exitCode := RunStandalone([]string{"--session", "codex-dev", "--attach"}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stdin:              stdin,
		Stdout:             &stdout,
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{session: session}).Open,
		Launcher:           &standaloneFakeLauncher{result: "launch codex"},
		Lock:               &fakeLock{},
	})

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

func TestRunStandaloneClosesSessionWhenPaneCreationFails(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &standaloneFakeSession{name: "codex", newPaneErr: errors.New("pane failed")}

	exitCode := RunStandalone([]string{}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stdout:             &stdout,
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{session: session}).Open,
		Launcher:           &standaloneFakeLauncher{result: "launch codex"},
		Lock:               &fakeLock{},
	})

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

func TestRunStandaloneClosesSessionWhenLaunchFails(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &standaloneFakeSession{name: "codex", pane: &standaloneFakePane{sendErr: errors.New("send failed")}}

	exitCode := RunStandalone([]string{}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stdout:             &stdout,
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{session: session}).Open,
		Launcher:           &standaloneFakeLauncher{result: "launch codex"},
		Lock:               &fakeLock{},
	})

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

func TestRunStandalonePropagatesAttachFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session := &standaloneFakeSession{
		name:      "codex",
		pane:      &standaloneFakePane{},
		attachErr: errors.New("attach failed"),
	}

	exitCode := RunStandalone([]string{"--attach"}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stdin:              strings.NewReader("interactive input"),
		Stdout:             &stdout,
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{session: session}).Open,
		Launcher:           &standaloneFakeLauncher{result: "launch codex"},
		Lock:               &fakeLock{},
	})

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

func TestRunStandaloneReturnsLockFailure(t *testing.T) {
	var stderr bytes.Buffer

	exitCode := RunStandalone([]string{}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stderr:             &stderr,
		OpenSession:        (&standaloneFakeSessionOpener{}).Open,
		Launcher:           &standaloneFakeLauncher{},
		Lock:               &fakeLock{acquireErr: errors.New("locked")},
	})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "locked") {
		t.Fatalf("stderr missing lock failure: %q", stderr.String())
	}
}

func TestRunStandaloneRejectsMissingLaunchDependencies(t *testing.T) {
	var stderr bytes.Buffer

	exitCode := RunStandalone([]string{}, StandaloneConfig{
		ProgramName:        "codex",
		DefaultSessionName: "codex",
		LaunchCommand:      "codex",
		SuccessLabel:       "Codex",
		Stderr:             &stderr,
		Lock:               &fakeLock{},
	})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "session opener is not configured") {
		t.Fatalf("stderr missing dependency failure: %q", stderr.String())
	}
}

var (
	_ tmux.TmuxSessionLike = (*standaloneFakeSession)(nil)
	_ tmux.TmuxPaneLike    = (*standaloneFakePane)(nil)
	_ launcherpkg.Builder  = (*standaloneFakeLauncher)(nil)
	_ Locker               = (*fakeLock)(nil)
)
