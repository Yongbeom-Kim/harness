package main

import (
	"os"

	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

func main() {
	os.Exit(agentsession.RunStandalone(os.Args[1:], agentsession.StandaloneConfig{
		ProgramName:        "agent",
		DefaultSessionName: "agent",
		LaunchCommand:      "agent",
		SuccessLabel:       "Agent",
		Stdin:              os.Stdin,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		OpenSession:        agentsession.NewSystemOpenSession,
		Launcher:           agentsession.NewSystemLauncher(),
		NewLock: func() (agentsession.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
	}))
}
