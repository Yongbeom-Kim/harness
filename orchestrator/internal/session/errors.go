package session

import (
	"errors"
	"fmt"
)

const (
	ErrorKindLaunch  = "launch"
	ErrorKindStartup = "startup"
	ErrorKindCapture = "capture"
	ErrorKindClose   = "close"
	ErrorKindState   = "state"
	ErrorKindAttach  = "attach"
)

type Error struct {
	Kind        string
	SessionName string
	Capture     string
	Err         error
}

func newError(kind string, sessionName string, capture string, err error) error {
	if err == nil {
		err = errors.New("session error")
	}
	return &Error{
		Kind:        kind,
		SessionName: sessionName,
		Capture:     capture,
		Err:         err,
	}
}

func (e *Error) Error() string {
	switch {
	case e.SessionName == "" && e.Kind == "":
		return e.Err.Error()
	case e.SessionName == "":
		return fmt.Sprintf("%s session error: %v", e.Kind, e.Err)
	case e.Kind == "":
		return fmt.Sprintf("session %s error: %v", e.SessionName, e.Err)
	default:
		return fmt.Sprintf("%s session %s error: %v", e.Kind, e.SessionName, e.Err)
	}
}

func (e *Error) Unwrap() error {
	return e.Err
}
