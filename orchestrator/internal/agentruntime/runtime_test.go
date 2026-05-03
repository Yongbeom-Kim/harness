package agentruntime

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/backend"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/mkpipe"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type fakeSession struct {
	name   string
	closed bool
	pane   *fakePane
}

func (s *fakeSession) Name() string { return s.name }
func (s *fakeSession) Attach(io.Reader, io.Writer, io.Writer) error {
	return nil
}
func (s *fakeSession) Close() error {
	s.closed = true
	return nil
}
func (s *fakeSession) NewPane() (tmux.TmuxPaneLike, error) { return s.pane, nil }

type fakePane struct {
	sent        []string
	captures    []string
	sendErr     error
	captureErr  error
	captureCall int
	closeErr    error
	closed      bool
}

func (p *fakePane) SendText(text string) error {
	if p.sendErr != nil {
		return p.sendErr
	}
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

func (p *fakePane) Close() error {
	p.closed = true
	return p.closeErr
}

type fakeListener struct {
	path       string
	messages   chan string
	errors     chan error
	closeCalls int
}

func newFakeListener(path string) *fakeListener {
	return &fakeListener{
		path:     path,
		messages: make(chan string, 8),
		errors:   make(chan error, 8),
	}
}

func (l *fakeListener) Path() string            { return l.path }
func (l *fakeListener) Messages() <-chan string { return l.messages }
func (l *fakeListener) Errors() <-chan error    { return l.errors }
func (l *fakeListener) Close() error {
	l.closeCalls++
	select {
	case <-l.messages:
	default:
	}
	select {
	case <-l.errors:
	default:
	}
	close(l.messages)
	close(l.errors)
	return nil
}

func readyDeps(listener mkpipe.Listener, now *time.Time) deps {
	return deps{
		buildLaunchCommand: func(command string, args ...string) (string, error) {
			return "launch " + command, nil
		},
		startMkpipe: func(mkpipe.Config) (mkpipe.Listener, error) {
			if listener == nil {
				return nil, errors.New("listener not configured")
			}
			return listener, nil
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

func newReadyRuntime(name string, pane *fakePane, now *time.Time) (*Runtime, *fakeSession) {
	session := &fakeSession{name: name, pane: pane}
	rt := newRuntime(backend.Codex{}, session, pane, Config{SessionName: name})
	rt.deps = readyDeps(newFakeListener("/tmp/.runtime.mkpipe"), now)
	return rt, session
}

func TestNewCodexUsesBackendDefaultSessionName(t *testing.T) {
	rt := NewCodex(nil, nil, Config{})
	if got := rt.SessionName(); got != "codex" {
		t.Fatalf("SessionName() = %q, want codex", got)
	}
}

func TestNewCursorHonorsExplicitSessionName(t *testing.T) {
	rt := NewCursor(nil, nil, Config{SessionName: "review"})
	if got := rt.SessionName(); got != "review" {
		t.Fatalf("SessionName() = %q, want review", got)
	}
}

func TestRuntimeStartWaitsForQuietReadyState(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"boot", "OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	rt, session := newReadyRuntime("dev", pane, &now)

	info, err := rt.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if info.Mkpipe != nil {
		t.Fatalf("Start() Mkpipe = %+v, want nil", info.Mkpipe)
	}
	if rt.state != stateStarted {
		t.Fatalf("state = %v, want started", rt.state)
	}
	if pane.closed || session.closed {
		t.Fatalf("pane.closed=%v session.closed=%v", pane.closed, session.closed)
	}
	if len(pane.sent) != 1 || pane.sent[0] != "launch codex" {
		t.Fatalf("sent launch text = %v", pane.sent)
	}
}

func TestRuntimeStartReturnsStartupErrorAndClosesOnlyPane(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"boot"}}
	rt, session := newReadyRuntime("dev", pane, &now)
	rt.deps.readyTimeout = 20 * time.Millisecond

	_, err := rt.Start()
	if err == nil {
		t.Fatal("expected startup timeout")
	}
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Kind != ErrorKindStartup || runtimeErr.Capture != "boot" {
		t.Fatalf("error = %#v, want startup Error with latest capture", err)
	}
	if !pane.closed || session.closed || rt.state != stateClosed {
		t.Fatalf("pane.closed=%v session.closed=%v state=%v", pane.closed, session.closed, rt.state)
	}
}

func TestRuntimeSendPromptCaptureAndCloseStateRules(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› ", "captured"}}
	rt, session := newReadyRuntime("dev", pane, &now)

	if err := rt.SendPrompt("before"); err == nil {
		t.Fatal("SendPrompt before start error = nil")
	}
	if _, err := rt.Capture(); err == nil {
		t.Fatal("Capture before start error = nil")
	}
	if _, err := rt.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.SendPrompt("hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got, err := rt.Capture(); err != nil || got != "captured" {
		t.Fatalf("Capture() = %q, %v; want captured, nil", got, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if !pane.closed || session.closed {
		t.Fatalf("pane.closed=%v session.closed=%v", pane.closed, session.closed)
	}
}

func TestRuntimeStartStartsConfiguredMkpipeUsesBasenameOverrideAndForwardsMessages(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	listener := newFakeListener("/abs/reviewer.pipe")
	rt, _ := newReadyRuntime("implement-with-reviewer-123", pane, &now)
	rt.mkpipe = &MkpipeConfig{BasenameOverride: "implement-with-reviewer-123-implementer"}

	var gotConfig mkpipe.Config
	rt.deps.startMkpipe = func(cfg mkpipe.Config) (mkpipe.Listener, error) {
		gotConfig = cfg
		return listener, nil
	}

	info, err := rt.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if info.Mkpipe == nil || info.Mkpipe.Path != "/abs/reviewer.pipe" {
		t.Fatalf("Start() Mkpipe = %+v, want /abs/reviewer.pipe", info.Mkpipe)
	}
	if gotConfig.BasenameOverride != "implement-with-reviewer-123-implementer" || gotConfig.SessionName != "implement-with-reviewer-123" {
		t.Fatalf("unexpected mkpipe config: %+v", gotConfig)
	}

	listener.messages <- "from pipe"
	deadline := time.After(time.Second)
	for {
		if len(pane.sent) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for forwarded prompt, sent=%v", pane.sent)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := pane.sent[1]; got != "from pipe" {
		t.Fatalf("forwarded prompt = %q, want %q", got, "from pipe")
	}

	if err := rt.StopMkpipe(); err != nil {
		t.Fatalf("StopMkpipe() error = %v", err)
	}
	if err := rt.StopMkpipe(); err != nil {
		t.Fatalf("second StopMkpipe() error = %v", err)
	}
}

func TestRuntimeMkpipeErrorsExposeAsyncDeliveryFailures(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	listener := newFakeListener("/abs/runtime.pipe")
	rt, _ := newReadyRuntime("dev", pane, &now)
	rt.mkpipe = &MkpipeConfig{}
	rt.deps.startMkpipe = func(cfg mkpipe.Config) (mkpipe.Listener, error) {
		return listener, nil
	}

	info, err := rt.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if info.Mkpipe == nil || info.Mkpipe.Path != "/abs/runtime.pipe" {
		t.Fatalf("Start() Mkpipe = %+v, want /abs/runtime.pipe", info.Mkpipe)
	}
	pane.sendErr = errors.New("send failed")

	listener.messages <- "from pipe"
	select {
	case err := <-rt.MkpipeErrors():
		if !strings.Contains(err.Error(), "send failed") {
			t.Fatalf("MkpipeErrors() = %v, want delivery failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mkpipe delivery error")
	}
}
