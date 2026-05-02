package agent

import (
	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
)

type ClaudeAgent struct {
	sessionDeps        agentsession.Dependencies
	name               string
	defaultSessionName string
	launchCommand      string
	successLabel       string
	startupInstruction string
	readyMatcher       func(string) bool
}

func NewClaudeAgent(sessionDeps agentsession.Dependencies) ClaudeAgent {
	return ClaudeAgent{
		sessionDeps:        sessionDeps,
		name:               "claude",
		defaultSessionName: "claude",
		launchCommand:      "claude",
		successLabel:       "Claude",
		startupInstruction: "You are running inside a persistent Claude tmux session. Reply with exactly Ready. Do not use tools or inspect files. Wait for the next task.",
	}
}

func (a ClaudeAgent) NewSession(opts agentsession.SessionOptions) (agentsession.Session, error) {
	return newSession(a.sessionDeps, a.name, a.launchCommand, a.startupInstruction, a.readyMatcher, opts)
}

func (a ClaudeAgent) RunStandalone(args []string, cfg StandaloneConfig) int {
	return runStandalone(args, a.sessionDeps, a.name, a.defaultSessionName, a.launchCommand, a.successLabel, cfg)
}
