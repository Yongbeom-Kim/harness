package agent

import (
	"bytes"
	"strings"
	"testing"
	"time"

	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
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
	agent := NewCodexAgent(agentsession.Dependencies{})
	if agent.readyMatcher("Sign in with ChatGPT") {
		t.Fatal("expected sign-in prompt to be treated as not ready")
	}
	if agent.readyMatcher("Press Enter to continue") {
		t.Fatal("expected enter-to-continue prompt to be treated as not ready")
	}
}

func TestCodexReadyMatcherAcceptsKnownCodexPrompts(t *testing.T) {
	agent := NewCodexAgent(agentsession.Dependencies{})
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

func TestNewSessionRejectsUnknownBackend(t *testing.T) {
	_, err := NewSession(agentsession.Dependencies{}, "bad", agentsession.SessionOptions{
		RunID:       "run",
		Role:        "implementer",
		IdleTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected session creation error")
	}
	if !strings.Contains(err.Error(), "unknown backend: bad") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodexAgentRunStandaloneUsesInjectedLaunchDeps(t *testing.T) {
	agent := NewCodexAgent(agentsession.Dependencies{})
	var stderr bytes.Buffer
	exitCode := agent.RunStandalone(nil, StandaloneConfig{
		Stderr: &stderr,
		Lock:   stubLock{},
	})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "session opener is not configured") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

type stubLock struct{}

func (stubLock) Acquire() error { return nil }

func (stubLock) Release() error { return nil }
