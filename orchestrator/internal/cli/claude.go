package cli

func NewClaudeSession(opts SessionOptions) (Session, error) {
	return newPersistentSession(
		"claude",
		"claude",
		nil,
		"You are running inside a persistent Claude tmux session. Reply with exactly Ready. Do not use tools or inspect files. Wait for the next task.",
		nil,
		false,
		opts,
	)
}
