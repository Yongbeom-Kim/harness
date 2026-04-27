package cli

import "strings"

func NewCodexSession(opts SessionOptions) (Session, error) {
	return newPersistentSession(
		"codex",
		BuildSourcedLauncher("codex"),
		"You are running inside a persistent Codex tmux session. Reply with exactly Ready. Do not use tools or inspect files. Wait for the next task.",
		func(capture string) bool {
			if strings.Contains(capture, "Sign in with ChatGPT") ||
				strings.Contains(capture, "Press Enter to continue") {
				return false
			}

			if strings.Contains(capture, "OpenAI Codex") && strings.Contains(capture, "\n› ") {
				return true
			}

			return strings.Contains(capture, "Welcome to Codex") && strings.Contains(capture, "\n› ")
		},
		opts,
	)
}
