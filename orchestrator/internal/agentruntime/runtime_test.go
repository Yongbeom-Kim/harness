package agentruntime

import (
	"errors"
	"io"
	"slices"
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
	keys        []string
	captures    []string
	sendErr     error
	pressErr    error
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

func (p *fakePane) PressKey(key string) error {
	if p.pressErr != nil {
		return p.pressErr
	}
	p.keys = append(p.keys, key)
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

type fakeBackend struct {
	defaultSessionName string
	launchFn           func(tmux.TmuxPaneLike, backend.LaunchCommandBuilder) error
	waitUntilReadyFn   func(tmux.TmuxPaneLike, backend.ReadinessOptions) error
	sendPromptNowFn    func(tmux.TmuxPaneLike, string) error
	sendPromptQueuedFn func(tmux.TmuxPaneLike, string) error
}

func (b fakeBackend) DefaultSessionName() string {
	if b.defaultSessionName != "" {
		return b.defaultSessionName
	}
	return "fake"
}

func (b fakeBackend) Launch(pane tmux.TmuxPaneLike, buildLaunchCommand backend.LaunchCommandBuilder) error {
	if b.launchFn != nil {
		return b.launchFn(pane, buildLaunchCommand)
	}
	return nil
}

func (b fakeBackend) WaitUntilReady(pane tmux.TmuxPaneLike, opts backend.ReadinessOptions) error {
	if b.waitUntilReadyFn != nil {
		return b.waitUntilReadyFn(pane, opts)
	}
	return nil
}

func (b fakeBackend) SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error {
	if b.sendPromptNowFn != nil {
		return b.sendPromptNowFn(pane, prompt)
	}
	return nil
}

func (b fakeBackend) SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error {
	if b.sendPromptQueuedFn != nil {
		return b.sendPromptQueuedFn(pane, prompt)
	}
	return nil
}

func newReadyRuntime(name string, pane *fakePane, now *time.Time) (*Runtime, *fakeSession) {
	return newReadyRuntimeWithBackend(name, backend.Codex{}, pane, now)
}

func newReadyRuntimeWithBackend(name string, b backend.Backend, pane *fakePane, now *time.Time) (*Runtime, *fakeSession) {
	session := &fakeSession{name: name, pane: pane}
	rt := newRuntime(b, session, pane, Config{SessionName: name})
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
	if !slices.Equal(pane.sent, []string{"launch codex"}) {
		t.Fatalf("sent launch text = %v", pane.sent)
	}
	if !slices.Equal(pane.keys, []string{"Enter"}) {
		t.Fatalf("pressed keys = %v, want [Enter]", pane.keys)
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

func TestRuntimeSendPromptMethodsKeepStateErrors(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &fakePane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› ", "captured"}}
	rt, session := newReadyRuntime("dev", pane, &now)

	assertRuntimeNotStartedError(t, rt.SendPromptNow("before"), "dev")
	assertRuntimeNotStartedError(t, rt.SendPromptQueued("before"), "dev")
	if _, err := rt.Capture(); err == nil {
		t.Fatal("Capture before start error = nil")
	}
	if _, err := rt.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.SendPromptNow("hello"); err != nil {
		t.Fatalf("SendPromptNow() error = %v", err)
	}
	if err := rt.SendPromptQueued("later"); err != nil {
		t.Fatalf("SendPromptQueued() error = %v", err)
	}
	if got, err := rt.Capture(); err != nil || got != "captured" {
		t.Fatalf("Capture() = %q, %v; want captured, nil", got, err)
	}
	if !slices.Equal(pane.sent, []string{"launch codex", "hello", "later"}) {
		t.Fatalf("sent = %v", pane.sent)
	}
	if !slices.Equal(pane.keys, []string{"Enter", "Enter", "Tab"}) {
		t.Fatalf("keys = %v", pane.keys)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	assertRuntimeNotStartedError(t, rt.SendPromptNow("after"), "dev")
	assertRuntimeNotStartedError(t, rt.SendPromptQueued("after"), "dev")
	if !pane.closed || session.closed {
		t.Fatalf("pane.closed=%v session.closed=%v", pane.closed, session.closed)
	}
}

func assertRuntimeNotStartedError(t *testing.T, err error, sessionName string) {
	t.Helper()

	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %#v, want *Error", err)
	}
	if runtimeErr.Kind != ErrorKindCapture || runtimeErr.SessionName != sessionName {
		t.Fatalf("error = %#v, want capture error for %q", runtimeErr, sessionName)
	}
	if runtimeErr.Err == nil || !strings.Contains(runtimeErr.Err.Error(), "runtime has not started") {
		t.Fatalf("wrapped error = %#v, want not-started message", runtimeErr.Err)
	}
}

func TestRuntimeSendPromptWrapsTypedTmuxSendFailures(t *testing.T) {
	tmuxErr := &tmux.NonInteractivePaneError{
		Target:    "%7",
		Operation: "send text",
		Attempts:  5,
	}
	pane := &fakePane{sendErr: tmuxErr}
	rt := newRuntime(
		fakeBackend{
			defaultSessionName: "codex",
			sendPromptNowFn: func(pane tmux.TmuxPaneLike, prompt string) error {
				if err := pane.SendText(prompt); err != nil {
					return err
				}
				return pane.PressKey("Enter")
			},
		},
		nil,
		pane,
		Config{SessionName: "dev"},
	)
	rt.state = stateStarted

	err := rt.SendPromptNow("hello")
	if err == nil {
		t.Fatal("SendPromptNow() error = nil, want wrapped tmux error")
	}

	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %#v, want *Error", err)
	}
	if runtimeErr.Kind != ErrorKindCapture || runtimeErr.SessionName != "dev" {
		t.Fatalf("runtime error = %#v, want capture error for dev", runtimeErr)
	}
	if runtimeErr.Capture != "" {
		t.Fatalf("Capture = %q, want empty capture", runtimeErr.Capture)
	}

	var wrappedTmuxErr *tmux.NonInteractivePaneError
	if !errors.As(runtimeErr.Err, &wrappedTmuxErr) {
		t.Fatalf("wrapped error = %#v, want *tmux.NonInteractivePaneError", runtimeErr.Err)
	}
	if wrappedTmuxErr != tmuxErr {
		t.Fatalf("wrapped error = %#v, want original tmux error %#v", wrappedTmuxErr, tmuxErr)
	}
}

func TestRuntimeMkpipeForwardersUseQueuedSemanticsWithoutLocalBuffering(t *testing.T) {
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

	listener.messages <- "first from pipe"
	listener.messages <- "second from pipe"
	waitForPaneSends(t, pane, 3)

	if got := pane.sent[1:]; !slices.Equal(got, []string{"first from pipe", "second from pipe"}) {
		t.Fatalf("forwarded prompts = %v", got)
	}
	if got := pane.keys[1:]; !slices.Equal(got, []string{"Tab", "Tab"}) {
		t.Fatalf("forwarded keys = %v, want queued Tab delivery", got)
	}

	if err := rt.StopMkpipe(); err != nil {
		t.Fatalf("StopMkpipe() error = %v", err)
	}
	if err := rt.StopMkpipe(); err != nil {
		t.Fatalf("second StopMkpipe() error = %v", err)
	}
}

func waitForPaneSends(t *testing.T, pane *fakePane, want int) {
	t.Helper()

	deadline := time.After(time.Second)
	for len(pane.sent) < want {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d sends, got sent=%v keys=%v", want, pane.sent, pane.keys)
		default:
			time.Sleep(10 * time.Millisecond)
		}
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
	t.Cleanup(func() {
		if err := rt.StopMkpipe(); err != nil {
			t.Fatalf("StopMkpipe() error = %v", err)
		}
	})
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

	listener.errors <- errors.New("listener failed")
	select {
	case err := <-rt.MkpipeErrors():
		if !strings.Contains(err.Error(), "listener failed") {
			t.Fatalf("MkpipeErrors() = %v, want listener failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mkpipe listener error")
	}
}

func TestRuntimeSendMutexSerializesFullPromptSequence(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstSequenceDone := make(chan struct{})

	testBackend := fakeBackend{
		defaultSessionName: "codex",
		sendPromptNowFn: func(pane tmux.TmuxPaneLike, prompt string) error {
			close(firstEntered)
			<-releaseFirst
			if err := pane.SendText(prompt); err != nil {
				return err
			}
			if err := pane.PressKey("Enter"); err != nil {
				return err
			}
			close(firstSequenceDone)
			return nil
		},
		sendPromptQueuedFn: func(pane tmux.TmuxPaneLike, prompt string) error {
			select {
			case <-firstSequenceDone:
			default:
				return errors.New("queued send entered before first sequence finished")
			}
			if err := pane.SendText(prompt); err != nil {
				return err
			}
			return pane.PressKey("Tab")
		},
	}

	pane := &fakePane{}
	rt := newRuntime(testBackend, nil, pane, Config{SessionName: "dev"})
	rt.state = stateStarted

	errs := make(chan error, 2)
	go func() { errs <- rt.SendPromptNow("first") }()
	<-firstEntered

	go func() { errs <- rt.SendPromptQueued("second") }()
	select {
	case err := <-errs:
		t.Fatalf("send returned before first sequence released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("send error = %v", err)
		}
	}

	if !slices.Equal(pane.sent, []string{"first", "second"}) {
		t.Fatalf("sent = %v", pane.sent)
	}
	if !slices.Equal(pane.keys, []string{"Enter", "Tab"}) {
		t.Fatalf("keys = %v", pane.keys)
	}
}
