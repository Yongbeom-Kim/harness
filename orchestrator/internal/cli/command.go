package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/tmux"
)

const (
	completionInstruction = "Finish your response with exactly <promise>done</promise>."
	defaultIdleTimeout    = 120 * time.Second
	startupReadyTimeout   = 30 * time.Second
	startupQuietPeriod    = 1500 * time.Millisecond
	capturePollInterval   = 250 * time.Millisecond
)

type persistentSession struct {
	backendName        string
	tmuxSession        tmux.TmuxSessionLike
	pane               tmux.TmuxPaneLike
	idleTimeout        time.Duration
	startupInstruction string
	readyMatcher       func(string) bool
	closed             bool
}

func newPersistentSession(
	backendName string,
	launchCommand string,
	startupInstruction string,
	readyMatcher func(string) bool,
	opts SessionOptions,
) (Session, error) {
	sessionName := sessionNameFor(opts.RunID, opts.Role)
	p := &persistentSession{
		backendName:        backendName,
		idleTimeout:        opts.IdleTimeout,
		startupInstruction: startupInstruction,
		readyMatcher:       readyMatcher,
	}
	if p.idleTimeout <= 0 {
		p.idleTimeout = defaultIdleTimeout
	}

	sess, err := tmux.NewTmuxSession(sessionName)
	if err != nil {
		return nil, NewRunnerError(RunnerErrorKindLaunch, sessionName, "", err)
	}
	pane, err := sess.NewPane()
	if err != nil {
		_ = sess.Close()
		return nil, NewRunnerError(RunnerErrorKindLaunch, sessionName, "", err)
	}
	if launchCommand != "" {
		if err := pane.SendText(launchCommand); err != nil {
			_ = sess.Close()
			return nil, NewRunnerError(RunnerErrorKindLaunch, sessionName, "", err)
		}
	}
	p.tmuxSession = sess
	p.pane = pane
	return p, nil
}

func (s *persistentSession) name() string {
	if s == nil || s.tmuxSession == nil {
		return ""
	}
	return s.tmuxSession.Name()
}

func (s *persistentSession) Start(rolePrompt string) error {
	if err := s.waitForReady(); err != nil {
		return s.normalizeStartupError(err)
	}
	if _, err := s.runPrompt(s.decorateStartupPrompt(rolePrompt)); err != nil {
		return s.normalizeStartupError(err)
	}
	return nil
}

func (s *persistentSession) waitForReady() error {
	deadline := time.Now().Add(startupReadyTimeout)
	lastCapture := ""
	lastChange := time.Now()
	firstCapture := true

	for {
		capture, err := s.pane.Capture()
		if err != nil {
			return NewRunnerError(RunnerErrorKindCapture, s.name(), lastCapture, err)
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
		if ready && time.Since(lastChange) >= startupQuietPeriod {
			return nil
		}

		if time.Now().After(deadline) {
			return NewRunnerError(RunnerErrorKindStartup, s.name(), lastCapture, fmt.Errorf("session did not become ready for startup prompt within %s", startupReadyTimeout))
		}

		time.Sleep(capturePollInterval)
	}
}

func (s *persistentSession) RunTurn(prompt string) (TurnResult, error) {
	return s.runPrompt(s.decorateTurnPrompt(prompt))
}

func (s *persistentSession) SessionName() string {
	return s.name()
}

func (s *persistentSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	if s.tmuxSession == nil {
		return nil
	}
	if err := s.tmuxSession.Close(); err != nil {
		return NewRunnerError(RunnerErrorKindClose, s.name(), "", err)
	}
	return nil
}

func (s *persistentSession) normalizeStartupError(err error) error {
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
			return NewRunnerError(RunnerErrorKindStartup, s.name(), capture, fmt.Errorf("startup acknowledgement timed out: %w", err))
		}
	}

	return NewRunnerError(RunnerErrorKindStartup, s.name(), capture, err)
}

func (s *persistentSession) decorateStartupPrompt(rolePrompt string) string {
	return s.decorateTurnPrompt(strings.TrimSpace(rolePrompt + "\n\n" + s.startupInstruction))
}

func (s *persistentSession) decorateTurnPrompt(prompt string) string {
	base := strings.TrimRight(prompt, "\n")
	if base == "" {
		return completionInstruction
	}
	return base + "\n\n" + completionInstruction
}

func (s *persistentSession) runPrompt(prompt string) (TurnResult, error) {
	turnMarker := s.nextTurnMarker()
	markedPrompt := prependTurnMarker(prompt, turnMarker)

	if err := s.pane.SendText(markedPrompt); err != nil {
		return TurnResult{}, NewRunnerError(RunnerErrorKindCapture, s.name(), "", err)
	}

	rawCapture, err := s.waitForDone(turnMarker)
	if err != nil {
		return TurnResult{}, err
	}
	// RawCapture is the turn slice after the iwr turn marker in pane text, not the full
	// capture-pane buffer. tmux.TmuxPaneLike.Capture() still returns the raw pane per poll.
	return TurnResult{
		Output:     sanitizeTurnCapture(rawCapture),
		RawCapture: rawCapture,
	}, nil
}

func (s *persistentSession) waitForDone(turnMarker string) (string, error) {
	lastTurnCapture := ""
	lastChange := time.Now()

	for {
		capture, err := s.pane.Capture()
		if err != nil {
			return "", NewRunnerError(RunnerErrorKindCapture, s.name(), lastTurnCapture, err)
		}

		turnCapture := extractTurnCapture(capture, turnMarker)
		if turnCapture != lastTurnCapture {
			lastTurnCapture = turnCapture
			lastChange = time.Now()
		}

		if hasExactLine(responseCapture(turnCapture), "<promise>done</promise>") {
			return turnCapture, nil
		}

		if time.Since(lastChange) >= s.idleTimeout {
			return "", NewRunnerError(RunnerErrorKindTimeout, s.name(), turnCapture, fmt.Errorf("session %s timed out waiting for completion marker", s.name()))
		}

		time.Sleep(capturePollInterval)
	}
}

func BuildSourcedLauncher(command string, args ...string) string {
	quotedCommand := make([]string, 0, 1+len(args))
	quotedCommand = append(quotedCommand, shellQuote(command))
	for _, arg := range args {
		quotedCommand = append(quotedCommand, shellQuote(arg))
	}
	script := `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo; ` + strings.Join(quotedCommand, " ")
	return "bash -lc " + shellQuote(script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sessionNameFor(runID string, role string) string {
	return fmt.Sprintf("iwr-%s-%s", runID, role)
}

func hasExactLine(text string, target string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func (s *persistentSession) nextTurnMarker() string {
	return fmt.Sprintf("<iwr:%x>", time.Now().UnixNano())
}

func prependTurnMarker(prompt string, marker string) string {
	if marker == "" {
		return prompt
	}
	return marker + "\n" + prompt
}

func extractTurnCapture(capture string, marker string) string {
	if capture == "" {
		return ""
	}
	if marker == "" {
		return capture
	}
	index := strings.Index(capture, marker)
	if index < 0 {
		return ""
	}
	return stripTurnMarker(capture[index:], marker)
}

func stripTurnMarker(capture string, marker string) string {
	if marker == "" {
		return capture
	}
	if strings.HasPrefix(capture, marker) {
		return capture[skipLineBreaks(capture, len(marker)):]
	}
	return capture
}

func sanitizeTurnCapture(capture string) string {
	trimmed := strings.TrimLeft(responseCapture(capture), "\r\n")
	trimmed = strings.TrimRight(trimmed, "\r\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}

func responseCapture(capture string) string {
	index := strings.LastIndex(capture, completionInstruction)
	if index < 0 {
		return capture
	}
	return capture[skipLineBreaks(capture, index+len(completionInstruction)):]
}

func skipLineBreaks(text string, index int) int {
	for index < len(text) && (text[index] == '\n' || text[index] == '\r') {
		index++
	}
	return index
}
