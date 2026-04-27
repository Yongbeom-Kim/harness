package cli

import (
	"errors"
	"fmt"
	"time"
)

const (
	RunnerErrorKindLaunch  = "launch"
	RunnerErrorKindStartup = "startup"
	RunnerErrorKindTimeout = "timeout"
	RunnerErrorKindCapture = "capture"
	RunnerErrorKindClose   = "close"
)

type Session interface {
	Start(rolePrompt string) error
	RunTurn(prompt string) (TurnResult, error)
	SessionName() string
	Close() error
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

type RunnerError interface {
	error
	Kind() string
	Capture() string
	SessionName() string
}

type runnerError struct {
	kind        string
	capture     string
	sessionName string
	err         error
}

func NewRunnerError(kind string, sessionName string, capture string, err error) error {
	if err == nil {
		err = errors.New("runner error")
	}
	return &runnerError{
		kind:        kind,
		capture:     capture,
		sessionName: sessionName,
		err:         err,
	}
}

func AsRunnerError(err error) (RunnerError, bool) {
	if err == nil {
		return nil, false
	}
	var runnerErr RunnerError
	if errors.As(err, &runnerErr) {
		return runnerErr, true
	}
	return nil, false
}

func (e *runnerError) Error() string {
	switch {
	case e.sessionName == "" && e.kind == "":
		return e.err.Error()
	case e.sessionName == "":
		return fmt.Sprintf("%s session error: %v", e.kind, e.err)
	case e.kind == "":
		return fmt.Sprintf("session %s error: %v", e.sessionName, e.err)
	default:
		return fmt.Sprintf("%s session %s error: %v", e.kind, e.sessionName, e.err)
	}
}

func (e *runnerError) Unwrap() error {
	return e.err
}

func (e *runnerError) Kind() string {
	return e.kind
}

func (e *runnerError) Capture() string {
	return e.capture
}

func (e *runnerError) SessionName() string {
	return e.sessionName
}
