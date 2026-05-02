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
	if _, err := runCommand("tmux", "new-session", "-d", "-s", name); err != nil {
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

func (s *TmuxSession) AttachTarget() string {
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
	_, err = runCommand("tmux", "kill-session", "-t", s.name)
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*TmuxSessionAbsentError](err); ok {
		return nil
	}
	return &KillSessionError{SessionName: s.name, Err: err}
}

func (s *TmuxSession) hasSession() (bool, error) {
	_, err := runCommand("tmux", "has-session", "-t", s.name)
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
	result, err := runCommand("tmux", "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", t)
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
	target  string
	session *TmuxSession
}

func (p *TmuxPane) SendText(text string) error {
	if p == nil {
		return fmt.Errorf("nil TmuxPane")
	}
	t := p.target
	if t == "" && p.session != nil {
		t = p.session.Name()
	}
	if t == "" {
		return fmt.Errorf("tmux pane: empty target and no session name")
	}
	bufPrefix := "pane"
	if p.session != nil {
		bufPrefix = p.session.Name()
	}
	bufferName := fmt.Sprintf("%s-%d", bufPrefix, time.Now().UnixNano())
	if _, err := runCommandWithInput(text, "tmux", "load-buffer", "-b", bufferName, "-"); err != nil {
		return &LoadBufferError{BufferName: bufferName, Err: err}
	}
	if _, err := runCommand("tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", t); err != nil {
		return &PasteBufferError{Target: t, BufferName: bufferName, Err: err}
	}
	if _, err := runCommand("tmux", "send-keys", "-t", t, "Enter"); err != nil {
		return &SendKeysError{Target: t, Keys: []string{"Enter"}, Err: err}
	}
	return nil
}

func (p *TmuxPane) Capture() (string, error) {
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
	r, err := runCommand("tmux", "capture-pane", "-p", "-J", "-S", captureHistoryStart, "-t", t)
	if err != nil {
		return "", &CapturePaneError{Target: t, Err: err}
	}
	return r.stdout, nil
}

func (p *TmuxPane) Target() string {
	if p == nil {
		return ""
	}
	if p.target != "" {
		return p.target
	}
	if p.session != nil {
		return p.session.name
	}
	return ""
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
