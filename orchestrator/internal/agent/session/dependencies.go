package session

import (
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

func NewSystemOpenSession(sessionName string) (tmux.TmuxSessionLike, error) {
	return tmux.NewTmuxSession(sessionName)
}

func NewSystemLauncher() launcher.Builder {
	return launcher.NewSourcedShellBuilder()
}

func NewSystemStandaloneDependencies() Dependencies {
	return Dependencies{
		OpenSession: NewSystemOpenSession,
		Launcher:    NewSystemLauncher(),
	}
}

func NewSystemDependencies(sessionNames SessionNamer) Dependencies {
	deps := NewSystemStandaloneDependencies()
	deps.Protocol = NewPromiseDoneProtocol()
	deps.SessionNames = sessionNames
	return deps
}
