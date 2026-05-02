package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/mkpipe"
)

type fakeCodexAgent struct {
	name     string
	startErr error
	readyErr error
	started  bool
	ready    bool
	closed   bool
	prompts  []string
	sendErrs []error
	sendHook func(string)
}

func (a *fakeCodexAgent) Start() error {
	a.started = true
	return a.startErr
}

func (a *fakeCodexAgent) WaitUntilReady() error {
	a.ready = true
	return a.readyErr
}

func (a *fakeCodexAgent) SessionName() string {
	return a.name
}

func (a *fakeCodexAgent) SendPrompt(prompt string) error {
	a.prompts = append(a.prompts, prompt)
	if a.sendHook != nil {
		a.sendHook(prompt)
	}
	if len(a.sendErrs) == 0 {
		return nil
	}
	err := a.sendErrs[0]
	a.sendErrs = a.sendErrs[1:]
	return err
}

func (a *fakeCodexAgent) Close() error {
	a.closed = true
	return nil
}

type fakeCodexTmuxSession struct {
	name        string
	attachErr   error
	attachCalls int
	attachFn    func(io.Reader, io.Writer, io.Writer) error
}

func (s *fakeCodexTmuxSession) Name() string { return s.name }
func (s *fakeCodexTmuxSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	s.attachCalls++
	if s.attachFn != nil {
		return s.attachFn(stdin, stdout, stderr)
	}
	return s.attachErr
}
func (s *fakeCodexTmuxSession) Close() error                        { return nil }
func (s *fakeCodexTmuxSession) NewPane() (tmux.TmuxPaneLike, error) { return nil, nil }

type stubLock struct{}

func (stubLock) Acquire() error { return nil }
func (stubLock) Release() error { return nil }

func TestRunLaunchesCodexAndPrintsBanner(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--session", "dev"}, nil, &stdout, io.Discard, codexDeps{
		newLock: func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(sessionName string) agentpkg.Agent {
			if sessionName != "dev" {
				t.Fatalf("unexpected session name: %q", sessionName)
			}
			return agent
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !agent.started || !agent.ready {
		t.Fatalf("expected start and readiness checks, got started=%v ready=%v", agent.started, agent.ready)
	}
	if !strings.Contains(stdout.String(), `Launched Codex in tmux session "codex-dev"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunAttachesCodexSessionAfterReady(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	session := &fakeCodexTmuxSession{name: "codex-dev"}

	exitCode := run([]string{"--attach"}, nil, io.Discard, io.Discard, codexDeps{
		newLock:     func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:    func(string) agentpkg.Agent { return agent },
		openSession: func(string) (tmux.TmuxSessionLike, error) { return session, nil },
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if session.attachCalls != 1 {
		t.Fatalf("expected attach once, got %d", session.attachCalls)
	}
}

func TestRunReturnsCodexReadinessFailure(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex", readyErr: errors.New("not ready")}
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, codexDeps{
		newLock:  func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(string) agentpkg.Agent { return agent },
	})
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("stderr missing readiness error: %q", stderr.String())
	}
	if !agent.closed {
		t.Fatal("expected readiness failure to close the agent")
	}
}

func TestRunRejectsUnexpectedPositionalArgs(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{"extra"}, nil, io.Discard, &stderr, codexDeps{})
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unexpected positional arguments") {
		t.Fatalf("stderr missing usage error: %q", stderr.String())
	}
}

func TestParseArgsSupportsCodexMkpipeForms(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{name: "bare_before_attach", args: []string{"--mkpipe", "--attach"}, wantPath: ""},
		{name: "bare_after_attach", args: []string{"--attach", "--mkpipe"}, wantPath: ""},
		{name: "explicit_relative", args: []string{"--session", "reviewer", "--mkpipe", "./custom.pipe", "--attach"}, wantPath: "./custom.pipe"},
		{name: "next_flag_not_consumed", args: []string{"--mkpipe", "--session", "named", "--attach"}, wantPath: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, exitCode, ok := parseArgs(tt.args, io.Discard)
			if !ok || exitCode != 0 {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d", tt.args, ok, exitCode)
			}
			if !parsed.mkpipeEnabled || parsed.mkpipePath != tt.wantPath {
				t.Fatalf("mkpipeEnabled=%v mkpipePath=%q", parsed.mkpipeEnabled, parsed.mkpipePath)
			}
		})
	}
}

func TestParseArgsRejectsCodexMkpipeUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
		{name: "duplicate", args: []string{"--attach", "--mkpipe", "--mkpipe"}, wantSubstring: "invalid --mkpipe: may be provided at most once"},
		{name: "missing_attach", args: []string{"--mkpipe"}, wantSubstring: "invalid --mkpipe: requires --attach"},
		{name: "raw_dash_path", args: []string{"--mkpipe", "-pipe", "--attach"}, wantSubstring: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, exitCode, ok := parseArgs(tt.args, &stderr)
			if ok || exitCode != 2 || !strings.Contains(stderr.String(), tt.wantSubstring) {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d stderr=%q", tt.args, ok, exitCode, stderr.String())
			}
		})
	}
}

type fakeMkpipeListener struct {
	path       string
	messages   chan string
	errors     chan error
	closeCalls int
	closed     chan struct{}
}

func newFakeMkpipeListener(path string) *fakeMkpipeListener {
	return &fakeMkpipeListener{
		path:     path,
		messages: make(chan string, 8),
		errors:   make(chan error, 8),
		closed:   make(chan struct{}),
	}
}

func (l *fakeMkpipeListener) Path() string            { return l.path }
func (l *fakeMkpipeListener) Messages() <-chan string { return l.messages }
func (l *fakeMkpipeListener) Errors() <-chan error    { return l.errors }
func (l *fakeMkpipeListener) Close() error {
	l.closeCalls++
	select {
	case <-l.closed:
	default:
		close(l.closed)
		close(l.messages)
		close(l.errors)
	}
	return nil
}

func TestRunCodexMkpipeStartsListenerAfterReadinessAndPrintsStatus(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	var stdout bytes.Buffer
	wantBanner := "Attaching Codex tmux session \"codex-dev\" with mkpipe \"/tmp/.codex-dev.mkpipe\"\n"
	promptDelivered := make(chan struct{}, 1)
	agent.sendHook = func(prompt string) {
		if prompt == "hello from pipe" {
			promptDelivered <- struct{}{}
		}
	}
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		if got := stdout.String(); got != wantBanner {
			t.Fatalf("attach started before banner was printed: got %q want %q", got, wantBanner)
		}
		select {
		case <-promptDelivered:
			return nil
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for prompt delivery")
			return nil
		}
	}
	go func() { listener.messages <- "hello from pipe" }()

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, &stdout, io.Discard, codexDeps{
		newLock:  func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(string) agentpkg.Agent { return agent },
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) {
			if !agent.ready {
				t.Fatal("mkpipe started before readiness")
			}
			return listener, nil
		},
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	})

	if exitCode != 0 || session.attachCalls != 1 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d attachCalls=%d closeCalls=%d", exitCode, session.attachCalls, listener.closeCalls)
	}
	if got := stdout.String(); got != wantBanner {
		t.Fatalf("stdout = %q, want %q", got, wantBanner)
	}
}

func TestRunCodexMkpipeOpenSessionFailureClosesAgentAndListenerWithoutBanner(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, &stdout, &stderr, codexDeps{
		newLock:       func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:      func(string) agentpkg.Agent { return agent },
		startMkpipe:   func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return nil, errors.New("open session failed") },
		signalContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	})

	if exitCode != 1 || !agent.closed || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closed=%v closeCalls=%d", exitCode, agent.closed, listener.closeCalls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no banner on openSession failure, got stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "open session failed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunCodexMkpipeSetupFailureClosesAgent(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, &stderr, codexDeps{
		newLock:  func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(string) agentpkg.Agent { return agent },
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) {
			return nil, errors.New("mkfifo failed")
		},
	})
	if exitCode != 1 || !agent.closed || !strings.Contains(stderr.String(), "mkfifo failed") {
		t.Fatalf("exit=%d closed=%v stderr=%q", exitCode, agent.closed, stderr.String())
	}
}

func TestRunCodexMkpipeLogsErrorsAndCleansUp(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev", sendErrs: []error{errors.New("pane busy"), nil}}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		listener.errors <- errors.New("fifo read failed")
		listener.messages <- "first"
		listener.messages <- "second"
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	var stderr bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, &stderr, codexDeps{
		newLock:       func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:      func(string) agentpkg.Agent { return agent },
		startMkpipe:   func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	})

	if exitCode != 0 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closeCalls=%d", exitCode, listener.closeCalls)
	}
	if !strings.Contains(stderr.String(), "mkpipe delivery failed: pane busy") ||
		!strings.Contains(stderr.String(), "mkpipe listener error: fifo read failed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunCodexMkpipeInterruptUsesSharedCleanup(t *testing.T) {
	agent := &fakeCodexAgent{name: "codex-dev"}
	listener := newFakeMkpipeListener("/tmp/.codex-dev.mkpipe")
	session := &fakeCodexTmuxSession{name: "codex-dev"}
	ctx, cancel := context.WithCancel(context.Background())
	session.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		cancel()
		<-listener.closed
		return nil
	}

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, io.Discard, codexDeps{
		newLock:       func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent:      func(string) agentpkg.Agent { return agent },
		startMkpipe:   func(mkpipe.Config) (mkpipe.Listener, error) { return listener, nil },
		openSession:   func(string) (tmux.TmuxSessionLike, error) { return session, nil },
		signalContext: func() (context.Context, context.CancelFunc) { return ctx, func() {} },
	})

	if exitCode != 0 || listener.closeCalls != 1 {
		t.Fatalf("exit=%d closeCalls=%d", exitCode, listener.closeCalls)
	}
}

var _ tmux.TmuxSessionLike = (*fakeCodexTmuxSession)(nil)
