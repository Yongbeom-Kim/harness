package backend

import (
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type Cursor struct{}

func (Cursor) DefaultSessionName() string { return "cursor" }

func (Cursor) Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error {
	return launchCommand(pane, buildLaunchCommand, "agent")
}

func (Cursor) WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error {
	return waitUntilReady(pane, cursorReady, opts)
}

func cursorReady(capture string) bool {
	lower := strings.ToLower(capture)
	if strings.Contains(lower, "log in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "authenticate") ||
		strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "trust") ||
		strings.Contains(lower, "setup") ||
		strings.Contains(lower, "press enter to continue") {
		return false
	}
	return strings.Contains(capture, "Cursor Agent")
}

func (Cursor) SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, prompt, "Enter", "Enter")
}

func (Cursor) SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, prompt, "Enter")
}
