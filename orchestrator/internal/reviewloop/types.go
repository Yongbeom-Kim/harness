package reviewloop

import (
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	RoleImplementer = "implementer"
	RoleReviewer    = "reviewer"

	resultStatusApproved     = "approved"
	resultStatusFailed       = "failed"
	resultStatusNonConverged = "non_converged"

	ChannelStatusDelivered         = "delivered"
	ChannelStatusDeliveryFailed    = "delivery_failed"
	ChannelStatusDroppedEmpty      = "dropped_empty"
	ChannelStatusDroppedNotStarted = "dropped_not_started"
	ChannelStatusReaderError       = "reader_error"
)

type Session interface {
	Start(rolePrompt string) error
	RunTurn(prompt string) (TurnResult, error)
	InjectSideChannel(message string) error
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
}

type ChannelConfig struct {
	Path string
}

type ChannelMessage struct {
	Path       string
	Body       string
	ReceivedAt time.Time
}

type ChannelManager interface {
	Messages() <-chan ChannelMessage
	Errors() <-chan error
	Stop() error
	Remove() error
}

type ChannelReaderError struct {
	Path string
	Err  error
}

type RunConfig struct {
	Task                           string
	Implementer                    string
	Reviewer                       string
	MaxIterations                  int
	IdleTimeout                    time.Duration
	Stdout                         io.Writer
	Stderr                         io.Writer
	NewSession                     func(string, SessionOptions) (Session, error)
	NewArtifactWriter              func(string) (ArtifactSink, error)
	NewCommunicationChannelManager func(ChannelConfig) (ChannelManager, error)
	NewRunID                       func() (string, error)
}

type ArtifactSink interface {
	WriteMetadata(RunMetadata) error
	AppendTransition(StateTransition) error
	AppendChannelEvent(ChannelEvent) error
	WriteCapture(name string, text string) error
	WriteResult(RunResult) error
}

type RunMetadata struct {
	RunID              string                     `json:"run_id"`
	Task               string                     `json:"task"`
	Implementer        string                     `json:"implementer"`
	Reviewer           string                     `json:"reviewer"`
	MaxIterations      int                        `json:"max_iterations"`
	IdleTimeoutSeconds int64                      `json:"idle_timeout_seconds"`
	CreatedAt          time.Time                  `json:"created_at"`
	Sessions           map[string]SessionMetadata `json:"sessions"`
}

type SessionMetadata struct {
	Backend         string `json:"backend"`
	TmuxSessionName string `json:"tmux_session_name"`
}

type StateTransition struct {
	At        time.Time `json:"at"`
	State     string    `json:"state"`
	Iteration int       `json:"iteration,omitempty"`
	Role      string    `json:"role,omitempty"`
	Backend   string    `json:"backend,omitempty"`
	Details   string    `json:"details,omitempty"`
}

type RunResult struct {
	RunID               string    `json:"run_id"`
	Status              string    `json:"status"`
	Approved            bool      `json:"approved"`
	Iterations          int       `json:"iterations"`
	FinalImplementation string    `json:"final_implementation,omitempty"`
	Error               string    `json:"error,omitempty"`
	CompletedAt         time.Time `json:"completed_at"`
}

type ChannelEvent struct {
	At              time.Time `json:"at"`
	SourceRole      string    `json:"source_role,omitempty"`
	DestinationRole string    `json:"destination_role,omitempty"`
	ChannelPath     string    `json:"channel_path"`
	Status          string    `json:"status"`
	RawBody         string    `json:"raw_body,omitempty"`
}

type ExitError struct {
	code   int
	silent bool
	err    error
}

func (e *ChannelReaderError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("file channel reader %s failed: %v", e.Path, e.Err)
}

func (e *ChannelReaderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewExitError(code int, silent bool, err error) *ExitError {
	return &ExitError{
		code:   code,
		silent: silent,
		err:    err,
	}
}

func AsExitError(err error) (*ExitError, bool) {
	if err == nil {
		return nil, false
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr, true
	}
	return nil, false
}

func (e *ExitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func (e *ExitError) Unwrap() error {
	return e.err
}

func (e *ExitError) Code() int {
	return e.code
}

func (e *ExitError) Silent() bool {
	return e.silent
}

func defaultRunConfig(cfg RunConfig) RunConfig {
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	return cfg
}

func validateRunConfig(cfg RunConfig) error {
	switch {
	case cfg.Task == "":
		return errors.New("task must not be empty")
	case cfg.Implementer == "":
		return errors.New("implementer backend must not be empty")
	case cfg.Reviewer == "":
		return errors.New("reviewer backend must not be empty")
	case cfg.MaxIterations < 1:
		return errors.New("max iterations must be >= 1")
	case cfg.IdleTimeout <= 0:
		return errors.New("idle timeout must be > 0")
	case cfg.NewSession == nil:
		return errors.New("NewSession must not be nil")
	case cfg.NewArtifactWriter == nil:
		return errors.New("NewArtifactWriter must not be nil")
	case cfg.NewCommunicationChannelManager == nil:
		return errors.New("NewCommunicationChannelManager must not be nil")
	case cfg.NewRunID == nil:
		return errors.New("NewRunID must not be nil")
	default:
		return nil
	}
}
