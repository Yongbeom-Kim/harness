package backend

import (
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type Claude struct{}

const claudeQueuedPromptPrefix = "Do this after all your pending tasks:\n\n"

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

func (Claude) SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, prompt, "Enter")
}

// Claude Code has no native queued-send gesture; this cooperative wrapper asks
// Claude to defer the work itself instead of relying on true CLI queueing.
func (Claude) SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error {
	return sendTextAndKeys(pane, claudeQueuedPromptPrefix+prompt, "Enter")
}
