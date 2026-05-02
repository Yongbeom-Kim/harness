package main

import (
	"os"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

func main() {
	os.Exit(agentpkg.RunStandalone(os.Args[1:], agentpkg.StandaloneConfig{
		ProgramName:        "tmux_claude",
		DefaultSessionName: "claude",
		SuccessLabel:       "Claude",
		Stdin:              os.Stdin,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		NewAgent: func(sessionName string) agentpkg.StandaloneAgent {
			return agentpkg.NewClaudeAgent(sessionName)
		},
		NewLock: func() (agentpkg.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
	}))
}
