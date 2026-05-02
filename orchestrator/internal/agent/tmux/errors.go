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

func (e *NewSessionError) Unwrap() error   { return e.Err }
func (e *NewSessionError) Command() string { return "new-session" }

type HasSessionError struct {
	SessionName string
	Err         error
}

func (e *HasSessionError) Error() string {
	return tmuxCommandErrorMessage("has-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *HasSessionError) Unwrap() error   { return e.Err }
func (e *HasSessionError) Command() string { return "has-session" }

type KillSessionError struct {
	SessionName string
	Err         error
}

func (e *KillSessionError) Error() string {
	return tmuxCommandErrorMessage("kill-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *KillSessionError) Unwrap() error   { return e.Err }
func (e *KillSessionError) Command() string { return "kill-session" }

type AttachSessionError struct {
	SessionName string
	Err         error
}

func (e *AttachSessionError) Error() string {
	return tmuxCommandErrorMessage("attach-session", fmt.Sprintf("for session %q", e.SessionName), e.Err)
}

func (e *AttachSessionError) Unwrap() error   { return e.Err }
func (e *AttachSessionError) Command() string { return "attach-session" }

type SplitWindowError struct {
	Target string
	Err    error
}

func (e *SplitWindowError) Error() string {
	return tmuxCommandErrorMessage("split-window", fmt.Sprintf("for target %q", e.Target), e.Err)
}

func (e *SplitWindowError) Unwrap() error   { return e.Err }
func (e *SplitWindowError) Command() string { return "split-window" }

type LoadBufferError struct {
	BufferName string
	Err        error
}

func (e *LoadBufferError) Error() string {
	return tmuxCommandErrorMessage("load-buffer", fmt.Sprintf("for buffer %q", e.BufferName), e.Err)
}

func (e *LoadBufferError) Unwrap() error   { return e.Err }
func (e *LoadBufferError) Command() string { return "load-buffer" }

type PasteBufferError struct {
	Target     string
	BufferName string
	Err        error
}

func (e *PasteBufferError) Error() string {
	detail := fmt.Sprintf("for buffer %q to target %q", e.BufferName, e.Target)
	return tmuxCommandErrorMessage("paste-buffer", detail, e.Err)
}

func (e *PasteBufferError) Unwrap() error   { return e.Err }
func (e *PasteBufferError) Command() string { return "paste-buffer" }

type SendKeysError struct {
	Target string
	Keys   []string
	Err    error
}

func (e *SendKeysError) Error() string {
	detail := fmt.Sprintf("for target %q with keys %q", e.Target, e.Keys)
	return tmuxCommandErrorMessage("send-keys", detail, e.Err)
}

func (e *SendKeysError) Unwrap() error   { return e.Err }
func (e *SendKeysError) Command() string { return "send-keys" }

type CapturePaneError struct {
	Target string
	Err    error
}

func (e *CapturePaneError) Error() string {
	return tmuxCommandErrorMessage("capture-pane", fmt.Sprintf("for target %q", e.Target), e.Err)
}

func (e *CapturePaneError) Unwrap() error   { return e.Err }
func (e *CapturePaneError) Command() string { return "capture-pane" }

func tmuxCommandErrorMessage(command string, detail string, err error) string {
	if detail == "" {
		return fmt.Sprintf("tmux %s failed: %v", command, err)
	}
	return fmt.Sprintf("tmux %s %s failed: %v", command, detail, err)
}
