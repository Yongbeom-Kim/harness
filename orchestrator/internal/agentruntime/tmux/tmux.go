// Package tmux runs the `tmux` binary.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultCommandTimeout = 10 * time.Second
	captureHistoryStart   = "-32768"
	paneStateFormat       = "#{pane_input_off}\t#{pane_in_mode}\t#{pane_mode}\t#{pane_dead}\t#{pane_current_command}"
	// Give the target TUI a moment to absorb literal text before a follow-up
	// control key like Tab or Enter.
	postSendTextKeyDelay             = 25 * time.Millisecond
	maxInteractiveRecoveryAttempts   = 5
	recoveryRetryDelay               = 10 * time.Millisecond
	deliveryVerificationTimeout      = 200 * time.Millisecond
	deliveryVerificationPollInterval = 10 * time.Millisecond
)

var (
	runTmuxCommand               = runCommand
	runTmuxCommandWithInput      = runCommandWithInput
	sleepBeforePressKey          = time.Sleep
	sleepForInteractiveRecovery  = time.Sleep
	sleepForDeliveryVerification = time.Sleep
)

type TmuxSession struct {
	name                string
	target              string
	defaultPaneReturned bool
}

func NewTmuxSession(name string) (*TmuxSession, error) {
	if name == "" {
		return nil, fmt.Errorf("tmux session name must not be empty")
	}
	if _, err := runTmuxCommand("tmux", "new-session", "-d", "-s", name); err != nil {
		return nil, &NewSessionError{SessionName: name, Err: err}
	}
	return &TmuxSession{name: name, target: defaultPaneTarget(name)}, nil
}

func OpenTmuxSession(name string) (*TmuxSession, error) {
	if name == "" {
		return nil, fmt.Errorf("tmux session name must not be empty")
	}
	session := &TmuxSession{name: name, target: defaultPaneTarget(name)}
	exists, err := session.hasSession()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &TmuxSessionAbsentError{Message: fmt.Sprintf("can't find session: %s", name)}
	}
	return session, nil
}

func (s *TmuxSession) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}

func (s *TmuxSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if s == nil {
		return fmt.Errorf("nil TmuxSession")
	}
	if s.name == "" {
		return fmt.Errorf("tmux session: empty session name")
	}
	cmd := exec.Command("tmux", "attach-session", "-t", s.name)
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return &AttachSessionError{SessionName: s.name, Err: err}
	}
	return nil
}

func (s *TmuxSession) Close() error {
	if s == nil {
		return nil
	}
	exists, err := s.hasSession()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = runTmuxCommand("tmux", "kill-session", "-t", s.name)
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*TmuxSessionAbsentError](err); ok {
		return nil
	}
	return &KillSessionError{SessionName: s.name, Err: err}
}

func (s *TmuxSession) hasSession() (bool, error) {
	_, err := runTmuxCommand("tmux", "has-session", "-t", s.name)
	if err == nil {
		return true, nil
	}
	if _, ok := errors.AsType[*TmuxSessionAbsentError](err); ok {
		return false, nil
	}
	return false, &HasSessionError{SessionName: s.name, Err: err}
}

func (s *TmuxSession) NewPane() (TmuxPaneLike, error) {
	if s == nil {
		return nil, fmt.Errorf("nil TmuxSession")
	}
	t := s.target
	if t == "" {
		t = defaultPaneTarget(s.name)
	}
	if !s.defaultPaneReturned {
		s.defaultPaneReturned = true
		return &TmuxPane{target: t, session: s}, nil
	}
	result, err := runTmuxCommand("tmux", "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", t)
	if err != nil {
		return nil, &SplitWindowError{Target: t, Err: err}
	}
	newTarget := strings.TrimSpace(result.stdout)
	if newTarget == "" {
		return nil, &SplitWindowError{Target: t, Err: fmt.Errorf("empty pane id in tmux output")}
	}
	return &TmuxPane{target: newTarget, session: s}, nil
}

func defaultPaneTarget(sessionName string) string {
	if sessionName == "" {
		return "%0"
	}
	return sessionName + ":0.0"
}

type TmuxPane struct {
	target              string
	session             *TmuxSession
	settleBeforeNextKey bool
	pendingDelivery     pendingDeliveryCheck
}

type pendingDeliveryCheck struct {
	baseline string
	active   bool
}

type paneStateSnapshot struct {
	InputOff       bool
	InMode         bool
	Mode           string
	Dead           bool
	CurrentCommand string
}

func (s paneStateSnapshot) isInteractive() bool {
	return !s.InputOff && !s.InMode && !s.Dead
}

func (s paneStateSnapshot) String() string {
	return fmt.Sprintf(
		"pane_input_off=%s pane_in_mode=%s pane_mode=%q pane_dead=%s pane_current_command=%q",
		boolToPaneStateValue(s.InputOff),
		boolToPaneStateValue(s.InMode),
		s.Mode,
		boolToPaneStateValue(s.Dead),
		s.CurrentCommand,
	)
}

func (p *TmuxPane) targetName() (string, error) {
	if p == nil {
		return "", fmt.Errorf("nil TmuxPane")
	}
	t := p.target
	if t == "" && p.session != nil {
		t = p.session.Name()
	}
	if t == "" {
		return "", fmt.Errorf("tmux pane: empty target and no session name")
	}
	return t, nil
}

func (p *TmuxPane) SendText(text string) error {
	t, err := p.targetName()
	if err != nil {
		return err
	}
	if _, err := p.ensureInteractive(t, "send text"); err != nil {
		return err
	}
	p.pendingDelivery = pendingDeliveryCheck{}
	p.settleBeforeNextKey = false
	if text == "" {
		return nil
	}
	baseline, err := capturePaneTarget(t)
	if err != nil {
		return err
	}
	bufferName := paneBufferName(p.session)
	if _, err := runTmuxCommandWithInput(text, "tmux", "load-buffer", "-b", bufferName, "-"); err != nil {
		return &LoadBufferError{BufferName: bufferName, Err: err}
	}
	if _, err := runTmuxCommand("tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", t); err != nil {
		return &PasteBufferError{Target: t, BufferName: bufferName, Err: err}
	}
	p.pendingDelivery = pendingDeliveryCheck{baseline: baseline, active: true}
	p.settleBeforeNextKey = true
	return nil
}

func (p *TmuxPane) PressKey(key string) error {
	t, err := p.targetName()
	if err != nil {
		return err
	}
	if p.settleBeforeNextKey && postSendTextKeyDelay > 0 {
		sleepBeforePressKey(postSendTextKeyDelay)
	}
	state, err := p.ensureInteractive(t, pressKeyOperation(key))
	if err != nil {
		return err
	}
	clearPendingAfterSend := false
	if isSubmitKey(key) && p.pendingDelivery.active {
		if err := p.verifyPendingDelivery(t, key, state); err != nil {
			p.pendingDelivery = pendingDeliveryCheck{}
			p.settleBeforeNextKey = false
			return err
		}
		clearPendingAfterSend = true
	} else if p.pendingDelivery.active {
		clearPendingAfterSend = true
	}
	if _, err := runTmuxCommand("tmux", "send-keys", "-t", t, key); err != nil {
		return &SendKeysError{Target: t, Keys: []string{key}, Err: err}
	}
	if clearPendingAfterSend {
		p.pendingDelivery = pendingDeliveryCheck{}
	}
	p.settleBeforeNextKey = false
	return nil
}

func (p *TmuxPane) Capture() (string, error) {
	t, err := p.targetName()
	if err != nil {
		return "", err
	}
	return capturePaneTarget(t)
}

// ensureInteractive applies tmux-local recovery until the pane can receive input or returns the last observed pane state.
func (p *TmuxPane) ensureInteractive(target string, operation string) (paneStateSnapshot, error) {
	for attempt := 1; attempt <= maxInteractiveRecoveryAttempts; attempt++ {
		state, err := readPaneState(target)
		if err != nil {
			return paneStateSnapshot{}, err
		}
		if state.isInteractive() {
			return state, nil
		}
		if state.Dead || attempt == maxInteractiveRecoveryAttempts {
			return state, &NonInteractivePaneError{
				Target:    target,
				Operation: operation,
				State:     state,
				Attempts:  attempt,
			}
		}
		recovered, err := recoverPaneState(target, state)
		if err != nil {
			return paneStateSnapshot{}, err
		}
		if !recovered {
			return state, &NonInteractivePaneError{
				Target:    target,
				Operation: operation,
				State:     state,
				Attempts:  attempt,
			}
		}
		if recoveryRetryDelay > 0 {
			sleepForInteractiveRecovery(recoveryRetryDelay)
		}
	}
	return paneStateSnapshot{}, &NonInteractivePaneError{
		Target:    target,
		Operation: operation,
		Attempts:  maxInteractiveRecoveryAttempts,
	}
}

func (p *TmuxPane) verifyPendingDelivery(target string, key string, initialState paneStateSnapshot) error {
	pollsRemaining := deliveryVerificationPollAttempts()
	for pollsRemaining > 0 {
		capture, err := capturePaneTarget(target)
		if err != nil {
			return err
		}
		if capture != p.pendingDelivery.baseline {
			return nil
		}
		pollsRemaining--
		if pollsRemaining == 0 {
			break
		}
		if deliveryVerificationPollInterval > 0 {
			sleepForDeliveryVerification(deliveryVerificationPollInterval)
		}
	}

	state := initialState
	if latestState, err := readPaneState(target); err == nil {
		state = latestState
	}
	return &DeliveryVerificationError{
		Target:    target,
		Operation: pressKeyOperation(key),
		State:     state,
		Timeout:   deliveryVerificationTimeout,
	}
}

func readPaneState(target string) (paneStateSnapshot, error) {
	result, err := runTmuxCommand("tmux", "display-message", "-p", "-t", target, paneStateFormat)
	if err != nil {
		return paneStateSnapshot{}, fmt.Errorf("tmux display-message for target %q failed: %w", target, err)
	}
	return parsePaneStateSnapshot(result.stdout)
}

func parsePaneStateSnapshot(output string) (paneStateSnapshot, error) {
	parts := strings.Split(strings.TrimRight(output, "\r\n"), "\t")
	if len(parts) != 5 {
		return paneStateSnapshot{}, fmt.Errorf("unexpected tmux pane state output %q", strings.TrimRight(output, "\r\n"))
	}
	inputOff, err := parsePaneStateFlag("pane_input_off", parts[0])
	if err != nil {
		return paneStateSnapshot{}, err
	}
	inMode, err := parsePaneStateFlag("pane_in_mode", parts[1])
	if err != nil {
		return paneStateSnapshot{}, err
	}
	dead, err := parsePaneStateFlag("pane_dead", parts[3])
	if err != nil {
		return paneStateSnapshot{}, err
	}
	return paneStateSnapshot{
		InputOff:       inputOff,
		InMode:         inMode,
		Mode:           parts[2],
		Dead:           dead,
		CurrentCommand: parts[4],
	}, nil
}

func parsePaneStateFlag(name string, value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("unexpected tmux %s value %q", name, value)
	}
}

func recoverPaneState(target string, state paneStateSnapshot) (bool, error) {
	var recovered bool
	if state.InMode {
		if _, err := runTmuxCommand("tmux", "copy-mode", "-q", "-t", target); err != nil {
			return false, fmt.Errorf("tmux copy-mode for target %q failed: %w", target, err)
		}
		recovered = true
	}
	if state.InputOff {
		if _, err := runTmuxCommand("tmux", "select-pane", "-e", "-t", target); err != nil {
			return false, fmt.Errorf("tmux select-pane for target %q failed: %w", target, err)
		}
		recovered = true
	}
	return recovered, nil
}

func capturePaneTarget(target string) (string, error) {
	result, err := runTmuxCommand("tmux", "capture-pane", "-p", "-J", "-S", captureHistoryStart, "-t", target)
	if err != nil {
		return "", &CapturePaneError{Target: target, Err: err}
	}
	return result.stdout, nil
}

func pressKeyOperation(key string) string {
	return fmt.Sprintf("press key %q", key)
}

func isSubmitKey(key string) bool {
	return key == "Enter" || key == "Tab"
}

func deliveryVerificationPollAttempts() int {
	if deliveryVerificationTimeout <= 0 || deliveryVerificationPollInterval <= 0 {
		return 1
	}
	return int(deliveryVerificationTimeout/deliveryVerificationPollInterval) + 1
}

func boolToPaneStateValue(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func paneBufferName(session *TmuxSession) string {
	bufPrefix := "pane"
	if session != nil && session.Name() != "" {
		bufPrefix = session.Name()
	}
	return fmt.Sprintf("%s-%d", bufPrefix, time.Now().UnixNano())
}

func (p *TmuxPane) Close() error {
	if p == nil {
		return nil
	}
	t, err := p.targetName()
	if err != nil {
		return err
	}
	_, err = runTmuxCommand("tmux", "kill-pane", "-t", t)
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*TmuxSessionAbsentError](err); ok {
		return nil
	}
	return &KillPaneError{Target: t, Err: err}
}

type commandResult struct {
	stdout string
	stderr string
}

func runCommand(name string, args ...string) (commandResult, error) {
	return runCommandWithInput("", name, args...)
}

func runCommandWithInput(input string, name string, args ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	err := cmd.Run()

	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if ctx.Err() != nil {
		return result, fmt.Errorf("%s %s timed out: %w", name, strings.Join(args, " "), ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(result.stderr)
		if detail != "" {
			if name == "tmux" {
				message := strings.ToLower(detail)
				if strings.Contains(message, "can't find session") ||
					strings.Contains(message, "no server running") ||
					strings.Contains(message, "server exited unexpectedly") {
					return result, &TmuxSessionAbsentError{Message: detail, Err: err}
				}
			}
			return result, fmt.Errorf("%w: %s", err, detail)
		}
		return result, err
	}
	return result, nil
}

var (
	_ TmuxSessionLike = (*TmuxSession)(nil)
	_ TmuxPaneLike    = (*TmuxPane)(nil)
)
