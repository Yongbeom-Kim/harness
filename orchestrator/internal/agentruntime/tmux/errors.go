package tmux

import "fmt"

type TmuxSessionAbsentError struct {
	Message string
	Err     error
}

func (e *TmuxSessionAbsentError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("tmux session absent: %v", e.Err)
	}
	return fmt.Sprintf("tmux session absent: %s", e.Message)
}

func (e *TmuxSessionAbsentError) Unwrap() error { return e.Err }

type NewSessionError struct {
	SessionName string
	Err         error
}

func (e *NewSessionError) Error() string {
	return tmuxCommandErrorMessage("new-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *NewSessionError) Unwrap() error { return e.Err }

type HasSessionError struct {
	SessionName string
	Err         error
}

func (e *HasSessionError) Error() string {
	return tmuxCommandErrorMessage("has-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *HasSessionError) Unwrap() error { return e.Err }

type KillSessionError struct {
	SessionName string
	Err         error
}

func (e *KillSessionError) Error() string {
	return tmuxCommandErrorMessage("kill-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *KillSessionError) Unwrap() error { return e.Err }

type AttachSessionError struct {
	SessionName string
	Err         error
}

func (e *AttachSessionError) Error() string {
	return tmuxCommandErrorMessage("attach-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *AttachSessionError) Unwrap() error { return e.Err }

type SplitWindowError struct {
	Target string
	Err    error
}

func (e *SplitWindowError) Error() string {
	return tmuxCommandErrorMessage("split-window", fmt.Sprintf("for target %q", e.Target), e.Err)
}

func (e *SplitWindowError) Unwrap() error { return e.Err }

type SendKeysError struct {
	Target string
	Keys   []string
	Err    error
}

func (e *SendKeysError) Error() string {
	detail := fmt.Sprintf("for target %q with keys %q", e.Target, e.Keys)
	return tmuxCommandErrorMessage("send-keys", detail, e.Err)
}

func (e *SendKeysError) Unwrap() error { return e.Err }

type CapturePaneError struct {
	Target string
	Err    error
}

func (e *CapturePaneError) Error() string {
	return tmuxCommandErrorMessage("capture-pane", fmt.Sprintf("for target %q", e.Target), e.Err)
}

func (e *CapturePaneError) Unwrap() error { return e.Err }

type KillPaneError struct {
	Target string
	Err    error
}

func (e *KillPaneError) Error() string {
	return tmuxCommandErrorMessage("kill-pane", fmt.Sprintf("for target %q", e.Target), e.Err)
}

func (e *KillPaneError) Unwrap() error { return e.Err }

func tmuxCommandErrorMessage(command string, detail string, err error) string {
	if detail == "" {
		return fmt.Sprintf("tmux %s failed: %v", command, err)
	}
	return fmt.Sprintf("tmux %s %s failed: %v", command, detail, err)
}
