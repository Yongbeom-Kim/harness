package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime"
	runtimetmux "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type fakeCodexLock struct {
	acquireCalls int
	releaseCalls int
	releaseErr   error
}

func (l *fakeCodexLock) Acquire() error {
	l.acquireCalls++
	return nil
}

func (l *fakeCodexLock) Release() error {
	l.releaseCalls++
	return l.releaseErr
}

type fakeCodexPane struct{}

func (p *fakeCodexPane) SendText(string) error    { return nil }
func (p *fakeCodexPane) Capture() (string, error) { return "", nil }
func (p *fakeCodexPane) Close() error             { return nil }

type fakeCodexTmuxSession struct {
	name        string
	pane        runtimetmux.TmuxPaneLike
	closed      bool
	attachCalls int
	attachErr   error
	attachFn    func() error
}

func (s *fakeCodexTmuxSession) Name() string { return s.name }
func (s *fakeCodexTmuxSession) Attach(io.Reader, io.Writer, io.Writer) error {
	s.attachCalls++
	if s.attachFn != nil {
		return s.attachFn()
	}
	return s.attachErr
}
func (s *fakeCodexTmuxSession) Close() error {
	s.closed = true
	return nil
}
func (s *fakeCodexTmuxSession) NewPane() (runtimetmux.TmuxPaneLike, error) {
	return s.pane, nil
}

type fakeCodexRuntime struct {
	name            string
	config          agentruntime.Config
	startErr        error
	startCalls      int
	startMkpipePath string
	stopMkpipeCalls int
	mkpipeErrors    chan error
	events          *[]string
}

func (r *fakeCodexRuntime) SessionName() string { return r.name }
func (r *fakeCodexRuntime) Start() (agentruntime.StartInfo, error) {
	r.startCalls++
	if r.events != nil {
		*r.events = append(*r.events, "start")
	}
	if r.startErr != nil {
		return agentruntime.StartInfo{}, r.startErr
	}
	info := agentruntime.StartInfo{}
	if r.config.Mkpipe != nil {
		if r.events != nil {
			*r.events = append(*r.events, "start_mkpipe")
		}
		if r.mkpipeErrors == nil {
			r.mkpipeErrors = make(chan error, 8)
		}
		info.Mkpipe = &agentruntime.StartedMkpipe{Path: r.startMkpipePath}
	}
	return info, nil
}
func (r *fakeCodexRuntime) MkpipeErrors() <-chan error { return r.mkpipeErrors }
func (r *fakeCodexRuntime) StopMkpipe() error {
	r.stopMkpipeCalls++
	if r.events != nil {
		*r.events = append(*r.events, "stop_mkpipe")
	}
	if r.mkpipeErrors != nil {
		close(r.mkpipeErrors)
		r.mkpipeErrors = nil
	}
	return nil
}

func TestRunLaunchesCodexAndPrintsBanner(t *testing.T) {
	lock := &fakeCodexLock{}
	session := &fakeCodexTmuxSession{name: "codex-dev", pane: &fakeCodexPane{}}
	runtime := &fakeCodexRuntime{name: "codex-dev"}
	var stdout bytes.Buffer

	exitCode := run([]string{"--session", "dev"}, nil, &stdout, io.Discard, codexDeps{
		newLock: func() (codexLock, error) { return lock, nil },
		newTmuxSession: func(name string) (codexTmuxSession, error) {
			if name != "dev" {
				t.Fatalf("unexpected session name: %q", name)
			}
			return session, nil
		},
		newRuntime: func(tmuxSession codexTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) codexRuntime {
			if config.SessionName != "dev" {
				t.Fatalf("unexpected runtime config: %+v", config)
			}
			runtime.config = config
			return runtime
		},
	})

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if runtime.startCalls != 1 || session.attachCalls != 0 {
		t.Fatalf("startCalls=%d attachCalls=%d", runtime.startCalls, session.attachCalls)
	}
	if lock.acquireCalls != 1 || lock.releaseCalls != 1 {
		t.Fatalf("acquire=%d release=%d", lock.acquireCalls, lock.releaseCalls)
	}
	if got := stdout.String(); !strings.Contains(got, `Launched Codex in tmux session "codex-dev"`) {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunCodexAttachMkpipeStartsRuntimeAfterStartAndStopsItAfterAttach(t *testing.T) {
	lock := &fakeCodexLock{}
	events := []string{}
	runtime := &fakeCodexRuntime{
		name:            "codex-dev",
		startMkpipePath: "/tmp/.codex-dev.mkpipe",
		events:          &events,
	}
	session := &fakeCodexTmuxSession{
		name: "codex-dev",
		pane: &fakeCodexPane{},
		attachFn: func() error {
			events = append(events, "attach")
			return nil
		},
	}
	var stdout bytes.Buffer

	exitCode := run([]string{"--attach", "--mkpipe", "./custom.pipe"}, nil, &stdout, io.Discard, codexDeps{
		newLock:        func() (codexLock, error) { return lock, nil },
		newTmuxSession: func(string) (codexTmuxSession, error) { return session, nil },
		newRuntime: func(tmuxSession codexTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) codexRuntime {
			if config.Mkpipe == nil || config.Mkpipe.Path != "./custom.pipe" {
				t.Fatalf("unexpected mkpipe config: %+v", config.Mkpipe)
			}
			runtime.config = config
			return runtime
		},
	})

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if runtime.stopMkpipeCalls != 1 || lock.releaseCalls != 1 {
		t.Fatalf("stopMkpipeCalls=%d releaseCalls=%d", runtime.stopMkpipeCalls, lock.releaseCalls)
	}
	if got, want := stdout.String(), "Attaching Codex tmux session \"codex-dev\" with mkpipe \"/tmp/.codex-dev.mkpipe\"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if want := []string{"start", "start_mkpipe", "attach", "stop_mkpipe"}; strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestRunCodexAttachLogsRuntimeMkpipeErrorsAfterAttachBegins(t *testing.T) {
	lock := &fakeCodexLock{}
	runtime := &fakeCodexRuntime{
		name:            "codex-dev",
		startMkpipePath: "/tmp/.codex-dev.mkpipe",
		mkpipeErrors:    make(chan error, 8),
	}
	session := &fakeCodexTmuxSession{
		name: "codex-dev",
		pane: &fakeCodexPane{},
		attachFn: func() error {
			runtime.mkpipeErrors <- errors.New("mkpipe delivery failed")
			return nil
		},
	}
	var stderr bytes.Buffer

	exitCode := run([]string{"--attach", "--mkpipe"}, nil, io.Discard, &stderr, codexDeps{
		newLock:        func() (codexLock, error) { return lock, nil },
		newTmuxSession: func(string) (codexTmuxSession, error) { return session, nil },
		newRuntime: func(_ codexTmuxSession, _ runtimetmux.TmuxPaneLike, config agentruntime.Config) codexRuntime {
			runtime.config = config
			return runtime
		},
	})

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "mkpipe delivery failed") {
		t.Fatalf("stderr = %q, want logged mkpipe failure", stderr.String())
	}
}

func TestRunReturnsCodexStartFailure(t *testing.T) {
	lock := &fakeCodexLock{}
	session := &fakeCodexTmuxSession{name: "codex", pane: &fakeCodexPane{}}
	runtime := &fakeCodexRuntime{name: "codex", startErr: errors.New("not ready")}
	var stderr bytes.Buffer

	exitCode := run(nil, nil, io.Discard, &stderr, codexDeps{
		newLock:        func() (codexLock, error) { return lock, nil },
		newTmuxSession: func(string) (codexTmuxSession, error) { return session, nil },
		newRuntime: func(codexTmuxSession, runtimetmux.TmuxPaneLike, agentruntime.Config) codexRuntime {
			return runtime
		},
	})

	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !session.closed || lock.releaseCalls != 1 {
		t.Fatalf("session.closed=%v lock.releaseCalls=%d", session.closed, lock.releaseCalls)
	}
	if !strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("stderr missing start error: %q", stderr.String())
	}
}

func TestParseArgsSupportsCodexMkpipeForms(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{name: "bare_before_attach", args: []string{"--mkpipe", "--attach"}, wantPath: ""},
		{name: "bare_after_attach", args: []string{"--attach", "--mkpipe"}, wantPath: ""},
		{name: "explicit_relative", args: []string{"--session", "reviewer", "--mkpipe", "./custom.pipe", "--attach"}, wantPath: "./custom.pipe"},
		{name: "next_flag_not_consumed", args: []string{"--mkpipe", "--session", "named", "--attach"}, wantPath: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, exitCode, ok := parseArgs(tt.args, io.Discard)
			if !ok || exitCode != 0 {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d", tt.args, ok, exitCode)
			}
			if !parsed.mkpipeEnabled || parsed.mkpipePath != tt.wantPath {
				t.Fatalf("mkpipeEnabled=%v mkpipePath=%q", parsed.mkpipeEnabled, parsed.mkpipePath)
			}
		})
	}
}

func TestParseArgsRejectsCodexMkpipeUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
		{name: "duplicate", args: []string{"--attach", "--mkpipe", "--mkpipe"}, wantSubstring: "invalid --mkpipe: may be provided at most once"},
		{name: "missing_attach", args: []string{"--mkpipe"}, wantSubstring: "invalid --mkpipe: requires --attach"},
		{name: "raw_dash_path", args: []string{"--mkpipe", "-pipe", "--attach"}, wantSubstring: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, exitCode, ok := parseArgs(tt.args, &stderr)
			if ok || exitCode != 2 || !strings.Contains(stderr.String(), tt.wantSubstring) {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d stderr=%q", tt.args, ok, exitCode, stderr.String())
			}
		})
	}
}

func TestRunCodexAttachFailureReturnsError(t *testing.T) {
	lock := &fakeCodexLock{}
	session := &fakeCodexTmuxSession{name: "codex-dev", pane: &fakeCodexPane{}, attachErr: errors.New("attach failed")}
	runtime := &fakeCodexRuntime{name: "codex-dev"}
	var stderr bytes.Buffer

	exitCode := run([]string{"--attach"}, nil, io.Discard, &stderr, codexDeps{
		newLock:        func() (codexLock, error) { return lock, nil },
		newTmuxSession: func(string) (codexTmuxSession, error) { return session, nil },
		newRuntime: func(codexTmuxSession, runtimetmux.TmuxPaneLike, agentruntime.Config) codexRuntime {
			return runtime
		},
	})

	if exitCode != 1 || !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	if lock.releaseCalls != 1 {
		t.Fatalf("lock.releaseCalls = %d, want 1", lock.releaseCalls)
	}
}
