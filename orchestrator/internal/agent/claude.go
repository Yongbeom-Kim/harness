package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	agentshell "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/shell"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
)

type ClaudeAgent struct {
	sessionName         string
	session             tmux.TmuxSessionLike
	pane                tmux.TmuxPaneLike
	openSession         func(string) (tmux.TmuxSessionLike, error)
	launchCommand       string
	readyMatcher        func(string) bool
	startupReadyTimeout time.Duration
	startupQuietPeriod  time.Duration
	capturePollInterval time.Duration
	closed              bool
	sendMu              sync.Mutex
}

func NewClaudeAgent(sessionName string) *ClaudeAgent {
	return &ClaudeAgent{
		sessionName:   sessionName,
		openSession:   newSystemTmuxSession,
		launchCommand: "claude",
		readyMatcher:  claudeReadyMatcher,
	}
}

func (a *ClaudeAgent) Start() error {
	if a == nil {
		return fmt.Errorf("nil ClaudeAgent")
	}
	if a.sessionName == "" {
		return fmt.Errorf("session name must not be empty")
	}
	openSession := a.openSession
	if openSession == nil {
		openSession = newSystemTmuxSession
	}
	session, err := openSession(a.sessionName)
	if err != nil {
		return NewAgentError(ErrorKindLaunch, a.sessionName, "", err)
	}
	pane, err := session.NewPane()
	if err != nil {
		_ = session.Close()
		return NewAgentError(ErrorKindLaunch, a.sessionName, "", err)
	}
	if err := pane.SendText(agentshell.BuildLaunchCommand(a.launchCommand)); err != nil {
		_ = session.Close()
		return NewAgentError(ErrorKindLaunch, a.sessionName, "", err)
	}
	a.session = session
	a.pane = pane
	a.closed = false
	return nil
}

func (a *ClaudeAgent) WaitUntilReady() error {
	if a.pane == nil {
		return NewAgentError(ErrorKindStartup, a.sessionName, "", fmt.Errorf("agent session has not started"))
	}

	readyTimeout := a.readyTimeout()
	quietPeriod := a.quietPeriod()
	pollInterval := a.pollInterval()
	deadline := time.Now().Add(readyTimeout)
	lastCapture := ""
	lastChange := time.Now()
	firstCapture := true

	for {
		capture, err := a.pane.Capture()
		if err != nil {
			return NewAgentError(ErrorKindCapture, a.sessionName, lastCapture, err)
		}
		if firstCapture || capture != lastCapture {
			lastCapture = capture
			lastChange = time.Now()
			firstCapture = false
		}

		ready := true
		if a.readyMatcher != nil {
			ready = a.readyMatcher(capture)
		}
		if ready && time.Since(lastChange) >= quietPeriod {
			return nil
		}
		if time.Now().After(deadline) {
			return NewAgentError(ErrorKindStartup, a.sessionName, lastCapture, fmt.Errorf("session did not become ready within %s", readyTimeout))
		}

		time.Sleep(pollInterval)
	}
}

func (a *ClaudeAgent) SendPrompt(prompt string) error {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	if a.pane == nil {
		return NewAgentError(ErrorKindCapture, a.sessionName, "", fmt.Errorf("agent session has not started"))
	}
	if err := a.pane.SendText(prompt); err != nil {
		return NewAgentError(ErrorKindCapture, a.sessionName, "", err)
	}
	return nil
}

func (a *ClaudeAgent) Capture() (string, error) {
	if a.pane == nil {
		return "", NewAgentError(ErrorKindCapture, a.sessionName, "", fmt.Errorf("agent session has not started"))
	}
	capture, err := a.pane.Capture()
	if err != nil {
		return "", NewAgentError(ErrorKindCapture, a.sessionName, "", err)
	}
	return capture, nil
}

func (a *ClaudeAgent) SessionName() string {
	if a == nil {
		return ""
	}
	if a.session != nil {
		return a.session.Name()
	}
	return a.sessionName
}

func (a *ClaudeAgent) Close() error {
	if a == nil || a.closed {
		return nil
	}
	a.closed = true
	if a.session == nil {
		return nil
	}
	if err := a.session.Close(); err != nil {
		return NewAgentError(ErrorKindClose, a.SessionName(), "", err)
	}
	return nil
}

func (a *ClaudeAgent) readyTimeout() time.Duration {
	if a.startupReadyTimeout > 0 {
		return a.startupReadyTimeout
	}
	return defaultStartupReadyTimeout
}

func (a *ClaudeAgent) quietPeriod() time.Duration {
	if a.startupQuietPeriod > 0 {
		return a.startupQuietPeriod
	}
	return defaultStartupQuietPeriod
}

func (a *ClaudeAgent) pollInterval() time.Duration {
	if a.capturePollInterval > 0 {
		return a.capturePollInterval
	}
	return defaultCapturePollInterval
}

func claudeReadyMatcher(capture string) bool {
	lower := strings.ToLower(capture)
	if strings.Contains(lower, "press enter to continue") ||
		strings.Contains(lower, "log in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "do you trust") {
		return false
	}
	return strings.TrimSpace(capture) != ""
}
