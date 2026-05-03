package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime"
	runtimetmux "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
)

type fakeWorkflowLock struct {
	releaseErr   error
	acquireCalls int
	releaseCalls int
	events       *[]string
}

func (l *fakeWorkflowLock) Acquire() error {
	l.acquireCalls++
	return nil
}

func (l *fakeWorkflowLock) Release() error {
	l.releaseCalls++
	if l.events != nil {
		*l.events = append(*l.events, "lock_release")
	}
	return l.releaseErr
}

type fakeWorkflowPane struct{}

func (p *fakeWorkflowPane) SendText(string) error    { return nil }
func (p *fakeWorkflowPane) PressKey(string) error    { return nil }
func (p *fakeWorkflowPane) Capture() (string, error) { return "", nil }
func (p *fakeWorkflowPane) Close() error             { return nil }

type fakeWorkflowSession struct {
	name        string
	panes       []runtimetmux.TmuxPaneLike
	newPaneCall int
	closed      bool
	events      *[]string
	attachFn    func() error
}

func (s *fakeWorkflowSession) Name() string { return s.name }
func (s *fakeWorkflowSession) Attach(io.Reader, io.Writer, io.Writer) error {
	if s.events != nil {
		*s.events = append(*s.events, "attach")
	}
	if s.attachFn != nil {
		return s.attachFn()
	}
	return nil
}
func (s *fakeWorkflowSession) Close() error {
	s.closed = true
	if s.events != nil {
		*s.events = append(*s.events, "session_close")
	}
	return nil
}
func (s *fakeWorkflowSession) NewPane() (runtimetmux.TmuxPaneLike, error) {
	pane := s.panes[s.newPaneCall]
	s.newPaneCall++
	return pane, nil
}

type fakeWorkflowRuntime struct {
	name             string
	config           agentruntime.Config
	startMkpipePath  string
	startErr         error
	sendPromptNowErr error
	started          int
	stopMkpipe       int
	prompts          []string
	mkpipeErrors     chan error
	events           *[]string
	eventPrefix      string
}

func (r *fakeWorkflowRuntime) SessionName() string { return r.name }
func (r *fakeWorkflowRuntime) Start() (agentruntime.StartInfo, error) {
	r.started++
	if r.events != nil {
		*r.events = append(*r.events, r.eventPrefix+"_start")
	}
	if r.startErr != nil {
		return agentruntime.StartInfo{}, r.startErr
	}
	info := agentruntime.StartInfo{}
	if r.config.Mkpipe != nil {
		if r.events != nil {
			*r.events = append(*r.events, r.eventPrefix+"_start_mkpipe")
		}
		if r.mkpipeErrors == nil {
			r.mkpipeErrors = make(chan error, 8)
		}
		info.Mkpipe = &agentruntime.StartedMkpipe{Path: r.startMkpipePath}
	}
	return info, nil
}
func (r *fakeWorkflowRuntime) MkpipeErrors() <-chan error { return r.mkpipeErrors }
func (r *fakeWorkflowRuntime) StopMkpipe() error {
	r.stopMkpipe++
	if r.events != nil {
		*r.events = append(*r.events, r.eventPrefix+"_stop_mkpipe")
	}
	if r.mkpipeErrors != nil {
		close(r.mkpipeErrors)
		r.mkpipeErrors = nil
	}
	return nil
}
func (r *fakeWorkflowRuntime) SendPromptNow(prompt string) error {
	r.prompts = append(r.prompts, prompt)
	if r.events != nil {
		*r.events = append(*r.events, r.eventPrefix+"_prompt_now")
	}
	return r.sendPromptNowErr
}

func TestRunBootstrapsSharedSessionStartsBothRuntimeMkpipesThenSeedsPrompts(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 34, 56, 0, time.UTC)
	sessionName := generateSessionName(now)
	events := []string{}
	lock := &fakeWorkflowLock{events: &events}
	session := &fakeWorkflowSession{
		name:   sessionName,
		panes:  []runtimetmux.TmuxPaneLike{&fakeWorkflowPane{}, &fakeWorkflowPane{}},
		events: &events,
	}
	implementer := &fakeWorkflowRuntime{
		name:            sessionName,
		startMkpipePath: "/abs/implementer.pipe",
		events:          &events,
		eventPrefix:     "implementer",
	}
	reviewer := &fakeWorkflowRuntime{
		name:            sessionName,
		startMkpipePath: "/abs/reviewer.pipe",
		events:          &events,
		eventPrefix:     "reviewer",
	}

	var stdout bytes.Buffer
	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude", "ship it"}, nil, &stdout, io.Discard, workflowDeps{
		now:            func() time.Time { return now },
		newLock:        func() (workflowLock, error) { return lock, nil },
		newTmuxSession: func(name string) (workflowTmuxSession, error) { return session, nil },
		newRuntime: func(name backendName, tmuxSession workflowTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) workflowRuntime {
			if config.Mkpipe == nil {
				t.Fatal("expected mkpipe config")
			}
			switch name {
			case backendCodex:
				if !strings.HasSuffix(config.Mkpipe.BasenameOverride, "-implementer") {
					t.Fatalf("unexpected implementer basename override: %q", config.Mkpipe.BasenameOverride)
				}
				implementer.config = config
				return implementer
			case backendClaude:
				if !strings.HasSuffix(config.Mkpipe.BasenameOverride, "-reviewer") {
					t.Fatalf("unexpected reviewer basename override: %q", config.Mkpipe.BasenameOverride)
				}
				reviewer.config = config
				return reviewer
			default:
				t.Fatalf("unexpected backend: %q", name)
				return nil
			}
		},
	})

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	wantEvents := []string{
		"implementer_start",
		"implementer_start_mkpipe",
		"reviewer_start",
		"reviewer_start_mkpipe",
		"implementer_prompt_now",
		"reviewer_prompt_now",
		"attach",
		"implementer_stop_mkpipe",
		"reviewer_stop_mkpipe",
		"lock_release",
	}
	if !slices.Equal(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	wantLine := "Attaching implement-with-reviewer tmux session \"" + sessionName + "\" (implementer=codex, reviewer=claude)\n"
	if got := stdout.String(); got != wantLine {
		t.Fatalf("stdout = %q, want %q", got, wantLine)
	}
	if len(implementer.prompts) != 1 || !strings.Contains(implementer.prompts[0], "/abs/reviewer.pipe") || !strings.Contains(implementer.prompts[0], markerImplementationReady) {
		t.Fatalf("unexpected implementer prompt: %q", implementer.prompts)
	}
	if len(reviewer.prompts) != 1 || !strings.Contains(reviewer.prompts[0], "/abs/implementer.pipe") || !strings.Contains(strings.ToLower(reviewer.prompts[0]), "wait for the implementer") {
		t.Fatalf("unexpected reviewer prompt: %q", reviewer.prompts)
	}
	if session.closed {
		t.Fatal("shared tmux session should remain alive after attach returns")
	}
}

func TestRunFailsIfRuntimeMkpipeReportsDeliveryErrorBeforeAttach(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 34, 56, 0, time.UTC)
	events := []string{}
	lock := &fakeWorkflowLock{events: &events}
	session := &fakeWorkflowSession{
		name:   generateSessionName(now),
		panes:  []runtimetmux.TmuxPaneLike{&fakeWorkflowPane{}, &fakeWorkflowPane{}},
		events: &events,
	}
	implementer := &fakeWorkflowRuntime{
		name:            session.name,
		startMkpipePath: "/abs/implementer.pipe",
		mkpipeErrors:    make(chan error, 8),
		events:          &events,
		eventPrefix:     "implementer",
	}
	reviewer := &fakeWorkflowRuntime{
		name:            session.name,
		startMkpipePath: "/abs/reviewer.pipe",
		mkpipeErrors:    make(chan error, 8),
		events:          &events,
		eventPrefix:     "reviewer",
	}
	reviewer.mkpipeErrors <- errors.New("bootstrap delivery failed")
	var stderr bytes.Buffer

	exitCode := run([]string{"-i", "codex", "-r", "claude", "ship it"}, nil, io.Discard, &stderr, workflowDeps{
		now:            func() time.Time { return now },
		newLock:        func() (workflowLock, error) { return lock, nil },
		newTmuxSession: func(string) (workflowTmuxSession, error) { return session, nil },
		newRuntime: func(name backendName, tmuxSession workflowTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) workflowRuntime {
			if name == backendCodex {
				implementer.config = config
				return implementer
			}
			reviewer.config = config
			return reviewer
		},
	})

	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !session.closed || lock.releaseCalls != 1 {
		t.Fatalf("session.closed=%v lock.releaseCalls=%d", session.closed, lock.releaseCalls)
	}
	if implementer.stopMkpipe != 1 || reviewer.stopMkpipe != 1 {
		t.Fatalf("implementer.stop=%d reviewer.stop=%d", implementer.stopMkpipe, reviewer.stopMkpipe)
	}
	if !strings.Contains(stderr.String(), "bootstrap delivery failed") {
		t.Fatalf("stderr = %q, want bootstrap error", stderr.String())
	}
}

func TestParseArgsValidation(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantExit      int
		wantSubstring string
	}{
		{name: "missing_implementer", args: []string{"--reviewer", "claude", "ship it"}, wantExit: 2, wantSubstring: "missing required --implementer"},
		{name: "missing_reviewer", args: []string{"--implementer", "codex", "ship it"}, wantExit: 2, wantSubstring: "missing required --reviewer"},
		{name: "unsupported_backend", args: []string{"--implementer", "foo", "--reviewer", "claude", "ship it"}, wantExit: 2, wantSubstring: "unsupported backend"},
		{name: "missing_prompt", args: []string{"--implementer", "codex", "--reviewer", "claude"}, wantExit: 2, wantSubstring: "missing required prompt"},
		{name: "extra_prompt", args: []string{"--implementer", "codex", "--reviewer", "claude", "one", "two"}, wantExit: 2, wantSubstring: "unexpected extra prompt arguments"},
		{name: "blank_prompt", args: []string{"--implementer", "codex", "--reviewer", "claude", "   "}, wantExit: 2, wantSubstring: "invalid prompt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, exitCode, ok := parseArgs(tt.args, &stderr)
			if ok || exitCode != tt.wantExit || !strings.Contains(stderr.String(), tt.wantSubstring) {
				t.Fatalf("parseArgs(%v) => ok=%v exit=%d stderr=%q", tt.args, ok, exitCode, stderr.String())
			}
		})
	}
}

func TestParseArgsHelpReturnsSuccess(t *testing.T) {
	_, exitCode, ok := parseArgs([]string{"-h"}, io.Discard)
	if ok || exitCode != 0 {
		t.Fatalf("ok=%v exitCode=%d, want ok=false exitCode=0", ok, exitCode)
	}
}
