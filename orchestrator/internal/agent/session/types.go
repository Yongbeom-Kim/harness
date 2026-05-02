package session

import (
	"time"
)

type Session interface {
	Start(rolePrompt string) error
	RunTurn(prompt string) (TurnResult, error)
	InjectSideChannel(message string) error
	SessionName() string
	Close() error
}

type SessionNamer interface {
	Build(runID string, role string) string
}

type SessionOptions struct {
	RunID       string
	Role        string
	IdleTimeout time.Duration
}

type TurnResult struct {
	Output     string
	RawCapture string
}
