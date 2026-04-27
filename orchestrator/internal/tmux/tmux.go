// Package tmux runs the `tmux` binary: [NewTmuxSession] creates a detached session (default shell),
// then [TmuxSession.NewPane] returns the default pane on first use and creates additional panes on
// later calls; callers start workloads via [TmuxPaneLike.SendText].
// Contracts are in interfaces.go.
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

var (
	execCommand         = exec.CommandContext
	execCommandStdio    = exec.Command
	runCommand          = defaultRunCommand
	runCommandWithInput = defaultRunCommandWithInput
)

// TmuxSession is the concrete tmux session. [NewTmuxSession] creates a single-window session with
// the default shell in the only pane. The first [TmuxSession.NewPane] returns a handle to that
// default pane; later calls create additional tmux panes whose lifecycle remains tied to the session
// (I/O and [TmuxSession.Close]).
//
// AttachTarget is for operators (e.g. `tmux attach -t`); the runner only uses [TmuxSession.Name] for
// metadata and log messages.
type TmuxSession struct {
	name                string
	target              string // -t for pane commands, typically the default session pane or session:0.0
	defaultPaneReturned bool
}

func (s *TmuxSession) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}

// AttachTarget is a value suitable for `tmux attach -t` (the session name).
func (s *TmuxSession) AttachTarget() string {
	if s == nil {
		return ""
	}
	return s.name
}

// Attach hands the provided stdio streams to `tmux attach-session -t <session>`.
func (s *TmuxSession) Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if s == nil {
		return fmt.Errorf("nil TmuxSession")
	}
	if s.name == "" {
		return fmt.Errorf("tmux session: empty session name")
	}
	cmd := execCommandStdio("tmux", "attach-session", "-t", s.name)
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

// Close issues kill-session for this session, or no-op if the session is already gone.
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
	if isTmuxSessionAbsentError(err) {
		return nil
	}
	return &KillSessionError{SessionName: s.name, Err: err}
}

func (s *TmuxSession) hasSession() (bool, error) {
	_, err := runCommand("tmux", "has-session", "-t", s.name)
	if err == nil {
		return true, nil
	}
	if isTmuxSessionAbsentError(err) {
		return false, nil
	}
	return false, &HasSessionError{SessionName: s.name, Err: err}
}

// NewTmuxSession runs `tmux new-session -d -s name` (no initial command) so the default window/pane
// starts the usual login/interactive shell. The caller starts Codex, Claude, etc. by sending the
// launch line on the pane after [TmuxSession.NewPane] (see cli.newPersistentSession). The [runCommand]
// hook is overridable in tests.
func NewTmuxSession(name string) (*TmuxSession, error) {
	if name == "" {
		return nil, fmt.Errorf("tmux session name must not be empty")
	}
	if _, err := runCommand("tmux", "new-session", "-d", "-s", name); err != nil {
		return nil, &NewSessionError{SessionName: name, Err: err}
	}
	paneTarget := defaultPaneTarget(name)
	return &TmuxSession{name: name, target: paneTarget}, nil
}

// NewPane returns the session's default pane on first use, then creates and returns additional panes.
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
	// One window, one pane: explicit pane target; session name alone is also accepted for many tmux
	// subcommands but this matches common attach patterns.
	if sessionName == "" {
		return "%0"
	}
	return sessionName + ":0.0"
}

// TmuxPane implements [TmuxPaneLike] for a single known pane.
type TmuxPane struct {
	target  string
	session *TmuxSession
}

func (p *TmuxPane) SendText(text string) error {
	if p == nil {
		return fmt.Errorf("nil TmuxPane")
	}
	t := p.target
	if t == "" {
		if p.session != nil {
			t = p.session.Name()
		}
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
	if t == "" {
		if p.session != nil {
			t = p.session.Name()
		}
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

func defaultRunCommand(name string, args ...string) (commandResult, error) {
	return defaultRunCommandWithInput("", name, args...)
}

func defaultRunCommandWithInput(input string, name string, args ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	cmd := execCommand(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
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

func isTmuxSessionAbsentError(err error) bool {
	for err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "can't find session") ||
			strings.Contains(message, "no server running") ||
			strings.Contains(message, "server exited unexpectedly") {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

var (
	_ TmuxSessionLike = (*TmuxSession)(nil)
	_ TmuxPaneLike    = (*TmuxPane)(nil)
)
