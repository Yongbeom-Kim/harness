package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session"
)

type fakeCodexSession struct {
	name       string
	config     session.Config
	startErr   error
	attachErr  error
	started    bool
	attached   bool
	attachOpts session.AttachOptions
}

func (s *fakeCodexSession) SessionName() string { return s.name }
func (s *fakeCodexSession) Start() error {
	s.started = true
	return s.startErr
}
func (s *fakeCodexSession) Attach(opts session.AttachOptions) error {
	s.attached = true
	s.attachOpts = opts
	if opts.BeforeAttach != nil {
		opts.BeforeAttach(session.AttachInfo{SessionName: s.name, MkpipePath: "/tmp/.codex-dev.mkpipe"})
	}
	return s.attachErr
}

func TestRunLaunchesCodexAndPrintsBanner(t *testing.T) {
	fake := &fakeCodexSession{name: "codex-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--session", "dev"}, nil, &stdout, io.Discard, codexDeps{
		newSession: func(config session.Config) codexSession {
			if config.SessionName != "dev" {
				t.Fatalf("unexpected session name: %q", config.SessionName)
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
	if !fake.started || fake.attached {
		t.Fatalf("expected start only, got started=%v attached=%v", fake.started, fake.attached)
	}
	if !strings.Contains(stdout.String(), `Launched Codex in tmux session "codex-dev"`) {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunAttachesCodexSession(t *testing.T) {
	fake := &fakeCodexSession{name: "codex-dev"}
	exitCode := run([]string{"--attach"}, nil, io.Discard, io.Discard, codexDeps{
		newSession: func(config session.Config) codexSession { return fake },
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if fake.started || !fake.attached {
		t.Fatalf("expected attach only, got started=%v attached=%v", fake.started, fake.attached)
	}
}

func TestRunReturnsCodexStartFailure(t *testing.T) {
	fake := &fakeCodexSession{name: "codex", startErr: errors.New("not ready")}
	var stderr bytes.Buffer
	exitCode := run(nil, nil, io.Discard, &stderr, codexDeps{
		newSession: func(config session.Config) codexSession { return fake },
	})
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("stderr missing start error: %q", stderr.String())
	}
}

func TestRunRejectsUnexpectedPositionalArgs(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{"extra"}, nil, io.Discard, &stderr, codexDeps{})
	if exitCode != 2 {
		t.Fatalf("expected exit 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unexpected positional arguments") {
		t.Fatalf("stderr missing usage error: %q", stderr.String())
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

func TestRunCodexMkpipePassesConfigAndPrintsStatusFromHook(t *testing.T) {
	fake := &fakeCodexSession{name: "codex-dev"}
	var stdout bytes.Buffer
	exitCode := run([]string{"--attach", "--mkpipe", "./custom.pipe"}, nil, &stdout, io.Discard, codexDeps{
		newSession: func(config session.Config) codexSession {
			if config.Mkpipe == nil || config.Mkpipe.Path != "./custom.pipe" {
				t.Fatalf("unexpected mkpipe config: %+v", config.Mkpipe)
			}
			return fake
		},
	})

	if exitCode != 0 || !fake.attached {
		t.Fatalf("exit=%d attached=%v", exitCode, fake.attached)
	}
	want := "Attaching Codex tmux session \"codex-dev\" with mkpipe \"/tmp/.codex-dev.mkpipe\"\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunCodexAttachFailureReturnsError(t *testing.T) {
	fake := &fakeCodexSession{name: "codex-dev", attachErr: errors.New("attach failed")}
	var stderr bytes.Buffer
	exitCode := run([]string{"--attach"}, nil, io.Discard, &stderr, codexDeps{
		newSession: func(config session.Config) codexSession { return fake },
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "attach failed") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}
