package cli

func NewClaudeSession(opts SessionOptions) (Session, error) {
	return newPersistentSession(
		"claude",
		"claude",
		nil,
		"You are running inside a persistent Claude tmux session. Acknowledge initialization briefly and wait for the next task.",
		opts,
	)
}
