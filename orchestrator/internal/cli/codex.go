package cli

func NewCodexSession(opts SessionOptions) (Session, error) {
	return newPersistentSession(
		"codex",
		"codex",
		nil,
		"You are running inside a persistent Codex tmux session. Acknowledge initialization briefly and wait for the next task.",
		opts,
	)
}
