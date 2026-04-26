package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	completionInstruction = "Finish your response with exactly <promise>done</promise>."
	defaultIdleTimeout    = 120 * time.Second
	commandTimeout        = 10 * time.Second
	capturePollInterval   = 250 * time.Millisecond
	// tmux capture-pane -S uses negative values to address scrollback history.
	// Start far enough back to capture the full current turn from pane history.
	captureHistoryStart = "-32768"
)

var (
	execCommand = exec.CommandContext
	runCommand  = defaultRunCommand
)

type persistentSession struct {
	backendName        string
	sessionName        string
	idleTimeout        time.Duration
	startupInstruction string
	launchCommand      string
	closed             bool
}

func newPersistentSession(backendName string, command string, args []string, startupInstruction string, opts SessionOptions) (Session, error) {
	sessionName := sessionNameFor(opts.RunID, opts.Role)
	session := &persistentSession{
		backendName:        backendName,
		sessionName:        sessionName,
		idleTimeout:        opts.IdleTimeout,
		startupInstruction: startupInstruction,
		launchCommand:      buildSourcedLauncher(command, args...),
	}
	if session.idleTimeout <= 0 {
		session.idleTimeout = defaultIdleTimeout
	}
	if err := session.createTmuxSession(); err != nil {
		return nil, NewSessionError(SessionErrorKindLaunch, session.sessionName, "", err)
	}
	return session, nil
}

func (s *persistentSession) Start(rolePrompt string) error {
	result, err := s.runPrompt(s.decorateStartupPrompt(rolePrompt))
	if err != nil {
		return s.normalizeStartupError(err)
	}
	if err := s.resetPane(); err != nil {
		return s.normalizeStartupError(NewSessionError(SessionErrorKindCapture, s.sessionName, result.RawCapture, err))
	}
	return nil
}

func (s *persistentSession) RunTurn(prompt string) (TurnResult, error) {
	result, err := s.runPrompt(s.decorateTurnPrompt(prompt))
	if err != nil {
		return TurnResult{}, err
	}
	if err := s.resetPane(); err != nil {
		return TurnResult{}, NewSessionError(SessionErrorKindCapture, s.sessionName, result.RawCapture, err)
	}
	return result, nil
}

func (s *persistentSession) SessionName() string {
	return s.sessionName
}

func (s *persistentSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	exists, err := s.hasSession()
	if err != nil {
		return NewSessionError(SessionErrorKindClose, s.sessionName, "", err)
	}
	if !exists {
		return nil
	}

	if _, err := runCommand("tmux", "kill-session", "-t", s.sessionName); err != nil {
		if isTmuxSessionAbsentError(err) {
			return nil
		}
		return NewSessionError(SessionErrorKindClose, s.sessionName, "", err)
	}
	return nil
}

func (s *persistentSession) createTmuxSession() error {
	_, err := runCommand("tmux", "new-session", "-d", "-s", s.sessionName, s.launchCommand)
	return err
}

func (s *persistentSession) hasSession() (bool, error) {
	_, err := runCommand("tmux", "has-session", "-t", s.sessionName)
	if err == nil {
		return true, nil
	}
	if isTmuxSessionAbsentError(err) {
		return false, nil
	}
	return false, err
}

func (s *persistentSession) normalizeStartupError(err error) error {
	if err == nil {
		return nil
	}

	capture := ""
	if sessionErr, ok := AsSessionError(err); ok {
		if sessionErr.Kind() == SessionErrorKindStartup {
			return err
		}
		capture = sessionErr.Capture()
		if sessionErr.Kind() == SessionErrorKindTimeout {
			return NewSessionError(SessionErrorKindStartup, s.sessionName, capture, fmt.Errorf("startup acknowledgement timed out: %w", err))
		}
	}

	return NewSessionError(SessionErrorKindStartup, s.sessionName, capture, err)
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
	baseline, err := s.capturePane()
	if err != nil {
		return TurnResult{}, NewSessionError(SessionErrorKindCapture, s.sessionName, "", err)
	}

	if err := s.sendPrompt(prompt); err != nil {
		return TurnResult{}, NewSessionError(SessionErrorKindCapture, s.sessionName, "", err)
	}

	rawCapture, err := s.waitForDone(prompt, baseline)
	if err != nil {
		return TurnResult{}, err
	}
	return TurnResult{
		Output:     sanitizeTurnCapture(rawCapture, prompt),
		RawCapture: rawCapture,
	}, nil
}

func (s *persistentSession) sendPrompt(prompt string) error {
	bufferName := fmt.Sprintf("%s-%d", s.sessionName, time.Now().UnixNano())
	if _, err := runCommand("tmux", "set-buffer", "-b", bufferName, "--", prompt); err != nil {
		return err
	}
	if _, err := runCommand("tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", s.sessionName); err != nil {
		return err
	}
	if _, err := runCommand("tmux", "send-keys", "-t", s.sessionName, "Enter"); err != nil {
		return err
	}
	return nil
}

func (s *persistentSession) waitForDone(prompt string, baselineCapture string) (string, error) {
	lastCapture := baselineCapture
	lastChange := time.Now()

	for {
		capture, err := s.capturePane()
		if err != nil {
			turnCapture := extractTurnCapture(lastCapture, baselineCapture)
			return "", NewSessionError(SessionErrorKindCapture, s.sessionName, turnCapture, err)
		}

		if capture != lastCapture {
			lastCapture = capture
			lastChange = time.Now()
		}

		turnCapture := extractTurnCapture(capture, baselineCapture)
		if hasExactLine(responseCapture(turnCapture, prompt), "<promise>done</promise>") {
			return turnCapture, nil
		}

		if time.Since(lastChange) >= s.idleTimeout {
			return "", NewSessionError(SessionErrorKindTimeout, s.sessionName, turnCapture, fmt.Errorf("session %s timed out waiting for completion marker", s.sessionName))
		}

		time.Sleep(capturePollInterval)
	}
}

func (s *persistentSession) capturePane() (string, error) {
	result, err := runCommand("tmux", "capture-pane", "-p", "-J", "-S", captureHistoryStart, "-t", s.sessionName)
	if err != nil {
		return "", err
	}
	return result.stdout, nil
}

func (s *persistentSession) resetPane() error {
	if _, err := runCommand("tmux", "send-keys", "-R", "-t", s.sessionName); err != nil {
		return err
	}
	// TODO: clean-history is probably not the cleanest way to do this, and hurts interactivity.
	// We can instead save a snapshot and diff the output (or just even count the lines delimited by "\n")
	if _, err := runCommand("tmux", "clear-history", "-t", s.sessionName); err != nil {
		return err
	}
	return nil
}

type commandResult struct {
	stdout string
	stderr string
}

func defaultRunCommand(name string, args ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := execCommand(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	result := commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
	}

	if ctx.Err() != nil {
		return result, fmt.Errorf("%s %s timed out: %w", name, strings.Join(args, " "), ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(result.stderr)
		if detail != "" {
			return result, fmt.Errorf("%w: %s", err, detail)
		}
		return result, err
	}

	return result, nil
}

func buildSourcedLauncher(command string, args ...string) string {
	quotedCommand := make([]string, 0, 1+len(args))
	quotedCommand = append(quotedCommand, shellQuote(command))
	for _, arg := range args {
		quotedCommand = append(quotedCommand, shellQuote(arg))
	}
	script := `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo; exec ` + strings.Join(quotedCommand, " ")
	return "bash -lc " + shellQuote(script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sessionNameFor(runID string, role string) string {
	return fmt.Sprintf("iwr-%s-%s", runID, role)
}

func isTmuxSessionAbsentError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "can't find session") ||
		strings.Contains(message, "no server running") ||
		strings.Contains(message, "server exited unexpectedly")
}

func hasExactLine(text string, target string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func extractTurnCapture(capture string, baseline string) string {
	if capture == "" {
		return ""
	}
	if baseline == "" {
		return capture
	}
	if strings.HasPrefix(capture, baseline) {
		return capture[len(baseline):]
	}

	limit := len(baseline)
	if len(capture) < limit {
		limit = len(capture)
	}
	for overlap := limit; overlap > 0; overlap-- {
		if strings.HasSuffix(baseline, capture[:overlap]) {
			return capture[overlap:]
		}
	}

	return capture
}

func sanitizeTurnCapture(capture string, prompt string) string {
	trimmed := strings.TrimLeft(responseCapture(capture, prompt), "\r\n")
	trimmed = strings.TrimRight(trimmed, "\r\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}

func responseCapture(capture string, prompt string) string {
	boundary := promptBoundaryIndex(capture, prompt)
	if boundary >= len(capture) {
		return ""
	}
	return capture[boundary:]
}

func promptBoundaryIndex(capture string, prompt string) int {
	if capture == "" {
		return 0
	}

	base := strings.TrimRight(prompt, "\n")
	searchLimit := len(capture)
	if base != "" && len(base)+256 < searchLimit {
		searchLimit = len(base) + 256
	}
	window := capture[:searchLimit]

	for _, candidate := range promptEchoCandidates(prompt) {
		if candidate == "" {
			continue
		}
		if idx := strings.Index(window, candidate); idx >= 0 {
			return skipLineBreaks(capture, idx+len(candidate))
		}
	}

	if idx := strings.Index(window, completionInstruction); idx >= 0 {
		return skipLineBreaks(capture, idx+len(completionInstruction))
	}

	return 0
}

func promptEchoCandidates(prompt string) []string {
	base := strings.TrimRight(prompt, "\n")
	if base == "" {
		return nil
	}
	return []string{
		base,
		base + "\n",
		strings.TrimSpace(base),
	}
}

func skipLineBreaks(text string, index int) int {
	for index < len(text) && (text[index] == '\n' || text[index] == '\r') {
		index++
	}
	return index
}
