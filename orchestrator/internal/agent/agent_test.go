package agent

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
)

func TestKnownBackendNamesReturnsCopy(t *testing.T) {
	names := KnownBackendNames()
	if len(names) != 2 {
		t.Fatalf("unexpected backend count: %d", len(names))
	}

	names[0] = "mutated"
	again := KnownBackendNames()
	if again[0] == "mutated" {
		t.Fatal("expected KnownBackendNames to return a copy")
	}
}

func TestCodexReadyMatcherRejectsInteractiveLoginPrompts(t *testing.T) {
	agent := NewCodexAgent("codex-test")
	if agent.readyMatcher("Sign in with ChatGPT") {
		t.Fatal("expected sign-in prompt to be treated as not ready")
	}
	if agent.readyMatcher("Press Enter to continue") {
		t.Fatal("expected enter-to-continue prompt to be treated as not ready")
	}
}

func TestCodexReadyMatcherAcceptsKnownCodexPrompts(t *testing.T) {
	agent := NewCodexAgent("codex-test")
	if !agent.readyMatcher("OpenAI Codex\n› ") {
		t.Fatal("expected OpenAI Codex prompt to be treated as ready")
	}
	if !agent.readyMatcher("Welcome to Codex\n› ") {
		t.Fatal("expected Welcome to Codex prompt to be treated as ready")
	}
}

func TestValidateBackendRejectsUnknownBackend(t *testing.T) {
	err := ValidateBackend("bad")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "unknown backend: bad") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentErrorCarriesKindSessionAndCapture(t *testing.T) {
	err := NewAgentError(ErrorKindTimeout, "session-name", "capture text", io.ErrUnexpectedEOF)
	agentErr, ok := AsAgentError(err)
	if !ok {
		t.Fatalf("expected agent error, got %v", err)
	}
	if agentErr.Kind != ErrorKindTimeout || agentErr.SessionName != "session-name" || agentErr.Capture != "capture text" {
		t.Fatalf("unexpected agent error: %+v", agentErr)
	}
	if !strings.Contains(err.Error(), "timeout agent session session-name error") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestBuildLaunchCommandSourcesAgentrcAndQuotesArgs(t *testing.T) {
	got := buildLaunchCommand("codex", "one two", "it's")
	if !strings.Contains(got, `. "$HOME/.agentrc"`) {
		t.Fatalf("launch command should source agentrc: %q", got)
	}
	if !strings.Contains(got, "one two") || !strings.Contains(got, `"'"'`) {
		t.Fatalf("launch command did not quote args: %q", got)
	}
}

func TestCodexAgentStartWaitSendCaptureClose(t *testing.T) {
	pane := &recordingPane{captures: []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}}
	session := &fakeTmuxSession{name: "codex-session", pane: pane}
	agent := NewCodexAgent("codex-session")
	agent.openSession = func(sessionName string) (tmux.TmuxSessionLike, error) {
		if sessionName != "codex-session" {
			t.Fatalf("unexpected session name: %q", sessionName)
		}
		return session, nil
	}
	agent.startupQuietPeriod = time.Millisecond
	agent.capturePollInterval = time.Millisecond

	if err := agent.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := agent.WaitUntilReady(); err != nil {
		t.Fatalf("WaitUntilReady: %v", err)
	}
	if err := agent.SendPrompt("do work"); err != nil {
		t.Fatalf("SendPrompt: %v", err)
	}
	capture, err := agent.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if capture != "OpenAI Codex\n› " {
		t.Fatalf("unexpected capture: %q", capture)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if session.closeCalls != 1 {
		t.Fatalf("expected one close call, got %d", session.closeCalls)
	}
	sent := pane.joined()
	if !strings.Contains(sent, "bash -lc") || !strings.Contains(sent, "do work") {
		t.Fatalf("unexpected sent text: %q", sent)
	}
}

func TestClaudeAgentStartWaitSendCaptureClose(t *testing.T) {
	pane := &recordingPane{captures: []string{"Claude ready", "Claude ready"}}
	session := &fakeTmuxSession{name: "claude-session", pane: pane}
	agent := NewClaudeAgent("claude-session")
	agent.openSession = func(string) (tmux.TmuxSessionLike, error) { return session, nil }
	agent.startupQuietPeriod = time.Millisecond
	agent.capturePollInterval = time.Millisecond

	if err := agent.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := agent.WaitUntilReady(); err != nil {
		t.Fatalf("WaitUntilReady: %v", err)
	}
	if err := agent.SendPrompt("review"); err != nil {
		t.Fatalf("SendPrompt: %v", err)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !strings.Contains(pane.joined(), "review") {
		t.Fatalf("expected prompt to be sent, got %q", pane.joined())
	}
}

func TestClaudeReadyMatcherRejectsInteractiveBlockers(t *testing.T) {
	agent := NewClaudeAgent("claude-test")
	for _, capture := range []string{"Press Enter to continue", "Log in to Claude", "Do you trust this folder?"} {
		if agent.readyMatcher(capture) {
			t.Fatalf("expected %q to be treated as not ready", capture)
		}
	}
	if !agent.readyMatcher("Claude ready") {
		t.Fatal("expected stable Claude output to be treated as ready")
	}
}

type stubLock struct{}

func (stubLock) Acquire() error { return nil }

func (stubLock) Release() error { return nil }

type fakeTmuxSession struct {
	name       string
	pane       tmux.TmuxPaneLike
	closeCalls int
}

func (s *fakeTmuxSession) Name() string                                 { return s.name }
func (s *fakeTmuxSession) AttachTarget() string                         { return s.name }
func (s *fakeTmuxSession) Attach(io.Reader, io.Writer, io.Writer) error { return nil }
func (s *fakeTmuxSession) Close() error {
	s.closeCalls++
	return nil
}
func (s *fakeTmuxSession) NewPane() (tmux.TmuxPaneLike, error) { return s.pane, nil }

type recordingPane struct {
	mu           sync.Mutex
	calls        []string
	captures     []string
	captureIndex int
}

func (p *recordingPane) SendText(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, text)
	return nil
}

func (p *recordingPane) Capture() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
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

func (p *recordingPane) Target() string { return "%1" }

func (p *recordingPane) joined() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.calls, "")
}

var (
	_ tmux.TmuxSessionLike = (*fakeTmuxSession)(nil)
	_ tmux.TmuxPaneLike    = (*recordingPane)(nil)
)
