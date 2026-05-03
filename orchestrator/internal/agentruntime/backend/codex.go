package backend

import (
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type Codex struct{}

func (Codex) DefaultSessionName() string { return "codex" }

func (Codex) Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error {
	return launchCommand(pane, buildLaunchCommand, "codex")
}

func (Codex) WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error {
	return waitUntilReady(pane, codexReady, opts)
}

func codexReady(capture string) bool {
	if strings.Contains(capture, "Sign in with ChatGPT") ||
		strings.Contains(capture, "Press Enter to continue") {
		return false
	}

	if strings.Contains(capture, "OpenAI Codex") && strings.Contains(capture, "\n› ") {
		return true
	}

	return strings.Contains(capture, "Welcome to Codex") && strings.Contains(capture, "\n› ")
}

func (Codex) SendPrompt(pane tmux.TmuxPaneLike, prompt string) error {
	return sendPrompt(pane, prompt)
}
