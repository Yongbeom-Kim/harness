package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/backend"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/dirlock"
	sessionenv "github.com/Yongbeom-Kim/harness/orchestrator/internal/session/env"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/mkpipe"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session/tmux"
)

const (
	defaultStartupReadyTimeout = 30 * time.Second
	defaultStartupQuietPeriod  = 1500 * time.Millisecond
	defaultCapturePollInterval = 250 * time.Millisecond
)

type Config struct {
	SessionName string
	Mkpipe      *MkpipeConfig
	LockPolicy  LockPolicy
}

type MkpipeConfig struct {
	Path string
}

type Lock interface {
	Acquire() error
	Release() error
}

type LockPolicy func() (Lock, error)

func CurrentDirectoryLockPolicy() LockPolicy {
	return func() (Lock, error) {
		return dirlock.NewInCurrentDirectory()
	}
}

type AttachOptions struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	BeforeAttach func(AttachInfo)
}

type AttachInfo struct {
	SessionName string
	MkpipePath  string
}

type state int

const (
	stateNew state = iota
	stateStarted
	stateClosed
)

type Session struct {
	backend     backend.Backend
	session     tmux.TmuxSessionLike
	pane        tmux.TmuxPaneLike
	sessionName string
	lockPolicy  LockPolicy
	mkpipe      *MkpipeConfig
	state       state
	deps        deps
	sendMu      sync.Mutex
}

type deps struct {
	newTmuxSession     func(string) (tmux.TmuxSessionLike, error)
	buildLaunchCommand func(string, ...string) (string, error)
	startMkpipe        func(mkpipe.Config) (mkpipe.Listener, error)
	signalContext      func() (context.Context, context.CancelFunc)
	now                func() time.Time
	sleep              func(time.Duration)
	readyTimeout       time.Duration
	quietPeriod        time.Duration
	pollInterval       time.Duration
}

func NewCodex(config Config) *Session {
	return newSession(backend.Codex{}, config)
}

func NewClaude(config Config) *Session {
	return newSession(backend.Claude{}, config)
}

func NewCursor(config Config) *Session {
	return newSession(backend.Cursor{}, config)
}

func newSession(b backend.Backend, config Config) *Session {
	sessionName := config.SessionName
	if sessionName == "" && b != nil {
		sessionName = b.DefaultSessionName()
	}
	return &Session{
		backend:     b,
		sessionName: sessionName,
		lockPolicy:  config.LockPolicy,
		mkpipe:      config.Mkpipe,
		state:       stateNew,
		deps:        defaultDeps(),
	}
}

func defaultDeps() deps {
	return deps{
		newTmuxSession:     func(name string) (tmux.TmuxSessionLike, error) { return tmux.NewTmuxSession(name) },
		buildLaunchCommand: sessionenv.BuildLaunchCommand,
		startMkpipe:        mkpipe.Start,
		signalContext: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		},
		now:   time.Now,
		sleep: time.Sleep,
	}
}

func (s *Session) SessionName() string {
	if s == nil {
		return ""
	}
	if s.session != nil {
		return s.session.Name()
	}
	return s.sessionName
}

func (s *Session) Start() error {
	if s == nil {
		return fmt.Errorf("nil Session")
	}
	if s.mkpipe != nil {
		return newError(ErrorKindState, s.sessionName, "", fmt.Errorf("mkpipe is only supported by Attach"))
	}
	if s.state != stateNew {
		return newError(ErrorKindState, s.sessionName, "", fmt.Errorf("session cannot start from %s state", s.stateName()))
	}
	lock, err := s.startup()
	if err != nil {
		return err
	}
	if lock != nil {
		if err := lock.Release(); err != nil {
			if s.session != nil {
				_ = s.session.Close()
			}
			s.session = nil
			s.pane = nil
			s.state = stateClosed
			return newError(ErrorKindClose, s.SessionName(), "", err)
		}
	}
	return nil
}

func (s *Session) Attach(opts AttachOptions) error {
	if s == nil {
		return fmt.Errorf("nil Session")
	}
	switch s.state {
	case stateNew:
		return s.attachNew(opts)
	case stateStarted:
		if opts.BeforeAttach != nil {
			opts.BeforeAttach(AttachInfo{SessionName: s.SessionName()})
		}
		if err := s.session.Attach(opts.Stdin, opts.Stdout, opts.Stderr); err != nil {
			return newError(ErrorKindAttach, s.SessionName(), "", err)
		}
		return nil
	default:
		return newError(ErrorKindState, s.sessionName, "", fmt.Errorf("session cannot attach from %s state", s.stateName()))
	}
}

func (s *Session) attachNew(opts AttachOptions) error {
	lock, err := s.startup()
	if err != nil {
		return err
	}
	cleanup := func() {}
	var listener mkpipe.Listener
	if s.mkpipe != nil {
		startMkpipe := s.deps.startMkpipe
		if startMkpipe == nil {
			s.cleanupAfterAttachPathFailure(lock)
			return newError(ErrorKindLaunch, s.SessionName(), "", fmt.Errorf("mkpipe starter is not configured"))
		}
		listener, err = startMkpipe(mkpipe.Config{
			SessionName:     s.SessionName(),
			DefaultBasename: s.backend.DefaultSessionName(),
			RequestedPath:   s.mkpipe.Path,
		})
		if err != nil {
			s.cleanupAfterAttachPathFailure(lock)
			return newError(ErrorKindLaunch, s.SessionName(), "", err)
		}
		cleanup = s.startMkpipeForwarders(listener, opts.Stderr)
	}

	signalContext := s.deps.signalContext
	if signalContext == nil {
		signalContext = defaultDeps().signalContext
	}
	ctx, stop := signalContext()
	defer stop()

	var cleanupOnce sync.Once
	releaseTransient := func() {
		cleanupOnce.Do(func() {
			cleanup()
			if lock != nil {
				if err := lock.Release(); err != nil {
					fmt.Fprintf(stderrOrDiscard(opts.Stderr), "lock cleanup failed: %v\n", err)
				}
			}
		})
	}
	go func() {
		<-ctx.Done()
		releaseTransient()
	}()

	info := AttachInfo{SessionName: s.SessionName()}
	if listener != nil {
		info.MkpipePath = listener.Path()
	}
	if opts.BeforeAttach != nil {
		opts.BeforeAttach(info)
	}
	if err := s.session.Attach(opts.Stdin, opts.Stdout, opts.Stderr); err != nil {
		releaseTransient()
		return newError(ErrorKindAttach, s.SessionName(), "", err)
	}
	releaseTransient()
	return nil
}

// cleanupAfterAttachPathFailure runs when attach-path setup fails after startup()
// succeeded (e.g. mkpipe) but before the blocking attach handoff.
func (s *Session) cleanupAfterAttachPathFailure(lock Lock) {
	if lock != nil {
		_ = lock.Release()
	}
	if s.session != nil {
		_ = s.session.Close()
	}
	s.session = nil
	s.pane = nil
	s.state = stateClosed
}

func (s *Session) startup() (Lock, error) {
	if s.sessionName == "" {
		s.state = stateClosed
		return nil, newError(ErrorKindLaunch, "", "", fmt.Errorf("session name must not be empty"))
	}
	var lock Lock
	if s.lockPolicy != nil {
		var err error
		lock, err = s.lockPolicy()
		if err != nil {
			s.state = stateClosed
			return nil, newError(ErrorKindLaunch, s.sessionName, "", err)
		}
		if lock == nil {
			s.state = stateClosed
			return nil, newError(ErrorKindLaunch, s.sessionName, "", fmt.Errorf("lock policy returned nil"))
		}
		if err := lock.Acquire(); err != nil {
			s.state = stateClosed
			return nil, newError(ErrorKindLaunch, s.sessionName, "", err)
		}
	}

	closeOnFailure := func(session tmux.TmuxSessionLike) {
		if session != nil {
			_ = session.Close()
		}
		if lock != nil {
			_ = lock.Release()
		}
		s.state = stateClosed
	}

	newTmuxSession := s.deps.newTmuxSession
	if newTmuxSession == nil {
		closeOnFailure(nil)
		return nil, newError(ErrorKindLaunch, s.sessionName, "", fmt.Errorf("tmux session constructor is not configured"))
	}
	session, err := newTmuxSession(s.sessionName)
	if err != nil {
		closeOnFailure(nil)
		return nil, newError(ErrorKindLaunch, s.sessionName, "", err)
	}
	pane, err := session.NewPane()
	if err != nil {
		closeOnFailure(session)
		return nil, newError(ErrorKindLaunch, s.sessionName, "", err)
	}
	buildLaunchCommand := s.deps.buildLaunchCommand
	if buildLaunchCommand == nil {
		closeOnFailure(session)
		return nil, newError(ErrorKindLaunch, s.sessionName, "", fmt.Errorf("launch command builder is not configured"))
	}
	if err := s.backend.Launch(pane, buildLaunchCommand); err != nil {
		closeOnFailure(session)
		return nil, newError(ErrorKindLaunch, s.sessionName, "", err)
	}

	s.session = session
	s.pane = pane
	if err := s.waitUntilReady(); err != nil {
		closeOnFailure(session)
		return nil, err
	}
	s.state = stateStarted
	return lock, nil
}

func (s *Session) waitUntilReady() error {
	if s.pane == nil {
		return newError(ErrorKindStartup, s.sessionName, "", fmt.Errorf("session has not started"))
	}
	err := s.backend.WaitUntilReady(s.pane, backend.ReadinessOptions{
		ReadyTimeout: s.readyTimeout(),
		QuietPeriod:  s.quietPeriod(),
		PollInterval: s.pollInterval(),
		Now:          s.now,
		Sleep:        s.sleep,
	})
	if err == nil {
		return nil
	}
	var readinessErr *backend.ReadinessError
	if errors.As(err, &readinessErr) {
		return newError(ErrorKindStartup, s.sessionName, readinessErr.Capture, readinessErr.Err)
	}
	return newError(ErrorKindStartup, s.sessionName, "", err)
}

func (s *Session) startMkpipeForwarders(listener mkpipe.Listener, stderr io.Writer) func() {
	var cleanupOnce sync.Once
	go func() {
		for prompt := range listener.Messages() {
			if err := s.SendPrompt(prompt); err != nil {
				fmt.Fprintf(stderrOrDiscard(stderr), "mkpipe delivery failed: %v\n", err)
			}
		}
	}()
	go func() {
		for err := range listener.Errors() {
			fmt.Fprintf(stderrOrDiscard(stderr), "mkpipe listener error: %v\n", err)
		}
	}()
	return func() {
		cleanupOnce.Do(func() {
			if err := listener.Close(); err != nil {
				fmt.Fprintf(stderrOrDiscard(stderr), "mkpipe cleanup failed: %v\n", err)
			}
		})
	}
}

func (s *Session) SendPrompt(prompt string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.state != stateStarted || s.pane == nil {
		return newError(ErrorKindCapture, s.sessionName, "", fmt.Errorf("session has not started"))
	}
	if err := s.backend.SendPrompt(s.pane, prompt); err != nil {
		return newError(ErrorKindCapture, s.sessionName, "", err)
	}
	return nil
}

func (s *Session) Capture() (string, error) {
	if s.state != stateStarted || s.pane == nil {
		return "", newError(ErrorKindCapture, s.sessionName, "", fmt.Errorf("session has not started"))
	}
	capture, err := s.pane.Capture()
	if err != nil {
		return "", newError(ErrorKindCapture, s.sessionName, "", err)
	}
	return capture, nil
}

func (s *Session) Close() error {
	if s == nil || s.state == stateClosed {
		return nil
	}
	if s.state == stateNew {
		return nil
	}
	if s.session == nil {
		s.state = stateClosed
		return nil
	}
	if err := s.session.Close(); err != nil {
		return newError(ErrorKindClose, s.SessionName(), "", err)
	}
	s.state = stateClosed
	return nil
}

func (s *Session) readyTimeout() time.Duration {
	if s.deps.readyTimeout > 0 {
		return s.deps.readyTimeout
	}
	return defaultStartupReadyTimeout
}

func (s *Session) quietPeriod() time.Duration {
	if s.deps.quietPeriod > 0 {
		return s.deps.quietPeriod
	}
	return defaultStartupQuietPeriod
}

func (s *Session) pollInterval() time.Duration {
	if s.deps.pollInterval > 0 {
		return s.deps.pollInterval
	}
	return defaultCapturePollInterval
}

func (s *Session) now() time.Time {
	if s.deps.now != nil {
		return s.deps.now()
	}
	return time.Now()
}

func (s *Session) sleep(d time.Duration) {
	if s.deps.sleep != nil {
		s.deps.sleep(d)
		return
	}
	time.Sleep(d)
}

func (s *Session) stateName() string {
	switch s.state {
	case stateNew:
		return "new"
	case stateStarted:
		return "started"
	case stateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

func stderrOrDiscard(stderr io.Writer) io.Writer {
	if stderr == nil {
		return io.Discard
	}
	return stderr
}
