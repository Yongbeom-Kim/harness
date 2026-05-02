package session

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	sessionlauncher "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

type fakeSessionOpener struct {
	sessionName string
	session     *fakeHostSession
	err         error
}

func (f *fakeSessionOpener) Open(sessionName string) (tmux.TmuxSessionLike, error) {
	f.sessionName = sessionName
	if f.err != nil {
		return nil, f.err
	}
	if f.session == nil {
		f.session = &fakeHostSession{
			name: sessionName,
			pane: &recordingPane{},
		}
	}
	f.session.name = sessionName
	return f.session, nil
}

type fakeHostSession struct {
	name       string
	pane       tmux.TmuxPaneLike
	newPaneErr error
	closeErr   error
	attachErr  error
}

func (s *fakeHostSession) Name() string {
	return s.name
}

func (s *fakeHostSession) AttachTarget() string {
	return s.name
}

func (s *fakeHostSession) Attach(io.Reader, io.Writer, io.Writer) error {
	return s.attachErr
}

func (s *fakeHostSession) NewPane() (tmux.TmuxPaneLike, error) {
	if s.newPaneErr != nil {
		return nil, s.newPaneErr
	}
	return s.pane, nil
}

func (s *fakeHostSession) Close() error {
	return s.closeErr
}

type recordingPane struct {
	mu           sync.Mutex
	calls        []string
	sendErr      error
	captures     []string
	captureErr   error
	captureIndex int
}

func (p *recordingPane) SendText(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sendErr != nil {
		return p.sendErr
	}
	p.calls = append(p.calls, text)
	return nil
}

func (p *recordingPane) Capture() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.captureErr != nil {
		return "", p.captureErr
	}
	if len(p.captures) == 0 {
		return "", nil
	}
	index := p.captureIndex
	if index >= len(p.captures) {
		index = len(p.captures) - 1
	} else {
		p.captureIndex++
	}
	return p.captures[index], nil
}

func (p *recordingPane) Target() string {
	return "%1"
}

func (p *recordingPane) joined() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.calls, "")
}

type interleavingPane struct {
	mu      sync.Mutex
	builder strings.Builder
}

func (p *interleavingPane) SendText(text string) error {
	for _, r := range text {
		p.mu.Lock()
		p.builder.WriteRune(r)
		p.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	return nil
}

func (p *interleavingPane) Capture() (string, error) {
	return "complete", nil
}

func (p *interleavingPane) Target() string {
	return "%1"
}

func (p *interleavingPane) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.builder.String()
}

type fakeLauncher struct {
	commands [][]string
	result   string
}

func (l *fakeLauncher) Build(command string, args ...string) string {
	entry := []string{command}
	entry = append(entry, args...)
	l.commands = append(l.commands, entry)
	return l.result
}

type fakeProtocol struct {
	rolePrompt         string
	startupInstruction string
	incomplete         bool
}

func (p *fakeProtocol) DecorateStartupPrompt(rolePrompt string, startupInstruction string) string {
	p.rolePrompt = rolePrompt
	p.startupInstruction = startupInstruction
	return "decorated startup"
}

func (p *fakeProtocol) PrepareTurn(prompt string) PreparedTurn {
	return PreparedTurn{Prompt: prompt, PromptBody: prompt, Marker: "marker"}
}

func (p *fakeProtocol) ExtractTurnCapture(capture string, turn PreparedTurn) string {
	return capture
}

func (p *fakeProtocol) IsTurnComplete(capture string, turn PreparedTurn) bool {
	return !p.incomplete
}

func (p *fakeProtocol) SanitizeTurnCapture(capture string, turn PreparedTurn) string {
	return capture
}

type fakeSessionNameBuilder struct{}

func (fakeSessionNameBuilder) Build(runID string, role string) string {
	return "session::" + runID + "::" + role
}

func TestNewUsesInjectedDependencies(t *testing.T) {
	sessionOpener := &fakeSessionOpener{}
	launchBuilder := &fakeLauncher{result: "launch-codex"}
	turnProtocol := &fakeProtocol{}

	session, err := New(Config{
		BackendName:         "codex",
		LaunchCommand:       "codex",
		StartupInstruction:  "startup",
		StartupQuietPeriod:  time.Millisecond,
		CapturePollInterval: time.Millisecond,
		Options:             SessionOptions{RunID: "run", Role: "implementer", IdleTimeout: time.Second},
		Dependencies: Dependencies{
			OpenSession:  sessionOpener.Open,
			Launcher:     launchBuilder,
			Protocol:     turnProtocol,
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if sessionOpener.sessionName != "session::run::implementer" {
		t.Fatalf("unexpected session name: %q", sessionOpener.sessionName)
	}
	if len(launchBuilder.commands) != 1 || launchBuilder.commands[0][0] != "codex" {
		t.Fatalf("launcher did not receive command: %#v", launchBuilder.commands)
	}
	recording, ok := sessionOpener.session.pane.(*recordingPane)
	if !ok {
		t.Fatal("expected recording pane")
	}
	if joined := recording.joined(); joined != "launch-codex" {
		t.Fatalf("unexpected launch text: %q", joined)
	}
	if session.SessionName() != "session::run::implementer" {
		t.Fatalf("unexpected session name from session: %q", session.SessionName())
	}
	_ = turnProtocol
}

func TestStartUsesPollingRuntimeAndProtocol(t *testing.T) {
	sessionOpener := &fakeSessionOpener{
		session: &fakeHostSession{
			pane: &recordingPane{
				captures: []string{"ready", "ready", "startup raw", "startup raw"},
			},
		},
	}
	turnProtocol := &fakeProtocol{}

	session, err := New(Config{
		BackendName:         "codex",
		LaunchCommand:       "codex",
		StartupInstruction:  "startup instruction",
		StartupQuietPeriod:  time.Millisecond,
		CapturePollInterval: time.Millisecond,
		Options:             SessionOptions{RunID: "run", Role: "reviewer", IdleTimeout: 2 * time.Second},
		Dependencies: Dependencies{
			OpenSession:  sessionOpener.Open,
			Launcher:     &fakeLauncher{result: "launch"},
			Protocol:     turnProtocol,
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := session.Start("role prompt"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if turnProtocol.rolePrompt != "role prompt" || turnProtocol.startupInstruction != "startup instruction" {
		t.Fatalf("startup prompt was not decorated with expected inputs: %+v", turnProtocol)
	}
	recording := sessionOpener.session.pane.(*recordingPane)
	if !strings.Contains(recording.joined(), "decorated startup") {
		t.Fatalf("expected startup prompt to be sent, got %q", recording.joined())
	}
}

func TestStartNormalizesTimeoutToStartupError(t *testing.T) {
	sessionOpener := &fakeSessionOpener{
		session: &fakeHostSession{
			pane: &recordingPane{
				captures: []string{"ready", "ready", "capture", "capture", "capture"},
			},
		},
	}
	session, err := New(Config{
		BackendName:         "codex",
		LaunchCommand:       "codex",
		StartupInstruction:  "startup instruction",
		StartupQuietPeriod:  time.Millisecond,
		CapturePollInterval: time.Millisecond,
		Options:             SessionOptions{RunID: "run", Role: "reviewer", IdleTimeout: 2 * time.Millisecond},
		Dependencies: Dependencies{
			OpenSession:  sessionOpener.Open,
			Launcher:     &fakeLauncher{result: "launch"},
			Protocol:     &fakeProtocol{incomplete: true},
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = session.Start("role prompt")
	runnerErr, ok := AsRunnerError(err)
	if !ok {
		t.Fatalf("expected runner error, got %v", err)
	}
	if runnerErr.Kind() != RunnerErrorKindStartup {
		t.Fatalf("expected startup error, got %q", runnerErr.Kind())
	}
}

func TestNewRejectsMissingIdleTimeout(t *testing.T) {
	_, err := New(Config{
		BackendName:        "codex",
		LaunchCommand:      "codex",
		StartupInstruction: "startup",
		Options:            SessionOptions{RunID: "run", Role: "implementer"},
		Dependencies: Dependencies{
			OpenSession:  (&fakeSessionOpener{}).Open,
			Launcher:     &fakeLauncher{result: "launch"},
			Protocol:     &fakeProtocol{},
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "IdleTimeout must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTurnAndInjectSideChannelSerializeOutboundWrites(t *testing.T) {
	pane := &interleavingPane{}
	sessionOpener := &fakeSessionOpener{
		session: &fakeHostSession{
			pane: pane,
		},
	}

	session, err := New(Config{
		BackendName:         "codex",
		LaunchCommand:       "codex",
		StartupInstruction:  "startup instruction",
		CapturePollInterval: time.Millisecond,
		Options:             SessionOptions{RunID: "run", Role: "implementer", IdleTimeout: time.Second},
		Dependencies: Dependencies{
			OpenSession:  sessionOpener.Open,
			Launcher:     &fakeLauncher{result: "LAUNCH"},
			Protocol:     &fakeProtocol{},
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, runErr := session.RunTurn("ignored")
		errCh <- runErr
	}()

	time.Sleep(2 * time.Millisecond)
	if err := session.InjectSideChannel("SIDECHANNEL"); err != nil {
		t.Fatalf("InjectSideChannel: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	got := pane.String()
	if !strings.Contains(got, "ignoredSIDECHANNEL") {
		t.Fatalf("outbound writes should remain serialized, got %q", got)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	sessionOpener := &fakeSessionOpener{}
	session, err := New(Config{
		BackendName:        "codex",
		LaunchCommand:      "codex",
		StartupInstruction: "startup instruction",
		Options:            SessionOptions{RunID: "run", Role: "implementer", IdleTimeout: time.Second},
		Dependencies: Dependencies{
			OpenSession:  sessionOpener.Open,
			Launcher:     &fakeLauncher{result: "launch"},
			Protocol:     &fakeProtocol{},
			SessionNames: fakeSessionNameBuilder{},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

var (
	_ tmux.TmuxSessionLike    = (*fakeHostSession)(nil)
	_ tmux.TmuxPaneLike       = (*recordingPane)(nil)
	_ sessionlauncher.Builder = (*fakeLauncher)(nil)
	_ Protocol                = (*fakeProtocol)(nil)
	_ SessionNamer            = fakeSessionNameBuilder{}
)
