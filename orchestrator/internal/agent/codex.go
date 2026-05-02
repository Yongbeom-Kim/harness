package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	agentshell "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/shell"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
)

const (
	defaultStartupReadyTimeout = 30 * time.Second
	defaultStartupQuietPeriod  = 1500 * time.Millisecond
	defaultCapturePollInterval = 250 * time.Millisecond
)

type CodexAgent struct {
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

func NewCodexAgent(sessionName string) *CodexAgent {
	return &CodexAgent{
		sessionName:   sessionName,
		openSession:   newSystemTmuxSession,
		launchCommand: "codex",
		readyMatcher:  codexReadyMatcher,
	}
}

func newSystemTmuxSession(sessionName string) (tmux.TmuxSessionLike, error) {
	return tmux.NewTmuxSession(sessionName)
}

func (a *CodexAgent) Start() error {
	if a == nil {
		return fmt.Errorf("nil CodexAgent")
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

func (a *CodexAgent) WaitUntilReady() error {
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

func (a *CodexAgent) SendPrompt(prompt string) error {
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

func (a *CodexAgent) Capture() (string, error) {
	if a.pane == nil {
		return "", NewAgentError(ErrorKindCapture, a.sessionName, "", fmt.Errorf("agent session has not started"))
	}
	capture, err := a.pane.Capture()
	if err != nil {
		return "", NewAgentError(ErrorKindCapture, a.sessionName, "", err)
	}
	return capture, nil
}

func (a *CodexAgent) SessionName() string {
	if a == nil {
		return ""
	}
	if a.session != nil {
		return a.session.Name()
	}
	return a.sessionName
}

func (a *CodexAgent) Close() error {
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

func (a *CodexAgent) readyTimeout() time.Duration {
	if a.startupReadyTimeout > 0 {
		return a.startupReadyTimeout
	}
	return defaultStartupReadyTimeout
}

func (a *CodexAgent) quietPeriod() time.Duration {
	if a.startupQuietPeriod > 0 {
		return a.startupQuietPeriod
	}
	return defaultStartupQuietPeriod
}

func (a *CodexAgent) pollInterval() time.Duration {
	if a.capturePollInterval > 0 {
		return a.capturePollInterval
	}
	return defaultCapturePollInterval
}

func codexReadyMatcher(capture string) bool {
	if strings.Contains(capture, "Sign in with ChatGPT") ||
		strings.Contains(capture, "Press Enter to continue") {
		return false
	}

	if strings.Contains(capture, "OpenAI Codex") && strings.Contains(capture, "\n› ") {
		return true
	}

	return strings.Contains(capture, "Welcome to Codex") && strings.Contains(capture, "\n› ")
}
