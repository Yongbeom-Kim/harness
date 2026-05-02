package agent

import (
	"fmt"
	"io"
	"strings"

	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
)

type Agent interface {
	NewSession(opts agentsession.SessionOptions) (agentsession.Session, error)
	RunStandalone(args []string, cfg StandaloneConfig) int
}

type StandaloneConfig struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Lock    agentsession.Locker
	NewLock func() (agentsession.Locker, error)
}

func KnownBackendNames() []string {
	return []string{"codex", "claude"}
}

func ValidateBackend(name string) error {
	switch name {
	case "codex", "claude":
		return nil
	}
	return UnknownBackendError(name)
}

func UnknownBackendError(name string) error {
	return fmt.Errorf("unknown backend: %s (expected %s)", name, joinBackendNames(KnownBackendNames()))
}

func NewSession(sessionDeps agentsession.Dependencies, name string, opts agentsession.SessionOptions) (agentsession.Session, error) {
	switch name {
	case "codex":
		return NewCodexAgent(sessionDeps).NewSession(opts)
	case "claude":
		return NewClaudeAgent(sessionDeps).NewSession(opts)
	default:
		return nil, UnknownBackendError(name)
	}
}

func newSession(deps agentsession.Dependencies, backendName string, launchCommand string, startupInstruction string, readyMatcher func(string) bool, opts agentsession.SessionOptions) (agentsession.Session, error) {
	return agentsession.New(agentsession.Config{
		BackendName:        backendName,
		LaunchCommand:      launchCommand,
		StartupInstruction: startupInstruction,
		ReadyMatcher:       readyMatcher,
		Options:            opts,
		Dependencies:       deps,
	})
}

func runStandalone(args []string, deps agentsession.Dependencies, programName string, defaultSessionName string, launchCommand string, successLabel string, cfg StandaloneConfig) int {
	return agentsession.RunStandalone(args, agentsession.StandaloneConfig{
		ProgramName:        programName,
		DefaultSessionName: defaultSessionName,
		LaunchCommand:      launchCommand,
		SuccessLabel:       successLabel,
		Stdin:              cfg.Stdin,
		Stdout:             cfg.Stdout,
		Stderr:             cfg.Stderr,
		OpenSession:        deps.OpenSession,
		Launcher:           deps.Launcher,
		Lock:               cfg.Lock,
		NewLock:            cfg.NewLock,
	})
}

func joinBackendNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
	}
}
