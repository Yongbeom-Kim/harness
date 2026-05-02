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

type fakeCodexAgent struct {
	name     string
	startErr error
	readyErr error
	started  bool
	ready    bool
	closed   bool
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

func (a *fakeCodexAgent) Close() error {
	a.closed = true
	return nil
}

type fakeCodexTmuxSession struct {
	name        string
	attachErr   error
	attachCalls int
}

func (s *fakeCodexTmuxSession) Name() string { return s.name }
func (s *fakeCodexTmuxSession) Attach(io.Reader, io.Writer, io.Writer) error {
	s.attachCalls++
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

var _ tmux.TmuxSessionLike = (*fakeCodexTmuxSession)(nil)
