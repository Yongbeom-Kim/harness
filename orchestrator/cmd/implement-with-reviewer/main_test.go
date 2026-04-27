package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/implementwithreviewer"
)

type panicReader struct{}

type errReader struct {
	err error
}

type runRecorder struct {
	configs []implementwithreviewer.RunConfig
	err     error
}

type fakeLock struct {
	acquireErr   error
	acquireCalls int
	releaseCalls int
}

func (panicReader) Read(_ []byte) (int, error) {
	panic("stdin should not be read")
}

func (r errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func (r *runRecorder) run(_ context.Context, cfg implementwithreviewer.RunConfig) error {
	r.configs = append(r.configs, cfg)
	return r.err
}

func (l *fakeLock) Acquire() error {
	l.acquireCalls++
	return l.acquireErr
}

func (l *fakeLock) Release() error {
	l.releaseCalls++
	return nil
}

func TestRunValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		stdin    string
		env      map[string]string
		expected string
	}{
		{name: "missing implementer", args: []string{"--reviewer", "claude"}, stdin: "task", expected: "missing required flag: --implementer"},
		{name: "missing reviewer", args: []string{"--implementer", "codex"}, stdin: "task", expected: "missing required flag: --reviewer"},
		{name: "empty stdin", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: "", expected: "task from stdin must not be empty"},
		{name: "whitespace stdin", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: " \n\t", expected: "task from stdin must not be empty"},
		{name: "invalid flag max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "abc"}, stdin: "task", expected: "invalid --max-iterations"},
		{name: "zero flag max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "0"}, stdin: "task", expected: "invalid --max-iterations"},
		{name: "invalid env max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: "task", env: map[string]string{"MAX_ITERATIONS": "abc"}, expected: "invalid MAX_ITERATIONS"},
		{name: "unexpected positional arg", args: []string{"--implementer", "codex", "--reviewer", "claude", "extra"}, stdin: "task", expected: "unexpected positional arguments: extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			recorder := &runRecorder{}

			exitCode := run(tt.args, runnerConfig{
				stdin:  strings.NewReader(tt.stdin),
				stdout: &stdout,
				stderr: &stderr,
				lock:   &fakeLock{},
				getenv: func(key string) string {
					if tt.env == nil {
						return ""
					}
					return tt.env[key]
				},
				validateBackend: func(string) error { return nil },
				run:             recorder.run,
			})

			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d\nstderr:\n%s", exitCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.expected) {
				t.Fatalf("stderr %q did not contain %q", stderr.String(), tt.expected)
			}
			if len(recorder.configs) != 0 {
				t.Fatalf("runner should not be invoked on validation error, got %d call(s)", len(recorder.configs))
			}
		})
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	recorder := &runRecorder{}

	exitCode := run([]string{"-h"}, runnerConfig{
		stdin:           strings.NewReader(""),
		stdout:          &stdout,
		stderr:          &stderr,
		lock:            &fakeLock{},
		getenv:          func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage of implement-with-reviewer:") {
		t.Fatalf("stderr missing usage text:\n%s", stderr.String())
	}
	if len(recorder.configs) != 0 {
		t.Fatalf("runner should not be invoked for help, got %d call(s)", len(recorder.configs))
	}
}

func TestRunRejectsInvalidBackendBeforeReadingStdin(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{name: "invalid implementer", args: []string{"--implementer", "bad", "--reviewer", "claude"}, expected: "unknown backend: bad"},
		{name: "invalid reviewer", args: []string{"--implementer", "codex", "--reviewer", "bad"}, expected: "unknown backend: bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			recorder := &runRecorder{}

			exitCode := run(tt.args, runnerConfig{
				stdin:  panicReader{},
				stdout: &stdout,
				stderr: &stderr,
				lock:   &fakeLock{},
				getenv: func(string) string { return "" },
				validateBackend: func(name string) error {
					if name == "bad" {
						return errors.New("unknown backend: bad (expected codex or claude)")
					}
					return nil
				},
				run: recorder.run,
			})

			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d", exitCode)
			}
			if !strings.Contains(stderr.String(), tt.expected) {
				t.Fatalf("stderr %q did not contain %q", stderr.String(), tt.expected)
			}
			if len(recorder.configs) != 0 {
				t.Fatalf("runner should not be invoked, got %d call(s)", len(recorder.configs))
			}
		})
	}
}

func TestRunReadTaskErrorExitsOne(t *testing.T) {
	readErr := errors.New("read failed")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	recorder := &runRecorder{}

	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:           errReader{err: readErr},
		stdout:          &stdout,
		stderr:          &stderr,
		lock:            &fakeLock{},
		getenv:          func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("failed to read stdin: %v", readErr)) {
		t.Fatalf("stderr missing read failure:\n%s", stderr.String())
	}
	if len(recorder.configs) != 0 {
		t.Fatalf("runner should not be invoked, got %d call(s)", len(recorder.configs))
	}
}

func TestRunSetsNewSession(t *testing.T) {
	recorder := &runRecorder{}
	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:           strings.NewReader("task"),
		stdout:          &bytes.Buffer{},
		stderr:          &bytes.Buffer{},
		lock:            &fakeLock{},
		getenv:          func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if len(recorder.configs) < 1 {
		t.Fatal("expected runner to be invoked")
	}
	if recorder.configs[0].NewSession == nil {
		t.Fatal("expected NewSession closure")
	}
}

func TestRunMaxIterationPrecedenceAndRunnerConfig(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	recorder := &runRecorder{}

	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "1"}, runnerConfig{
		stdin:  strings.NewReader("task body\n"),
		stdout: &stdout,
		stderr: &stderr,
		lock:   &fakeLock{},
		getenv: func(key string) string {
			if key == "MAX_ITERATIONS" {
				return "5"
			}
			return ""
		},
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", exitCode, stderr.String())
	}
	if len(recorder.configs) != 1 {
		t.Fatalf("expected runner to be invoked once, got %d call(s)", len(recorder.configs))
	}

	cfg := recorder.configs[0]
	if cfg.Task != "task body" {
		t.Fatalf("unexpected task: %q", cfg.Task)
	}
	if cfg.Implementer != "codex" || cfg.Reviewer != "claude" {
		t.Fatalf("unexpected backends: implementer=%q reviewer=%q", cfg.Implementer, cfg.Reviewer)
	}
	if cfg.MaxIterations != 1 {
		t.Fatalf("expected flag to override env max iterations, got %d", cfg.MaxIterations)
	}
	if cfg.IdleTimeout != 30*time.Minute {
		t.Fatalf("unexpected idle timeout: %s", cfg.IdleTimeout)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("expected empty output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunPropagatesRunnerExitCode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	recorder := &runRecorder{
		err: implementwithreviewer.NewExitError(1, true, errors.New("already reported")),
	}

	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:           strings.NewReader("task"),
		stdout:          &stdout,
		stderr:          &stderr,
		lock:            &fakeLock{},
		getenv:          func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stderr.String() != "" {
		t.Fatalf("expected silent runner exit to avoid duplicate stderr, got %q", stderr.String())
	}
}

func TestRunReturnsLockAcquireFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	recorder := &runRecorder{}
	lock := &fakeLock{acquireErr: errors.New("lock busy")}

	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:           strings.NewReader("task"),
		stdout:          &stdout,
		stderr:          &stderr,
		lock:            lock,
		getenv:          func(string) string { return "" },
		validateBackend: func(string) error { return nil },
		run:             recorder.run,
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "lock busy") {
		t.Fatalf("stderr %q did not contain lock error", stderr.String())
	}
	if len(recorder.configs) != 0 {
		t.Fatalf("runner should not be invoked, got %d call(s)", len(recorder.configs))
	}
	if lock.acquireCalls != 1 || lock.releaseCalls != 0 {
		t.Fatalf("unexpected lock calls: acquire=%d release=%d", lock.acquireCalls, lock.releaseCalls)
	}
}
