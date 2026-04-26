package cli

import "fmt"

func ValidateBackend(name string) error {
	switch name {
	case "codex", "claude":
		return nil
	default:
		return fmt.Errorf("unknown backend: %s (expected codex or claude)", name)
	}
}

func NewSession(name string, opts SessionOptions) (Session, error) {
	switch name {
	case "codex":
		return NewCodexSession(opts)
	case "claude":
		return NewClaudeSession(opts)
	default:
		return nil, fmt.Errorf("unknown backend: %s (expected codex or claude)", name)
	}
}
