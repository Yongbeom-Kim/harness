package backend

import (
	"fmt"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type LaunchCommandBuilder func(command string, args ...string) (string, error)

type Backend interface {
	DefaultSessionName() string
	Launch(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder) error
	WaitUntilReady(pane tmux.TmuxPaneLike, opts ReadinessOptions) error
	SendPromptNow(pane tmux.TmuxPaneLike, prompt string) error
	SendPromptQueued(pane tmux.TmuxPaneLike, prompt string) error
}

type ReadinessOptions struct {
	ReadyTimeout time.Duration
	QuietPeriod  time.Duration
	PollInterval time.Duration
	Now          func() time.Time
	Sleep        func(time.Duration)
}

type ReadinessError struct {
	Capture string
	Err     error
}

func (e *ReadinessError) Error() string {
	if e.Capture == "" {
		return fmt.Sprintf("backend readiness failed: %v", e.Err)
	}
	return fmt.Sprintf("backend readiness failed after capture %q: %v", e.Capture, e.Err)
}

func (e *ReadinessError) Unwrap() error {
	return e.Err
}

func launchCommand(pane tmux.TmuxPaneLike, buildLaunchCommand LaunchCommandBuilder, command string, args ...string) error {
	if pane == nil {
		return fmt.Errorf("backend launch: nil tmux pane")
	}
	if buildLaunchCommand == nil {
		return fmt.Errorf("backend launch: command builder is not configured")
	}
	launchText, err := buildLaunchCommand(command, args...)
	if err != nil {
		return err
	}
	return sendTextAndKeys(pane, launchText, "Enter")
}

func sendTextAndKeys(pane tmux.TmuxPaneLike, text string, keys ...string) error {
	if pane == nil {
		return fmt.Errorf("backend prompt: nil tmux pane")
	}
	if err := pane.SendText(text); err != nil {
		return err
	}
	for _, key := range keys {
		if err := pane.PressKey(key); err != nil {
			return err
		}
	}
	return nil
}

func waitUntilReady(pane tmux.TmuxPaneLike, ready func(string) bool, opts ReadinessOptions) error {
	if pane == nil {
		return &ReadinessError{Err: fmt.Errorf("backend readiness: nil tmux pane")}
	}
	nowFunc := opts.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	sleepFunc := opts.Sleep
	if sleepFunc == nil {
		sleepFunc = time.Sleep
	}
	readyTimeout := opts.ReadyTimeout
	quietPeriod := opts.QuietPeriod
	pollInterval := opts.PollInterval

	now := nowFunc()
	deadline := now.Add(readyTimeout)
	lastCapture := ""
	lastChange := now
	firstCapture := true

	for {
		capture, err := pane.Capture()
		if err != nil {
			return &ReadinessError{Capture: lastCapture, Err: err}
		}
		now = nowFunc()
		if firstCapture || capture != lastCapture {
			lastCapture = capture
			lastChange = now
			firstCapture = false
		}

		if ready(capture) && now.Sub(lastChange) >= quietPeriod {
			return nil
		}
		if now.After(deadline) {
			return &ReadinessError{
				Capture: lastCapture,
				Err:     fmt.Errorf("session did not become ready within %s", readyTimeout),
			}
		}

		sleepFunc(pollInterval)
	}
}
