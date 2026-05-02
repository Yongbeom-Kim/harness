package main

import (
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	roleImplementer = "implementer"
	roleReviewer    = "reviewer"

	resultStatusApproved     = "approved"
	resultStatusFailed       = "failed"
	resultStatusNonConverged = "non_converged"

	channelStatusDelivered         = "delivered"
	channelStatusDeliveryFailed    = "delivery_failed"
	channelStatusDroppedEmpty      = "dropped_empty"
	channelStatusDroppedNotStarted = "dropped_not_started"
	channelStatusReaderError       = "reader_error"
)

type workflowAgent interface {
	Start() error
	WaitUntilReady() error
	SendPrompt(prompt string) error
	Capture() (string, error)
	SessionName() string
	Close() error
}

type channelConfig struct {
	Path string
}

type channelMessage struct {
	Path       string
	Body       string
	ReceivedAt time.Time
}

type channelManager interface {
	Messages() <-chan channelMessage
	Errors() <-chan error
	Stop() error
	Remove() error
}

type channelReaderError struct {
	Path string
	Err  error
}

type artifactSink interface {
	WriteMetadata(runMetadata) error
	AppendTransition(stateTransition) error
	AppendChannelEvent(channelEvent) error
	WriteCapture(name string, text string) error
	WriteResult(runResult) error
}

type runMetadata struct {
	RunID              string                     `json:"run_id"`
	Task               string                     `json:"task"`
	Implementer        string                     `json:"implementer"`
	Reviewer           string                     `json:"reviewer"`
	MaxIterations      int                        `json:"max_iterations"`
	IdleTimeoutSeconds int64                      `json:"idle_timeout_seconds"`
	CreatedAt          time.Time                  `json:"created_at"`
	Sessions           map[string]sessionMetadata `json:"sessions"`
}

type sessionMetadata struct {
	Backend         string `json:"backend"`
	TmuxSessionName string `json:"tmux_session_name"`
}

type stateTransition struct {
	At        time.Time `json:"at"`
	State     string    `json:"state"`
	Iteration int       `json:"iteration,omitempty"`
	Role      string    `json:"role,omitempty"`
	Backend   string    `json:"backend,omitempty"`
	Details   string    `json:"details,omitempty"`
}

type runResult struct {
	RunID               string    `json:"run_id"`
	Status              string    `json:"status"`
	Approved            bool      `json:"approved"`
	Iterations          int       `json:"iterations"`
	FinalImplementation string    `json:"final_implementation,omitempty"`
	Error               string    `json:"error,omitempty"`
	CompletedAt         time.Time `json:"completed_at"`
}

type channelEvent struct {
	At              time.Time `json:"at"`
	SourceRole      string    `json:"source_role,omitempty"`
	DestinationRole string    `json:"destination_role,omitempty"`
	ChannelPath     string    `json:"channel_path"`
	Status          string    `json:"status"`
	RawBody         string    `json:"raw_body,omitempty"`
}

type exitError struct {
	code   int
	silent bool
	err    error
}

func (e *channelReaderError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("file channel reader %s failed: %v", e.Path, e.Err)
}

func (e *channelReaderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newExitError(code int, silent bool, err error) *exitError {
	return &exitError{code: code, silent: silent, err: err}
}

func asExitError(err error) (*exitError, bool) {
	if err == nil {
		return nil, false
	}
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr, true
	}
	return nil, false
}

func (e *exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func (e *exitError) Unwrap() error { return e.err }
func (e *exitError) Code() int     { return e.code }
func (e *exitError) Silent() bool  { return e.silent }

type runConfig struct {
	Task                           string
	Implementer                    string
	Reviewer                       string
	MaxIterations                  int
	IdleTimeout                    time.Duration
	Stdout                         io.Writer
	Stderr                         io.Writer
	NewAgent                       func(string, string) (workflowAgent, error)
	NewArtifactWriter              func(string) (artifactSink, error)
	NewCommunicationChannelManager func(channelConfig) (channelManager, error)
	NewRunID                       func() (string, error)
}
