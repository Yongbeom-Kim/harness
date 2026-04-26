package cli

import (
	"errors"
	"fmt"
	"time"
)

const (
	SessionErrorKindLaunch  = "launch"
	SessionErrorKindStartup = "startup"
	SessionErrorKindTimeout = "timeout"
	SessionErrorKindCapture = "capture"
	SessionErrorKindClose   = "close"
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

type SessionError interface {
	error
	Kind() string
	Capture() string
	SessionName() string
}

type sessionError struct {
	kind        string
	capture     string
	sessionName string
	err         error
}

func NewSessionError(kind string, sessionName string, capture string, err error) error {
	if err == nil {
		err = errors.New("session error")
	}
	return &sessionError{
		kind:        kind,
		capture:     capture,
		sessionName: sessionName,
		err:         err,
	}
}

func AsSessionError(err error) (SessionError, bool) {
	if err == nil {
		return nil, false
	}
	var sessionErr SessionError
	if errors.As(err, &sessionErr) {
		return sessionErr, true
	}
	return nil, false
}

func (e *sessionError) Error() string {
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

func (e *sessionError) Unwrap() error {
	return e.err
}

func (e *sessionError) Kind() string {
	return e.kind
}

func (e *sessionError) Capture() string {
	return e.capture
}

func (e *sessionError) SessionName() string {
	return e.sessionName
}
