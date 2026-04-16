package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/cli"
)

type scriptedResponse struct {
	stdout string
	stderr string
	err    error
}

type fakeCliTool struct {
	responses []scriptedResponse
	prompts   []string
}

type panicReader struct{}

type errReader struct {
	err error
}

func (panicReader) Read(_ []byte) (int, error) {
	panic("stdin should not be read")
}

func (r errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func (f *fakeCliTool) SendMessage(options cli.CliToolSendMessageOptions) (string, string, error) {
	f.prompts = append(f.prompts, options.Message)
	if len(f.responses) == 0 {
		return "", "", errors.New("unexpected SendMessage call")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.stdout, response.stderr, response.err
}

func TestRunImmediateApproval(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "package main\n"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: approvedMarker}}}
	stdout, stderr, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude"}, "build it\n", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, nil)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s\nstdout:\n%s", exitCode, stderr, stdout)
	}
	if len(implementer.prompts) != 1 {
		t.Fatalf("expected 1 implementer call, got %d", len(implementer.prompts))
	}
	if len(reviewer.prompts) != 1 {
		t.Fatalf("expected 1 reviewer call, got %d", len(reviewer.prompts))
	}
	if !strings.Contains(stdout, "Approved after 1 review round(s).") {
		t.Fatalf("stdout missing approval message:\n%s", stdout)
	}
}

func TestRunApprovalDetectedBySubstring(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "impl"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: "looks good " + approvedMarker + " ship it"}}}
	_, _, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude"}, "task", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, nil)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if len(implementer.prompts) != 1 || len(reviewer.prompts) != 1 {
		t.Fatalf("expected one call per role, got implementer=%d reviewer=%d", len(implementer.prompts), len(reviewer.prompts))
	}
	if !strings.Contains(reviewer.prompts[0], "Implementation:\nimpl") {
		t.Fatalf("reviewer prompt missing implementation:\n%s", reviewer.prompts[0])
	}
	if !strings.Contains(reviewer.prompts[0], reviewerSystemPrompt) {
		t.Fatalf("reviewer prompt missing system prompt:\n%s", reviewer.prompts[0])
	}
	if !strings.Contains(implementer.prompts[0], implementerSystemPrompt) {
		t.Fatalf("implementer prompt missing system prompt:\n%s", implementer.prompts[0])
	}
}

func TestRunMultipleReviewRounds(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "draft v1"}, {stdout: "draft v2"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: "handle edge cases"}, {stdout: approvedMarker}}}
	stdout, _, exitCode := runWithTools(t, []string{"--implementer", "claude", "--reviewer", "codex", "--max-iterations", "3"}, "task body", map[string]cli.CliTool{
		"claude": implementer,
		"codex":  reviewer,
	}, nil)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s", exitCode, stdout)
	}
	if len(implementer.prompts) != 2 {
		t.Fatalf("expected 2 implementer calls, got %d", len(implementer.prompts))
	}
	if !strings.Contains(implementer.prompts[1], "Your previous implementation:\ndraft v1") {
		t.Fatalf("rewrite prompt missing previous implementation:\n%s", implementer.prompts[1])
	}
	if !strings.Contains(implementer.prompts[1], "Reviewer feedback:\nhandle edge cases") {
		t.Fatalf("rewrite prompt missing reviewer feedback:\n%s", implementer.prompts[1])
	}
	if !strings.Contains(stdout, "Final implementation\ndraft v2") {
		t.Fatalf("stdout missing final implementation:\n%s", stdout)
	}
}

func TestRunNonConvergence(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "draft v1"}, {stdout: "draft v2"}, {stdout: "draft v3"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: "fix issue 1"}, {stdout: "fix issue 2"}}}
	stdout, _, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "2"}, "task", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, nil)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stdout, "Did not converge after 2 iterations.") {
		t.Fatalf("stdout missing non-convergence message:\n%s", stdout)
	}
	if len(implementer.prompts) != 3 {
		t.Fatalf("expected 3 implementer calls, got %d", len(implementer.prompts))
	}
}

func TestRunImplementerFailure(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "partial", stderr: "warn", err: errors.New("boom")}}}
	reviewer := &fakeCliTool{}
	_, stderr, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude"}, "task", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, nil)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr, "warn") || !strings.Contains(stderr, "agent invocation failed: boom") {
		t.Fatalf("stderr missing failure details:\n%s", stderr)
	}
	if len(reviewer.prompts) != 0 {
		t.Fatalf("reviewer should not have been called")
	}
}

func TestRunReviewerFailure(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "draft v1"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: "feedback", stderr: "review warn", err: errors.New("review failed")}}}
	_, stderr, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude"}, "task", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, nil)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr, "review warn") || !strings.Contains(stderr, "agent invocation failed: review failed") {
		t.Fatalf("stderr missing reviewer failure details:\n%s", stderr)
	}
	if len(implementer.prompts) != 1 {
		t.Fatalf("expected implementer to run once, got %d", len(implementer.prompts))
	}
	if len(reviewer.prompts) != 1 {
		t.Fatalf("expected reviewer to run once, got %d", len(reviewer.prompts))
	}
}

func TestRunValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		stdin    string
		env      map[string]string
		expected string
	}{
		{name: "invalid implementer", args: []string{"--implementer", "bad", "--reviewer", "claude"}, stdin: "task", expected: "unknown backend: bad"},
		{name: "invalid reviewer", args: []string{"--implementer", "codex", "--reviewer", "bad"}, stdin: "task", expected: "unknown backend: bad"},
		{name: "empty stdin", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: "", expected: "task from stdin must not be empty"},
		{name: "whitespace stdin", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: "  \n\t", expected: "task from stdin must not be empty"},
		{name: "invalid flag max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "abc"}, stdin: "task", expected: "invalid --max-iterations"},
		{name: "zero flag max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "0"}, stdin: "task", expected: "invalid --max-iterations"},
		{name: "invalid env max iterations", args: []string{"--implementer", "codex", "--reviewer", "claude"}, stdin: "task", env: map[string]string{"MAX_ITERATIONS": "abc"}, expected: "invalid MAX_ITERATIONS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, exitCode := runWithTools(t, tt.args, tt.stdin, map[string]cli.CliTool{
				"codex":  &fakeCliTool{},
				"claude": &fakeCliTool{},
			}, tt.env)
			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d\nstderr:\n%s", exitCode, stderr)
			}
			if !strings.Contains(stderr, tt.expected) {
				t.Fatalf("stderr %q did not contain %q", stderr, tt.expected)
			}
		})
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	stdout, stderr, exitCode := runWithTools(t, []string{"-h"}, "", nil, nil)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s\nstdout:\n%s", exitCode, stderr, stdout)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "Usage of implement-with-reviewer:") {
		t.Fatalf("stderr missing usage text:\n%s", stderr)
	}
	if strings.Contains(stderr, "missing required flag") {
		t.Fatalf("help output should not continue into validation:\n%s", stderr)
	}
}

func TestRunReadTaskErrorExitsOne(t *testing.T) {
	readErr := errors.New("read failed")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--implementer", "codex", "--reviewer", "claude"}, runnerConfig{
		stdin:  errReader{err: readErr},
		stdout: &stdout,
		stderr: &stderr,
		getenv: func(string) string { return "" },
		factory: func(name string) (cli.CliTool, error) {
			return &fakeCliTool{}, nil
		},
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

			exitCode := run(tt.args, runnerConfig{
				stdin:  panicReader{},
				stdout: &stdout,
				stderr: &stderr,
				getenv: func(string) string { return "" },
				factory: func(name string) (cli.CliTool, error) {
					if name == "bad" {
						return nil, errors.New("unknown backend: bad (expected codex or claude)")
					}
					return &fakeCliTool{}, nil
				},
			})

			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d", exitCode)
			}
			if !strings.Contains(stderr.String(), tt.expected) {
				t.Fatalf("stderr %q did not contain %q", stderr.String(), tt.expected)
			}
		})
	}
}

func TestRunMaxIterationPrecedence(t *testing.T) {
	implementer := &fakeCliTool{responses: []scriptedResponse{{stdout: "draft v1"}, {stdout: "draft v2"}}}
	reviewer := &fakeCliTool{responses: []scriptedResponse{{stdout: "needs fix"}}}
	stdout, _, exitCode := runWithTools(t, []string{"--implementer", "codex", "--reviewer", "claude", "--max-iterations", "1"}, "task", map[string]cli.CliTool{
		"codex":  implementer,
		"claude": reviewer,
	}, map[string]string{"MAX_ITERATIONS": "5"})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stdout, "Did not converge after 1 iterations.") {
		t.Fatalf("stdout missing flag precedence result:\n%s", stdout)
	}
	if len(reviewer.prompts) != 1 {
		t.Fatalf("expected reviewer to run once, got %d", len(reviewer.prompts))
	}
}

func TestImplementWithReviewerIntegration(t *testing.T) {
	if os.Getenv("HARNESS_RUN_INTEGRATION") != "1" {
		t.Skip("set HARNESS_RUN_INTEGRATION=1 to run the live integration test")
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))

	implementer := os.Getenv("HARNESS_IMPLEMENTER_BACKEND")
	if implementer == "" {
		implementer = "codex"
	}
	reviewer := os.Getenv("HARNESS_REVIEWER_BACKEND")
	if reviewer == "" {
		reviewer = implementer
	}

	prompt := "Create a Go source file snippet that defines a package named demo and a constant named HarnessIntegrationToken with value HARNESS_INTEGRATION_TOKEN. Return code only.\n"

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/implement-with-reviewer", "--implementer", implementer, "--reviewer", reviewer, "--max-iterations", "1")
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(prompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("integration command failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	stdoutText := stdout.String()
	if !strings.Contains(stdoutText, "Implementer : ") {
		t.Fatalf("stdout missing implementer header:\n%s", stdoutText)
	}
	if !strings.Contains(stdoutText, "Reviewer    : ") {
		t.Fatalf("stdout missing reviewer header:\n%s", stdoutText)
	}
	if !strings.Contains(stdoutText, approvedMarker) && !strings.Contains(stdoutText, "Approved after 1 review round(s).") {
		t.Fatalf("stdout missing approval markers:\n%s", stdoutText)
	}
}

func runWithTools(t *testing.T, args []string, stdin string, tools map[string]cli.CliTool, env map[string]string) (string, string, int) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(args, runnerConfig{
		stdin:  strings.NewReader(stdin),
		stdout: &stdout,
		stderr: &stderr,
		getenv: func(key string) string {
			if env == nil {
				return ""
			}
			return env[key]
		},
		factory: func(name string) (cli.CliTool, error) {
			tool, ok := tools[name]
			if !ok {
				return nil, errors.New("unknown backend: " + name + " (expected codex or claude)")
			}
			return tool, nil
		},
	})
	return stdout.String(), stderr.String(), exitCode
}
