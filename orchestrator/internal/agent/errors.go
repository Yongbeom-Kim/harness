package agent

import (
	"errors"
	"fmt"
)

const (
	ErrorKindLaunch  = "launch"
	ErrorKindStartup = "startup"
	ErrorKindCapture = "capture"
	ErrorKindClose   = "close"
)

type AgentError struct {
	Kind        string
	SessionName string
	Capture     string
	Err         error
}

func NewAgentError(kind string, sessionName string, capture string, err error) error {
	if err == nil {
		err = errors.New("agent error")
	}
	return &AgentError{
		Kind:        kind,
		SessionName: sessionName,
		Capture:     capture,
		Err:         err,
	}
}

func (e *AgentError) Error() string {
	switch {
	case e.SessionName == "" && e.Kind == "":
		return e.Err.Error()
	case e.SessionName == "":
		return fmt.Sprintf("%s agent error: %v", e.Kind, e.Err)
	case e.Kind == "":
		return fmt.Sprintf("agent session %s error: %v", e.SessionName, e.Err)
	default:
		return fmt.Sprintf("%s agent session %s error: %v", e.Kind, e.SessionName, e.Err)
	}
}

func (e *AgentError) Unwrap() error {
	return e.Err
}
