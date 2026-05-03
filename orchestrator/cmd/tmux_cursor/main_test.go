package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session"
)

type fakeCursorSession struct {
	name       string
	config     session.Config
	startErr   error
	attachErr  error
	started    bool
	attached   bool
	attachOpts session.AttachOptions
}

func (s *fakeCursorSession) SessionName() string { return s.name }
func (s *fakeCursorSession) Start() error {
	s.started = true
	return s.startErr
}
func (s *fakeCursorSession) Attach(opts session.AttachOptions) error {
	s.attached = true
	s.attachOpts = opts
	if opts.BeforeAttach != nil {
		opts.BeforeAttach(session.AttachInfo{SessionName: s.name, MkpipePath: "/tmp/.cursor-dev.mkpipe"})
	}
	return s.attachErr
}

func TestRunUsesDefaultCursorSessionName(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-main"}
	var stdout bytes.Buffer
	exitCode := run(nil, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.SessionName != "cursor" {
				t.Fatalf("unexpected default session name: %q", config.SessionName)
			}
			if config.LockPolicy == nil {
				t.Fatal("expected lock policy")
			}
			fake.config = config
			return fake
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stdout.String(), `Launched Cursor in tmux session "cursor-main"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunLaunchesCursorAndPrintsBanner(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--session", "dev"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.SessionName != "dev" {
				t.Fatalf("unexpected session name: %q", config.SessionName)
			}
			if config.LockPolicy == nil {
				t.Fatal("expected lock policy")
			}
			return fake
		},
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, `Launched Cursor in tmux session "cursor-dev"`) {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunAttachesCursorSessionWithoutBanner(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession { return fake },
	})
	if exitCode != 0 || fake.started || !fake.attached {
		t.Fatalf("exit=%d started=%v attached=%v", exitCode, fake.started, fake.attached)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no pre-attach banner", stdout.String())
	}
}

func TestRunCursorMkpipePassesConfigAndPrintsStatusFromHook(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe", "./custom.pipe"}, nil, &stdout, io.Discard, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			if config.Mkpipe == nil || config.Mkpipe.Path != "./custom.pipe" {
				t.Fatalf("unexpected mkpipe config: %+v", config.Mkpipe)
			}
			return fake
		},
	})
	if exitCode != 0 || !fake.attached {
		t.Fatalf("exit=%d attached=%v", exitCode, fake.attached)
	}
	want := "Attaching Cursor tmux session \"cursor-dev\" with mkpipe \"/tmp/.cursor-dev.mkpipe\"\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReturnsErrorWhenCursorSessionConstructorIsNil(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, cursorDeps{})
	if exitCode != 1 || !strings.Contains(stderr.String(), "cursor session constructor is not configured") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunReturnsErrorWhenCursorSessionConstructorReturnsNil(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession { return nil },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "cursor session constructor returned nil") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunCursorAttachFailureReturnsError(t *testing.T) {
	fake := &fakeCursorSession{name: "cursor-dev", attachErr: errors.New("attach failed")}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, io.Discard, &stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession { return fake },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestParseArgsSupportsCursorMkpipeForms(t *testing.T) {
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

func TestParseArgsRejectsCursorUsageErrors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSubstring string
	}{
		{name: "positional", args: []string{"extra"}, wantSubstring: "unexpected positional arguments"},
		{name: "blank_session", args: []string{"--session", "  "}, wantSubstring: "invalid --session"},
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

func TestRunHelpReturnsSuccess(t *testing.T) {
	exitCode := run([]string{"-h"}, nil, io.Discard, io.Discard, cursorDeps{})
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}
