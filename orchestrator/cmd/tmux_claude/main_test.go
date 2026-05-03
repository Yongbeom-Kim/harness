package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session"
)

type fakeClaudeSession struct {
	name       string
	config     session.Config
	startErr   error
	attachErr  error
	started    bool
	attached   bool
	attachOpts session.AttachOptions
}

func (s *fakeClaudeSession) SessionName() string { return s.name }
func (s *fakeClaudeSession) Start() error {
	s.started = true
	return s.startErr
}
func (s *fakeClaudeSession) Attach(opts session.AttachOptions) error {
	s.attached = true
	s.attachOpts = opts
	if opts.BeforeAttach != nil {
		opts.BeforeAttach(session.AttachInfo{SessionName: s.name, MkpipePath: "/tmp/.claude-dev.mkpipe"})
	}
	return s.attachErr
}

func TestRunUsesDefaultClaudeSessionName(t *testing.T) {
	fake := &fakeClaudeSession{name: "claude-main"}
	var stdout bytes.Buffer
	exitCode := run(nil, nil, &stdout, io.Discard, claudeDeps{
		newSession: func(config session.Config) claudeSession {
			if config.SessionName != "claude" {
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
	if !strings.Contains(stdout.String(), `Launched Claude in tmux session "claude-main"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunAttachesClaudeSession(t *testing.T) {
	fake := &fakeClaudeSession{name: "claude-dev"}
	exitCode := run([]string{"--attach"}, nil, io.Discard, io.Discard, claudeDeps{
		newSession: func(config session.Config) claudeSession { return fake },
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if fake.started || !fake.attached {
		t.Fatalf("expected attach only, got started=%v attached=%v", fake.started, fake.attached)
	}
}

func TestRunReturnsClaudeStartFailure(t *testing.T) {
	fake := &fakeClaudeSession{name: "claude", startErr: errors.New("not ready")}
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, claudeDeps{
		newSession: func(config session.Config) claudeSession { return fake },
	})
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("stderr missing start error: %q", stderr.String())
	}
}

func TestRunRejectsBlankClaudeSession(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{"--session", "  "}, nil, io.Discard, &stderr, claudeDeps{})
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid --session") {
		t.Fatalf("stderr missing validation error: %q", stderr.String())
	}
}

func TestParseArgsSupportsClaudeMkpipeForms(t *testing.T) {
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

func TestParseArgsRejectsClaudeMkpipeUsageErrors(t *testing.T) {
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

func TestRunClaudeMkpipePassesConfigAndPrintsStatusFromHook(t *testing.T) {
	fake := &fakeClaudeSession{name: "claude-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe", "./custom.pipe"}, nil, &stdout, io.Discard, claudeDeps{
		newSession: func(config session.Config) claudeSession {
			if config.Mkpipe == nil || config.Mkpipe.Path != "./custom.pipe" {
				t.Fatalf("unexpected mkpipe config: %+v", config.Mkpipe)
			}
			return fake
		},
	})

	if exitCode != 0 || !fake.attached {
		t.Fatalf("exit=%d attached=%v", exitCode, fake.attached)
	}
	want := "Attaching Claude tmux session \"claude-dev\" with mkpipe \"/tmp/.claude-dev.mkpipe\"\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunClaudeAttachFailureReturnsError(t *testing.T) {
	fake := &fakeClaudeSession{name: "claude-dev", attachErr: errors.New("attach failed")}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, io.Discard, &stderr, claudeDeps{
		newSession: func(config session.Config) claudeSession { return fake },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}
