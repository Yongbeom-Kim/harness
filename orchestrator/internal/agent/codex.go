package agent

import (
	"strings"

	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
)

type CodexAgent struct {
	sessionDeps        agentsession.Dependencies
	name               string
	defaultSessionName string
	launchCommand      string
	successLabel       string
	startupInstruction string
	readyMatcher       func(string) bool
}

func NewCodexAgent(sessionDeps agentsession.Dependencies) CodexAgent {
	return CodexAgent{
		sessionDeps:        sessionDeps,
		name:               "codex",
		defaultSessionName: "codex",
		launchCommand:      "codex",
		successLabel:       "Codex",
		startupInstruction: "You are running inside a persistent Codex tmux session. Reply with exactly Ready. Do not use tools or inspect files. Wait for the next task.",
		readyMatcher:       codexReadyMatcher,
	}
}

func (a CodexAgent) NewSession(opts agentsession.SessionOptions) (agentsession.Session, error) {
	return newSession(a.sessionDeps, a.name, a.launchCommand, a.startupInstruction, a.readyMatcher, opts)
}

func (a CodexAgent) RunStandalone(args []string, cfg StandaloneConfig) int {
	return runStandalone(args, a.sessionDeps, a.name, a.defaultSessionName, a.launchCommand, a.successLabel, cfg)
}

func codexReadyMatcher(capture string) bool {
	if strings.Contains(capture, "Sign in with ChatGPT") ||
		strings.Contains(capture, "Press Enter to continue") {
		return false
	}

	if strings.Contains(capture, "OpenAI Codex") && strings.Contains(capture, "\n› ") {
		return true
	}

	return strings.Contains(capture, "Welcome to Codex") && strings.Contains(capture, "\n› ")
}
