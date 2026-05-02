package main

import (
	"os"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

func main() {
	agent := agentpkg.NewClaudeAgent(agentsession.NewSystemStandaloneDependencies())
	os.Exit(agent.RunStandalone(os.Args[1:], agentpkg.StandaloneConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		NewLock: func() (agentsession.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
	}))
}
