package session

import (
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

func launchSession(openSession func(string) (tmux.TmuxSessionLike, error), build launcher.Builder, sessionName string, command string, args ...string) (tmux.TmuxSessionLike, tmux.TmuxPaneLike, error) {
	session, err := openSession(sessionName)
	if err != nil {
		return nil, nil, err
	}

	pane, err := session.NewPane()
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}

	launchCommand := build.Build(command, args...)
	if launchCommand != "" {
		if err := pane.SendText(launchCommand); err != nil {
			_ = session.Close()
			return nil, nil, err
		}
	}

	return session, pane, nil
}
