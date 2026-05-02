package agent

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
)

type standaloneFakeAgent struct {
	name     string
	startErr error
	readyErr error
	started  bool
	ready    bool
	closed   bool
}

func (a *standaloneFakeAgent) Start() error {
	a.started = true
	return a.startErr
}

func (a *standaloneFakeAgent) WaitUntilReady() error {
	a.ready = true
	return a.readyErr
}

func (a *standaloneFakeAgent) SessionName() string {
	return a.name
}

func (a *standaloneFakeAgent) Close() error {
	a.closed = true
	return nil
}

type standaloneFakeTmuxSession struct {
	name        string
	attachErr   error
	attachCalls int
}

func (s *standaloneFakeTmuxSession) Name() string         { return s.name }
func (s *standaloneFakeTmuxSession) AttachTarget() string { return s.name }
func (s *standaloneFakeTmuxSession) Attach(io.Reader, io.Writer, io.Writer) error {
	s.attachCalls++
	return s.attachErr
}
func (s *standaloneFakeTmuxSession) Close() error                        { return nil }
func (s *standaloneFakeTmuxSession) NewPane() (tmux.TmuxPaneLike, error) { return nil, nil }

func TestParseStandaloneArgsPreservesSessionAndAttachFlags(t *testing.T) {
	var stderr bytes.Buffer
	parsed, exitCode, ok := parseStandaloneArgs([]string{"--session", "dev", "--attach"}, StandaloneConfig{
		ProgramName:        "tmux_codex",
		DefaultSessionName: "codex",
		Stderr:             &stderr,
	})
	if !ok || exitCode != 0 {
		t.Fatalf("parse failed: exit=%d stderr=%q", exitCode, stderr.String())
	}
	if parsed.sessionName != "dev" || !parsed.attach {
		t.Fatalf("unexpected parse result: %+v", parsed)
	}
}

func TestStandaloneLaunchWaitsUntilReady(t *testing.T) {
	var stdout bytes.Buffer
	agent := &standaloneFakeAgent{name: "codex-dev"}
	exitCode := RunStandalone(nil, StandaloneConfig{
		ProgramName:        "tmux_codex",
		DefaultSessionName: "codex",
		SuccessLabel:       "Codex",
		Stdout:             &stdout,
		Stderr:             io.Discard,
		Lock:               stubLock{},
		NewAgent:           func(string) StandaloneAgent { return agent },
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

func TestStandaloneAttachUsesOpenSessionAfterReady(t *testing.T) {
	agent := &standaloneFakeAgent{name: "codex-dev"}
	session := &standaloneFakeTmuxSession{name: "codex-dev"}
	exitCode := RunStandalone([]string{"--attach"}, StandaloneConfig{
		ProgramName:        "tmux_codex",
		DefaultSessionName: "codex",
		SuccessLabel:       "Codex",
		Stdout:             io.Discard,
		Stderr:             io.Discard,
		Lock:               stubLock{},
		NewAgent:           func(string) StandaloneAgent { return agent },
		OpenSession:        func(string) (tmux.TmuxSessionLike, error) { return session, nil },
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if session.attachCalls != 1 {
		t.Fatalf("expected attach once, got %d", session.attachCalls)
	}
}

func TestStandaloneReturnsReadinessFailure(t *testing.T) {
	var stderr bytes.Buffer
	agent := &standaloneFakeAgent{name: "codex", readyErr: errors.New("not ready")}
	exitCode := RunStandalone(nil, StandaloneConfig{
		ProgramName:        "tmux_codex",
		DefaultSessionName: "codex",
		SuccessLabel:       "Codex",
		Stderr:             &stderr,
		Lock:               stubLock{},
		NewAgent: func(string) StandaloneAgent {
			return agent
		},
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
