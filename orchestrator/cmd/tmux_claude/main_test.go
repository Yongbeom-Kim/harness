package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

type fakeClaudeAgent struct {
	name     string
	startErr error
	readyErr error
	started  bool
	ready    bool
	closed   bool
}

func (a *fakeClaudeAgent) Start() error {
	a.started = true
	return a.startErr
}

func (a *fakeClaudeAgent) WaitUntilReady() error {
	a.ready = true
	return a.readyErr
}

func (a *fakeClaudeAgent) SessionName() string {
	return a.name
}

func (a *fakeClaudeAgent) Close() error {
	a.closed = true
	return nil
}

type fakeClaudeTmuxSession struct {
	name        string
	attachErr   error
	attachCalls int
}

func (s *fakeClaudeTmuxSession) Name() string { return s.name }
func (s *fakeClaudeTmuxSession) Attach(io.Reader, io.Writer, io.Writer) error {
	s.attachCalls++
	return s.attachErr
}
func (s *fakeClaudeTmuxSession) Close() error                        { return nil }
func (s *fakeClaudeTmuxSession) NewPane() (tmux.TmuxPaneLike, error) { return nil, nil }

type stubLock struct{}

func (stubLock) Acquire() error { return nil }
func (stubLock) Release() error { return nil }

func TestRunUsesDefaultClaudeSessionName(t *testing.T) {
	agent := &fakeClaudeAgent{name: "claude-main"}
	var stdout bytes.Buffer
	exitCode := run(nil, nil, &stdout, io.Discard, claudeDeps{
		newLock: func() (dirlock.Locker, error) { return stubLock{}, nil },
		newAgent: func(sessionName string) agentpkg.Agent {
			if sessionName != "claude" {
				t.Fatalf("unexpected default session name: %q", sessionName)
			}
			return agent
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stdout.String(), `Launched Claude in tmux session "claude-main"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunAttachesClaudeSessionAfterReady(t *testing.T) {
	agent := &fakeClaudeAgent{name: "claude-dev"}
	session := &fakeClaudeTmuxSession{name: "claude-dev"}

	exitCode := run([]string{"--attach"}, nil, io.Discard, io.Discard, claudeDeps{
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

func TestRunReturnsClaudeReadinessFailure(t *testing.T) {
	agent := &fakeClaudeAgent{name: "claude", readyErr: errors.New("not ready")}
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, claudeDeps{
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

func TestRunRejectsBlankClaudeSession(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{"--session", "  "}, nil, io.Discard, &stderr, claudeDeps{})
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid --session") {
		t.Fatalf("stderr missing validation error: %q", stderr.String())
	}
}

var _ tmux.TmuxSessionLike = (*fakeClaudeTmuxSession)(nil)
