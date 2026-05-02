package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

const (
	defaultStartupReadyTimeout = 30 * time.Second
	defaultStartupQuietPeriod  = 1500 * time.Millisecond
	defaultCapturePollInterval = 250 * time.Millisecond
)

type Dependencies struct {
	OpenSession  func(sessionName string) (tmux.TmuxSessionLike, error)
	Launcher     launcher.Builder
	Protocol     Protocol
	SessionNames SessionNamer
}

type Config struct {
	BackendName         string
	LaunchCommand       string
	StartupInstruction  string
	ReadyMatcher        func(string) bool
	StartupReadyTimeout time.Duration
	StartupQuietPeriod  time.Duration
	CapturePollInterval time.Duration
	Options             SessionOptions
	Dependencies        Dependencies
}

type runtimeSession struct {
	session             tmux.TmuxSessionLike
	pane                tmux.TmuxPaneLike
	idleTimeout         time.Duration
	readyMatcher        func(string) bool
	startupInstruction  string
	protocol            Protocol
	startupReadyTimeout time.Duration
	startupQuietPeriod  time.Duration
	capturePollInterval time.Duration
	closed              bool
	sendMu              sync.Mutex
}

func New(cfg Config) (Session, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	sessionName := cfg.Dependencies.SessionNames.Build(cfg.Options.RunID, cfg.Options.Role)
	session := &runtimeSession{
		idleTimeout:         cfg.Options.IdleTimeout,
		readyMatcher:        cfg.ReadyMatcher,
		startupInstruction:  cfg.StartupInstruction,
		protocol:            cfg.Dependencies.Protocol,
		startupReadyTimeout: cfg.StartupReadyTimeout,
		startupQuietPeriod:  cfg.StartupQuietPeriod,
		capturePollInterval: cfg.CapturePollInterval,
	}

	hostSession, pane, err := launchSession(
		cfg.Dependencies.OpenSession,
		cfg.Dependencies.Launcher,
		sessionName,
		cfg.LaunchCommand,
	)
	if err != nil {
		return nil, NewRunnerError(RunnerErrorKindLaunch, sessionName, "", err)
	}

	session.session = hostSession
	session.pane = pane
	return session, nil
}

func (s *runtimeSession) Start(rolePrompt string) error {
	if err := s.waitUntilReady(); err != nil {
		return s.normalizeStartupError(err)
	}
	startupPrompt := s.protocol.DecorateStartupPrompt(rolePrompt, s.startupInstruction)
	if _, err := s.runTurn(startupPrompt); err != nil {
		return s.normalizeStartupError(err)
	}
	return nil
}

func (s *runtimeSession) RunTurn(prompt string) (TurnResult, error) {
	result, err := s.runTurn(prompt)
	if err != nil {
		return TurnResult{}, err
	}
	return result, nil
}

func (s *runtimeSession) InjectSideChannel(message string) error {
	return s.sendText(message)
}

func (s *runtimeSession) SessionName() string {
	if s == nil || s.session == nil {
		return ""
	}
	return s.session.Name()
}

func (s *runtimeSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	if s.session == nil {
		return nil
	}
	if err := s.session.Close(); err != nil {
		return NewRunnerError(RunnerErrorKindClose, s.SessionName(), "", err)
	}
	return nil
}

func (s *runtimeSession) normalizeStartupError(err error) error {
	if err == nil {
		return nil
	}

	capture := ""
	if sessionErr, ok := AsRunnerError(err); ok {
		if sessionErr.Kind() == RunnerErrorKindStartup {
			return err
		}
		capture = sessionErr.Capture()
		if sessionErr.Kind() == RunnerErrorKindTimeout {
			return NewRunnerError(RunnerErrorKindStartup, s.SessionName(), capture, fmt.Errorf("startup acknowledgement timed out: %w", err))
		}
	}

	return NewRunnerError(RunnerErrorKindStartup, s.SessionName(), capture, err)
}

func (s *runtimeSession) sendText(text string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.pane.SendText(text)
}

func (s *runtimeSession) waitUntilReady() error {
	deadline := time.Now().Add(s.readyTimeout())
	lastCapture := ""
	lastChange := time.Now()
	firstCapture := true

	for {
		capture, err := s.pane.Capture()
		if err != nil {
			return NewRunnerError(RunnerErrorKindCapture, s.SessionName(), lastCapture, err)
		}

		if firstCapture || capture != lastCapture {
			lastCapture = capture
			lastChange = time.Now()
			firstCapture = false
		}

		ready := true
		if s.readyMatcher != nil {
			ready = s.readyMatcher(capture)
		}
		if ready && time.Since(lastChange) >= s.quietPeriod() {
			return nil
		}

		if time.Now().After(deadline) {
			return NewRunnerError(RunnerErrorKindStartup, s.SessionName(), lastCapture, fmt.Errorf("session did not become ready for startup prompt within %s", s.readyTimeout()))
		}

		time.Sleep(s.pollInterval())
	}
}

func (s *runtimeSession) runTurn(prompt string) (TurnResult, error) {
	prepared := s.protocol.PrepareTurn(prompt)
	if err := s.sendText(prepared.Prompt); err != nil {
		return TurnResult{}, NewRunnerError(RunnerErrorKindCapture, s.SessionName(), "", err)
	}

	rawCapture, err := s.waitForDone(prepared)
	if err != nil {
		return TurnResult{}, err
	}

	return TurnResult{
		Output:     s.protocol.SanitizeTurnCapture(rawCapture, prepared),
		RawCapture: rawCapture,
	}, nil
}

func (s *runtimeSession) waitForDone(prepared PreparedTurn) (string, error) {
	lastTurnCapture := ""
	lastChange := time.Now()
	timeout := s.idleTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	for {
		capture, err := s.pane.Capture()
		if err != nil {
			return "", NewRunnerError(RunnerErrorKindCapture, s.SessionName(), lastTurnCapture, err)
		}

		turnCapture := s.protocol.ExtractTurnCapture(capture, prepared)
		if turnCapture != lastTurnCapture {
			lastTurnCapture = turnCapture
			lastChange = time.Now()
		}

		if s.protocol.IsTurnComplete(turnCapture, prepared) && time.Since(lastChange) >= s.pollInterval() {
			return turnCapture, nil
		}

		if time.Since(lastChange) >= timeout {
			return "", NewRunnerError(RunnerErrorKindTimeout, s.SessionName(), turnCapture, fmt.Errorf("session %s timed out waiting for completion marker", s.SessionName()))
		}

		time.Sleep(s.pollInterval())
	}
}

func (s *runtimeSession) readyTimeout() time.Duration {
	if s.startupReadyTimeout > 0 {
		return s.startupReadyTimeout
	}
	return defaultStartupReadyTimeout
}

func (s *runtimeSession) quietPeriod() time.Duration {
	if s.startupQuietPeriod > 0 {
		return s.startupQuietPeriod
	}
	return defaultStartupQuietPeriod
}

func (s *runtimeSession) pollInterval() time.Duration {
	if s.capturePollInterval > 0 {
		return s.capturePollInterval
	}
	return defaultCapturePollInterval
}

func validateConfig(cfg Config) error {
	switch {
	case cfg.BackendName == "":
		return fmt.Errorf("backend name must not be empty")
	case cfg.LaunchCommand == "":
		return fmt.Errorf("launch command must not be empty")
	case cfg.Options.IdleTimeout <= 0:
		return fmt.Errorf("IdleTimeout must be > 0")
	case cfg.Dependencies.OpenSession == nil:
		return fmt.Errorf("OpenSession must not be nil")
	case cfg.Dependencies.Launcher == nil:
		return fmt.Errorf("Launcher must not be nil")
	case cfg.Dependencies.Protocol == nil:
		return fmt.Errorf("Protocol must not be nil")
	case cfg.Dependencies.SessionNames == nil:
		return fmt.Errorf("SessionNames must not be nil")
	default:
		return nil
	}
}
