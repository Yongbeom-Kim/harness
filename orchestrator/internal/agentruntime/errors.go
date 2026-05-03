package agentruntime

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
)

type Error struct {
	Kind        string
	SessionName string
	Capture     string
	Err         error
}

func newError(kind string, sessionName string, capture string, err error) error {
	if err == nil {
		err = errors.New("runtime error")
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
		return fmt.Sprintf("%s runtime error: %v", e.Kind, e.Err)
	case e.Kind == "":
		return fmt.Sprintf("runtime %s error: %v", e.SessionName, e.Err)
	default:
		return fmt.Sprintf("%s runtime %s error: %v", e.Kind, e.SessionName, e.Err)
	}
}

func (e *Error) Unwrap() error {
	return e.Err
}
