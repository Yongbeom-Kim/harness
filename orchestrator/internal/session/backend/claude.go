package backend

import (
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/tmux"
)

type Claude struct{}

func (Claude) DefaultSessionName() string { return "claude" }

func (Claude) Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error {
	return launchCommand(pane, buildLaunchCommand, "claude")
}

func (Claude) WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error {
	return waitUntilReady(pane, claudeReady, opts)
}

func claudeReady(capture string) bool {
	lower := strings.ToLower(capture)
	if strings.Contains(lower, "press enter to continue") ||
		strings.Contains(lower, "log in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "do you trust") {
		return false
	}
	return strings.TrimSpace(capture) != ""
}

func (Claude) SendPrompt(pane tmux.TmuxPaneLike, prompt string) error {
	return sendPrompt(pane, prompt)
}
