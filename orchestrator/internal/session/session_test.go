package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/backend"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/mkpipe"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/tmux"
)

type fakeSession struct {
	name        string
	pane        *fakePane
	closed      bool
	attachCalls int
	attachErr   error
	attachFn    func(io.Reader, io.Writer, io.Writer) error
}

func (s *fakeSession) Name() string { return s.name }
func (s *fakeSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	s.attachCalls++
	if s.attachFn != nil {
		return s.attachFn(stdin, stdout, stderr)
	}
	return s.attachErr
}
func (s *fakeSession) Close() error {
	s.closed = true
	return nil
}
func (s *fakeSession) NewPane() (tmux.TmuxPaneLike, error) { return s.pane, nil }

type fakePane struct {
	sent        []string
	captures    []string
	captureErr  error
	captureCall int
}

func (p *fakePane) SendText(text string) error {
	p.sent = append(p.sent, text)
	return nil
}
func (p *fakePane) Capture() (string, error) {
	if p.captureErr != nil {
		return "", p.captureErr
	}
	if len(p.captures) == 0 {
		return "", nil
	}
	if p.captureCall >= len(p.captures) {
		return p.captures[len(p.captures)-1], nil
	}
	got := p.captures[p.captureCall]
	p.captureCall++
	return got, nil
}

type fakeLock struct {
	acquireCalls int
	releaseCalls int
	releaseErr   error
}

func (l *fakeLock) Acquire() error {
	l.acquireCalls++
	return nil
}
func (l *fakeLock) Release() error {
	l.releaseCalls++
	return l.releaseErr
}

func TestStartLockReleaseFailureClosesSession(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	s, tmuxSession := newReadySession("dev", pane, &now)
	lock := &fakeLock{releaseErr: errors.New("release failed")}
	s.lockPolicy = func() (Lock, error) { return lock, nil }

	err := s.Start()
	if err == nil {
		t.Fatal("expected error from lock release")
	}
	if !tmuxSession.closed || s.state != stateClosed {
		t.Fatalf("closed=%v state=%v", tmuxSession.closed, s.state)
	}
	if lock.releaseCalls != 1 {
		t.Fatalf("release calls = %d", lock.releaseCalls)
	}
}

type fakeListener struct {
	path       string
	messages   chan string
	errors     chan error
	closeCalls int
	closed     chan struct{}
}

func newFakeListener(path string) *fakeListener {
	return &fakeListener{
		path:     path,
		messages: make(chan string, 8),
		errors:   make(chan error, 8),
		closed:   make(chan struct{}),
	}
}
func (l *fakeListener) Path() string            { return l.path }
func (l *fakeListener) Messages() <-chan string { return l.messages }
func (l *fakeListener) Errors() <-chan error    { return l.errors }
func (l *fakeListener) Close() error {
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

func readyDeps(session *fakeSession, now *time.Time) deps {
	return deps{
		newTmuxSession: func(name string) (tmux.TmuxSessionLike, error) {
			session.name = name
			return session, nil
		},
		buildLaunchCommand: func(command string, args ...string) (string, error) {
			return "launch " + command, nil
		},
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) {
			return newFakeListener("/tmp/.session.mkpipe"), nil
		},
		signalContext: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
		now: func() time.Time { return *now },
		sleep: func(d time.Duration) {
			*now = now.Add(d)
		},
		readyTimeout: time.Second,
		quietPeriod:  10 * time.Millisecond,
		pollInterval: 10 * time.Millisecond,
	}
}

func newReadySession(name string, pane *fakePane, now *time.Time) (*Session, *fakeSession) {
	tmuxSession := &fakeSession{pane: pane}
	s := newSession(backend.Codex{}, Config{SessionName: name})
	s.deps = readyDeps(tmuxSession, now)
	return s, tmuxSession
}

func TestNewCodexUsesBackendDefaultSessionName(t *testing.T) {
	s := NewCodex(Config{})
	if got := s.SessionName(); got != "codex" {
		t.Fatalf("SessionName() = %q, want codex", got)
	}
}

func TestNewCursorUsesBackendDefaultSessionName(t *testing.T) {
	s := NewCursor(Config{})
	if got := s.SessionName(); got != "cursor" {
		t.Fatalf("SessionName() = %q, want cursor", got)
	}
}

func TestNewCursorHonorsExplicitSessionName(t *testing.T) {
	s := NewCursor(Config{SessionName: "review"})
	if got := s.SessionName(); got != "review" {
		t.Fatalf("SessionName() = %q, want review", got)
	}
}

func TestStartRejectsMkpipeConfig(t *testing.T) {
	s := NewCodex(Config{Mkpipe: &MkpipeConfig{}})
	err := s.Start()
	if err == nil || !strings.Contains(err.Error(), "mkpipe") {
		t.Fatalf("Start() error = %v, want mkpipe rejection", err)
	}
}

func TestStartWaitsForQuietReadyState(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"boot", "OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	s, tmuxSession := newReadySession("dev", pane, &now)

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if s.state != stateStarted {
		t.Fatalf("state = %v, want started", s.state)
	}
	if tmuxSession.closed {
		t.Fatal("session should remain running after Start")
	}
	if len(pane.sent) != 1 || pane.sent[0] != "launch codex" {
		t.Fatalf("sent launch text = %v", pane.sent)
	}
}

func TestStartReturnsStartupErrorWhenReadyTimeoutExpires(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"boot"}}
	s, tmuxSession := newReadySession("dev", pane, &now)
	s.deps.readyTimeout = 20 * time.Millisecond

	err := s.Start()
	if err == nil {
		t.Fatal("expected startup timeout")
	}
	var sessionErr *Error
	if !errors.As(err, &sessionErr) || sessionErr.Kind != ErrorKindStartup || sessionErr.Capture != "boot" {
		t.Fatalf("error = %#v, want startup Error with latest capture", err)
	}
	if !tmuxSession.closed || s.state != stateClosed {
		t.Fatalf("closed=%v state=%v, want cleanup and closed state", tmuxSession.closed, s.state)
	}
}

func TestStartupFailureClosesSessionReleasesLockAndTransitionsClosed(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captureErr: errors.New("capture failed")}
	lock := &fakeLock{}
	s, tmuxSession := newReadySession("dev", pane, &now)
	s.lockPolicy = func() (Lock, error) { return lock, nil }

	err := s.Start()
	if err == nil {
		t.Fatal("expected startup failure")
	}
	if !tmuxSession.closed || lock.releaseCalls != 1 || s.state != stateClosed {
		t.Fatalf("closed=%v releases=%d state=%v", tmuxSession.closed, lock.releaseCalls, s.state)
	}
}

func TestCloseFromNewIsNoOp(t *testing.T) {
	s := NewCodex(Config{SessionName: "dev"})
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if s.state != stateNew {
		t.Fatalf("state = %v, want new (unchanged)", s.state)
	}
}

func TestAttachMkpipeStartFailureCleansSessionAndReleasesLock(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	lock := &fakeLock{}
	tmuxSession := &fakeSession{pane: pane}
	s := newSession(backend.Codex{}, Config{
		SessionName: "dev",
		Mkpipe:      &MkpipeConfig{},
		LockPolicy:  func() (Lock, error) { return lock, nil },
	})
	s.deps = readyDeps(tmuxSession, &now)
	s.deps.startMkpipe = func(mkpipe.Config) (mkpipe.Listener, error) {
		return nil, errors.New("mkpipe failed")
	}

	err := s.Attach(AttachOptions{})
	if err == nil {
		t.Fatal("expected error from mkpipe start")
	}
	if !tmuxSession.closed || s.state != stateClosed || lock.releaseCalls != 1 {
		t.Fatalf("closed=%v state=%v lockReleases=%d", tmuxSession.closed, s.state, lock.releaseCalls)
	}
}

func TestSendPromptCaptureAndCloseStateRules(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› ", "captured"}}
	s, tmuxSession := newReadySession("dev", pane, &now)

	if err := s.SendPrompt("before"); err == nil {
		t.Fatal("SendPrompt before start error = nil")
	}
	if _, err := s.Capture(); err == nil {
		t.Fatal("Capture before start error = nil")
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.SendPrompt("hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got, err := s.Capture(); err != nil || got != "captured" {
		t.Fatalf("Capture() = %q, %v; want captured, nil", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if !tmuxSession.closed {
		t.Fatal("expected Close to close backend session")
	}
}

func TestAttachNewHandleStartsMkpipeBeforeAttachAndInvokesBeforeAttach(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	listener := newFakeListener("/tmp/.dev.mkpipe")
	lock := &fakeLock{}
	tmuxSession := &fakeSession{pane: pane}
	var stderr bytes.Buffer
	s := newSession(backend.Codex{}, Config{
		SessionName: "dev",
		Mkpipe:      &MkpipeConfig{Path: "./custom.pipe"},
		LockPolicy:  func() (Lock, error) { return lock, nil },
	})
	s.deps = readyDeps(tmuxSession, &now)
	s.deps.startMkpipe = func(cfg mkpipe.Config) (mkpipe.Listener, error) {
		if cfg.RequestedPath != "./custom.pipe" || cfg.SessionName != "dev" || cfg.DefaultBasename != "codex" {
			t.Fatalf("unexpected mkpipe config: %+v", cfg)
		}
		return listener, nil
	}
	var before sessionAttach
	tmuxSession.attachFn = func(io.Reader, io.Writer, io.Writer) error {
		if before.info.MkpipePath != listener.Path() {
			t.Fatalf("BeforeAttach info = %+v", before.info)
		}
		listener.messages <- "from pipe"
		return nil
	}

	err := s.Attach(AttachOptions{
		Stderr: &stderr,
		BeforeAttach: func(info AttachInfo) {
			before.called = true
			before.info = info
		},
	})
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if !before.called || tmuxSession.attachCalls != 1 || listener.closeCalls != 1 || lock.releaseCalls != 1 {
		t.Fatalf("before=%v attach=%d listener close=%d lock release=%d", before.called, tmuxSession.attachCalls, listener.closeCalls, lock.releaseCalls)
	}
	if s.state != stateStarted || tmuxSession.closed {
		t.Fatalf("state=%v closed=%v, want started live session", s.state, tmuxSession.closed)
	}
}

type sessionAttach struct {
	called bool
	info   AttachInfo
}

func TestAttachStartedHandleDoesNotRestartOrEnableMkpipe(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	s, tmuxSession := newReadySession("dev", pane, &now)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	startSent := len(pane.sent)
	var info AttachInfo
	if err := s.Attach(AttachOptions{
		BeforeAttach: func(got AttachInfo) { info = got },
	}); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if len(pane.sent) != startSent || tmuxSession.attachCalls != 1 || info.MkpipePath != "" {
		t.Fatalf("sent=%d/%d attach=%d info=%+v", len(pane.sent), startSent, tmuxSession.attachCalls, info)
	}
}
