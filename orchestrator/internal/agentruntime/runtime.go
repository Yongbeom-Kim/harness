package agentruntime

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/backend"
	runtimeenv "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/env"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/mkpipe"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

const (
	defaultStartupReadyTimeout = 30 * time.Second
	defaultStartupQuietPeriod  = 1500 * time.Millisecond
	defaultCapturePollInterval = 250 * time.Millisecond
)

type Config struct {
	SessionName string
	Mkpipe      *MkpipeConfig
}

type MkpipeConfig struct {
	Path             string
	BasenameOverride string
}

type StartInfo struct {
	Mkpipe *StartedMkpipe
}

type StartedMkpipe struct {
	Path string
}

type state int

const (
	stateNew state = iota
	stateStarted
	stateClosed
)

type Runtime struct {
	backend     backend.Backend
	session     tmux.TmuxSessionLike
	pane        tmux.TmuxPaneLike
	sessionName string
	mkpipe      *MkpipeConfig
	mkpipePath  string
	listener    mkpipe.Listener
	mkpipeErrs  chan error
	state       state
	deps        deps
	sendMu      sync.Mutex
	mkpipeWG    sync.WaitGroup
}

type deps struct {
	buildLaunchCommand func(string, ...string) (string, error)
	startMkpipe        func(mkpipe.Config) (mkpipe.Listener, error)
	now                func() time.Time
	sleep              func(time.Duration)
	readyTimeout       time.Duration
	quietPeriod        time.Duration
	pollInterval       time.Duration
}

func NewCodex(session tmux.TmuxSessionLike, pane tmux.TmuxPaneLike, config Config) *Runtime {
	return newRuntime(backend.Codex{}, session, pane, config)
}

func NewClaude(session tmux.TmuxSessionLike, pane tmux.TmuxPaneLike, config Config) *Runtime {
	return newRuntime(backend.Claude{}, session, pane, config)
}

func NewCursor(session tmux.TmuxSessionLike, pane tmux.TmuxPaneLike, config Config) *Runtime {
	return newRuntime(backend.Cursor{}, session, pane, config)
}

func newRuntime(b backend.Backend, session tmux.TmuxSessionLike, pane tmux.TmuxPaneLike, config Config) *Runtime {
	sessionName := config.SessionName
	if sessionName == "" && session != nil {
		sessionName = session.Name()
	}
	if sessionName == "" && b != nil {
		sessionName = b.DefaultSessionName()
	}
	return &Runtime{
		backend:     b,
		session:     session,
		pane:        pane,
		sessionName: sessionName,
		mkpipe:      config.Mkpipe,
		state:       stateNew,
		deps:        defaultDeps(),
	}
}

func defaultDeps() deps {
	return deps{
		buildLaunchCommand: runtimeenv.BuildLaunchCommand,
		startMkpipe:        mkpipe.Start,
		now:                time.Now,
		sleep:              time.Sleep,
	}
}

func (r *Runtime) SessionName() string {
	if r == nil {
		return ""
	}
	if r.session != nil && r.session.Name() != "" {
		return r.session.Name()
	}
	return r.sessionName
}

func (r *Runtime) Start() (StartInfo, error) {
	if r == nil {
		return StartInfo{}, fmt.Errorf("nil Runtime")
	}
	if r.state != stateNew {
		return StartInfo{}, newError(ErrorKindState, r.SessionName(), "", fmt.Errorf("runtime cannot start from %s state", r.stateName()))
	}
	if r.pane == nil {
		r.state = stateClosed
		return StartInfo{}, newError(ErrorKindLaunch, r.SessionName(), "", fmt.Errorf("tmux pane is not configured"))
	}
	if r.SessionName() == "" {
		r.state = stateClosed
		return StartInfo{}, newError(ErrorKindLaunch, "", "", fmt.Errorf("session name must not be empty"))
	}

	buildLaunchCommand := r.deps.buildLaunchCommand
	if buildLaunchCommand == nil {
		r.cleanupAfterStartFailure()
		return StartInfo{}, newError(ErrorKindLaunch, r.SessionName(), "", fmt.Errorf("launch command builder is not configured"))
	}
	if err := r.backend.Launch(r.pane, buildLaunchCommand); err != nil {
		r.cleanupAfterStartFailure()
		return StartInfo{}, newError(ErrorKindLaunch, r.SessionName(), "", err)
	}
	if err := r.waitUntilReady(); err != nil {
		r.cleanupAfterStartFailure()
		return StartInfo{}, err
	}

	r.state = stateStarted
	if r.mkpipe != nil {
		if err := r.startConfiguredMkpipe(); err != nil {
			r.cleanupAfterStartFailure()
			return StartInfo{}, err
		}
	}
	info := StartInfo{}
	if r.mkpipePath != "" {
		info.Mkpipe = &StartedMkpipe{Path: r.mkpipePath}
	}
	return info, nil
}

func (r *Runtime) MkpipeErrors() <-chan error {
	if r == nil {
		return nil
	}
	return r.mkpipeErrs
}

func (r *Runtime) StopMkpipe() error {
	if r == nil || r.listener == nil {
		return nil
	}

	listener := r.listener
	errs := r.mkpipeErrs
	r.listener = nil
	r.mkpipeErrs = nil
	r.mkpipePath = ""

	err := listener.Close()
	r.mkpipeWG.Wait()
	if errs != nil {
		close(errs)
	}

	if err != nil {
		return newError(ErrorKindClose, r.SessionName(), "", err)
	}
	return nil
}

func (r *Runtime) startConfiguredMkpipe() error {
	if r.mkpipe == nil {
		return nil
	}
	if r.listener != nil {
		return newError(ErrorKindState, r.SessionName(), "", fmt.Errorf("mkpipe already started"))
	}

	startMkpipe := r.deps.startMkpipe
	if startMkpipe == nil {
		return newError(ErrorKindLaunch, r.SessionName(), "", fmt.Errorf("mkpipe starter is not configured"))
	}

	listener, err := startMkpipe(mkpipe.Config{
		SessionName:      r.SessionName(),
		BasenameOverride: r.mkpipe.BasenameOverride,
		DefaultBasename:  r.backend.DefaultSessionName(),
		RequestedPath:    r.mkpipe.Path,
	})
	if err != nil {
		return newError(ErrorKindLaunch, r.SessionName(), "", err)
	}

	r.listener = listener
	r.mkpipePath = listener.Path()
	r.mkpipeErrs = make(chan error, 16)
	r.startMkpipeForwarders(listener)
	return nil
}

func (r *Runtime) SendPrompt(prompt string) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	if r.state != stateStarted || r.pane == nil {
		return newError(ErrorKindCapture, r.SessionName(), "", fmt.Errorf("runtime has not started"))
	}
	if err := r.backend.SendPrompt(r.pane, prompt); err != nil {
		return newError(ErrorKindCapture, r.SessionName(), "", err)
	}
	return nil
}

func (r *Runtime) Capture() (string, error) {
	if r.state != stateStarted || r.pane == nil {
		return "", newError(ErrorKindCapture, r.SessionName(), "", fmt.Errorf("runtime has not started"))
	}
	capture, err := r.pane.Capture()
	if err != nil {
		return "", newError(ErrorKindCapture, r.SessionName(), "", err)
	}
	return capture, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.state == stateClosed {
		return nil
	}
	if r.listener != nil {
		if err := r.StopMkpipe(); err != nil {
			return err
		}
	}
	if r.state == stateNew {
		return nil
	}
	if r.pane == nil {
		r.state = stateClosed
		return nil
	}
	if err := r.pane.Close(); err != nil {
		return newError(ErrorKindClose, r.SessionName(), "", err)
	}
	r.state = stateClosed
	return nil
}

func (r *Runtime) waitUntilReady() error {
	err := r.backend.WaitUntilReady(r.pane, backend.ReadinessOptions{
		ReadyTimeout: r.readyTimeout(),
		QuietPeriod:  r.quietPeriod(),
		PollInterval: r.pollInterval(),
		Now:          r.now,
		Sleep:        r.sleep,
	})
	if err == nil {
		return nil
	}
	var readinessErr *backend.ReadinessError
	if errors.As(err, &readinessErr) {
		return newError(ErrorKindStartup, r.SessionName(), readinessErr.Capture, readinessErr.Err)
	}
	return newError(ErrorKindStartup, r.SessionName(), "", err)
}

func (r *Runtime) startMkpipeForwarders(listener mkpipe.Listener) {
	r.mkpipeWG.Add(2)

	go func() {
		defer r.mkpipeWG.Done()
		for prompt := range listener.Messages() {
			if err := r.SendPrompt(prompt); err != nil {
				r.emitMkpipeError(fmt.Errorf("mkpipe delivery failed: %w", err))
			}
		}
	}()

	go func() {
		defer r.mkpipeWG.Done()
		for err := range listener.Errors() {
			r.emitMkpipeError(fmt.Errorf("mkpipe listener error: %w", err))
		}
	}()
}

func (r *Runtime) emitMkpipeError(err error) {
	if err == nil || r.mkpipeErrs == nil {
		return
	}
	select {
	case r.mkpipeErrs <- err:
	default:
	}
}

func (r *Runtime) cleanupAfterStartFailure() {
	if r.listener != nil {
		_ = r.StopMkpipe()
	}
	if r.pane != nil {
		_ = r.pane.Close()
	}
	r.state = stateClosed
}

func (r *Runtime) readyTimeout() time.Duration {
	if r.deps.readyTimeout > 0 {
		return r.deps.readyTimeout
	}
	return defaultStartupReadyTimeout
}

func (r *Runtime) quietPeriod() time.Duration {
	if r.deps.quietPeriod > 0 {
		return r.deps.quietPeriod
	}
	return defaultStartupQuietPeriod
}

func (r *Runtime) pollInterval() time.Duration {
	if r.deps.pollInterval > 0 {
		return r.deps.pollInterval
	}
	return defaultCapturePollInterval
}

func (r *Runtime) now() time.Time {
	if r.deps.now != nil {
		return r.deps.now()
	}
	return time.Now()
}

func (r *Runtime) sleep(d time.Duration) {
	if r.deps.sleep != nil {
		r.deps.sleep(d)
		return
	}
	time.Sleep(d)
}

func (r *Runtime) stateName() string {
	switch r.state {
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
