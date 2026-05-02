package main

import (
	"os"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

func main() {
	os.Exit(agentpkg.RunStandalone(os.Args[1:], agentpkg.StandaloneConfig{
		ProgramName:        "tmux_codex",
		DefaultSessionName: "codex",
		SuccessLabel:       "Codex",
		Stdin:              os.Stdin,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		NewAgent: func(sessionName string) agentpkg.StandaloneAgent {
			return agentpkg.NewCodexAgent(sessionName)
		},
		NewLock: func() (agentpkg.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
	}))
}
