package cli

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/tmux"
)

// fakeTmuxSessionForTest is a [tmux.TmuxSessionLike] for tests that build a [persistentSession] without
// [tmux.NewTmuxSession] (no real tmux; Close is a no-op).
type fakeTmuxSessionForTest struct{ name string }

func (f *fakeTmuxSessionForTest) Name() string         { return f.name }
func (f *fakeTmuxSessionForTest) AttachTarget() string { return f.name }
func (f *fakeTmuxSessionForTest) Attach(io.Reader, io.Writer, io.Writer) error {
	return nil
}
func (f *fakeTmuxSessionForTest) Close() error { return nil }
func (f *fakeTmuxSessionForTest) NewPane() (tmux.TmuxPaneLike, error) {
	return fakeTmuxPaneForTest{}, nil
}

type fakeTmuxPaneForTest struct{}

func (fakeTmuxPaneForTest) SendText(string) error    { return nil }
func (fakeTmuxPaneForTest) Capture() (string, error) { return "", nil }
func (fakeTmuxPaneForTest) Target() string           { return "fake" }

// testPane is a [tmux.TmuxPaneLike] for [persistentSession] unit tests.
// Before the first [testPane.SendText], [testPane.Capture] emulates a quiet prompt for [persistentSession.waitForReady].
// After the first [testPane.SendText], it follows either a custom [testPane.runTurn] (optional) or the two-step default used by
// [TestRunTurnWaitsForDoneAfterPromptBoundary].
type testPane struct {
	ready     string
	readyUsed bool
	numZero   int
	maxReady  int

	sent  string
	phase int
	// if set, all captures after the first send use this
	runTurn func(p *testPane) string
}

func (p *testPane) SendText(text string) error {
	if !p.readyUsed {
		p.readyUsed = true
	}
	p.sent = text
	return nil
}

func (p *testPane) Capture() (string, error) {
	if !p.readyUsed {
		p.numZero++
		if p.maxReady > 0 && p.numZero > p.maxReady {
			return "", nil
		}
		if p.ready != "" {
			return p.ready, nil
		}
		return "> ", nil
	}
	if p.runTurn != nil {
		return p.runTurn(p), nil
	}
	p.phase++
	if p.phase == 1 {
		return "> " + p.sent + "\n", nil
	}
	return "> " + p.sent + "\nUse Token = \"v2\"\n<promise>done</promise>\n", nil
}

func (p *testPane) Target() string { return "%0" }

func TestRunTurnWaitsForDoneAfterPromptBoundary(t *testing.T) {
	pane := &testPane{ready: "> "}
	sess := &fakeTmuxSessionForTest{name: "iwr-test-reviewer"}
	s := &persistentSession{
		tmuxSession: sess,
		pane:        pane,
		backendName: "codex",
		idleTimeout: time.Second,
	}
	userPrompt := "Review this draft:\npackage demo\nconst Token = \"v1\"\n<promise>done</promise>\n"

	result, err := s.RunTurn(userPrompt)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	wantOut := "Use Token = \"v2\"\n<promise>done</promise>\n"
	if result.Output != wantOut {
		t.Fatalf("output: want %q got %q", wantOut, result.Output)
	}
	decorated := s.decorateTurnPrompt(userPrompt)
	wantRaw := decorated + "\nUse Token = \"v2\"\n<promise>done</promise>\n"
	if result.RawCapture != wantRaw {
		t.Fatalf("raw: want %q got %q", wantRaw, result.RawCapture)
	}
}

func TestBuildSourcedLauncherPreservesAgentrcPath(t *testing.T) {
	launcher := BuildSourcedLauncher("codex", "--model", "gpt-5")

	if strings.Contains(launcher, "export PATH=") {
		t.Fatalf("launcher should not overwrite PATH: %s", launcher)
	}
	if !strings.Contains(launcher, `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo;`) {
		t.Fatalf("launcher should source .agentrc before invoking the backend command: %s", launcher)
	}
	if strings.Contains(launcher, `; exec `) {
		t.Fatalf("launcher should not use exec because that bypasses shell functions from .agentrc: %s", launcher)
	}
	if !strings.Contains(launcher, `'codex'`) {
		t.Fatalf("launcher should reference backend command: %s", launcher)
	}
}

func TestStartSucceedsWithoutReset(t *testing.T) {
	calls := 0
	pane := &testPane{ready: "> "}
	pane.runTurn = func(p *testPane) string {
		calls++
		if calls == 1 {
			return "> " + p.sent + "\n"
		}
		return "> " + p.sent + "\nready\n<promise>done</promise>\n"
	}

	sess := &fakeTmuxSessionForTest{name: "iwr-test-implementer"}
	s := &persistentSession{
		tmuxSession:        sess,
		pane:               pane,
		backendName:        "codex",
		idleTimeout:        time.Second,
		startupInstruction: "Acknowledge initialization.",
		readyMatcher:       func(string) bool { return true },
	}
	rolePrompt := "You are the implementer."
	decoratedPrompt := s.decorateStartupPrompt(rolePrompt)
	if decoratedPrompt == "" {
		t.Fatal("expected decorated prompt")
	}

	if err := s.Start(rolePrompt); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(pane.sent, decoratedPrompt) {
		t.Fatalf("sent prompt should contain decorated startup prompt: %q", pane.sent)
	}
}

func TestSessionNameIsOwningTmuxSessionName(t *testing.T) {
	const want = "iwr-test-runid-implementer"
	sess := &fakeTmuxSessionForTest{name: want}
	s := &persistentSession{tmuxSession: sess, pane: &testPane{ready: "> "}}
	if s.SessionName() != want {
		t.Fatalf("SessionName: want %q got %q", want, s.SessionName())
	}
}

func TestCloseIsIdempotentForTestSession(t *testing.T) {
	pane := &testPane{ready: "> "}
	sess := &fakeTmuxSessionForTest{name: "iwr-gone"}
	s := &persistentSession{tmuxSession: sess, pane: pane, backendName: "x"}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
