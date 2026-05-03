package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime"
	runtimetmux "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

const workflowProgramName = "implement-with-reviewer"

type backendName string

const (
	backendCodex  backendName = "codex"
	backendClaude backendName = "claude"
	backendCursor backendName = "cursor"
)

type workflowArgs struct {
	implementer backendName
	reviewer    backendName
	prompt      string
}

type workflowDeps struct {
	now            func() time.Time
	newLock        func() (workflowLock, error)
	newTmuxSession func(string) (workflowTmuxSession, error)
	newRuntime     func(backendName, workflowTmuxSession, runtimetmux.TmuxPaneLike, agentruntime.Config) workflowRuntime
}

type workflowLock interface {
	Acquire() error
	Release() error
}

type workflowTmuxSession interface {
	Name() string
	Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	Close() error
	NewPane() (runtimetmux.TmuxPaneLike, error)
}

type workflowRuntime interface {
	SessionName() string
	Start() (agentruntime.StartInfo, error)
	MkpipeErrors() <-chan error
	StopMkpipe() error
	SendPromptNow(string) error
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, workflowDeps{
		now: time.Now,
		newLock: func() (workflowLock, error) {
			return dirlock.NewInCurrentDirectory()
		},
		newTmuxSession: func(name string) (workflowTmuxSession, error) {
			return runtimetmux.NewTmuxSession(name)
		},
		newRuntime: func(name backendName, session workflowTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) workflowRuntime {
			switch name {
			case backendCodex:
				return agentruntime.NewCodex(session, pane, config)
			case backendClaude:
				return agentruntime.NewClaude(session, pane, config)
			case backendCursor:
				return agentruntime.NewCursor(session, pane, config)
			default:
				return nil
			}
		},
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps workflowDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.newLock == nil {
		fmt.Fprintln(stderr, "workflow lock constructor is not configured")
		return 1
	}
	if deps.newTmuxSession == nil {
		fmt.Fprintln(stderr, "workflow tmux session constructor is not configured")
		return 1
	}
	if deps.newRuntime == nil {
		fmt.Fprintln(stderr, "workflow runtime constructor is not configured")
		return 1
	}

	sessionName := generateSessionName(deps.now())

	lock, err := deps.newLock()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if lock == nil {
		fmt.Fprintln(stderr, "workflow lock constructor returned nil")
		return 1
	}
	if err := lock.Acquire(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	tmuxSession, err := deps.newTmuxSession(sessionName)
	if err != nil {
		logWorkflowLockCleanup(lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	implementerPane, err := tmuxSession.NewPane()
	if err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	reviewerPane, err := tmuxSession.NewPane()
	if err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	implementerRuntime := deps.newRuntime(parsed.implementer, tmuxSession, implementerPane, agentruntime.Config{
		SessionName: sessionName,
		Mkpipe: &agentruntime.MkpipeConfig{
			BasenameOverride: sessionName + "-" + implementerMkpipeRoleSuffix,
		},
	})
	reviewerRuntime := deps.newRuntime(parsed.reviewer, tmuxSession, reviewerPane, agentruntime.Config{
		SessionName: sessionName,
		Mkpipe: &agentruntime.MkpipeConfig{
			BasenameOverride: sessionName + "-" + reviewerMkpipeRoleSuffix,
		},
	})
	if implementerRuntime == nil || reviewerRuntime == nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, "workflow runtime constructor returned nil")
		return 1
	}

	implementerStart, err := implementerRuntime.Start()
	if err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	reviewerStart, err := reviewerRuntime.Start()
	if err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if implementerStart.Mkpipe == nil || implementerStart.Mkpipe.Path == "" {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, "implementer runtime did not expose mkpipe path")
		return 1
	}
	if reviewerStart.Mkpipe == nil || reviewerStart.Mkpipe.Path == "" {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, "reviewer runtime did not expose mkpipe path")
		return 1
	}
	implementerPipePath := implementerStart.Mkpipe.Path
	reviewerPipePath := reviewerStart.Mkpipe.Path
	if err := bootstrapWorkflowMkpipeError(implementerRuntime.MkpipeErrors(), reviewerRuntime.MkpipeErrors()); err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if err := implementerRuntime.SendPromptNow(buildImplementerPrompt(parsed.prompt, reviewerPipePath, sessionName)); err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := reviewerRuntime.SendPromptNow(buildReviewerPrompt(parsed.prompt, implementerPipePath, sessionName)); err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := bootstrapWorkflowMkpipeError(implementerRuntime.MkpipeErrors(), reviewerRuntime.MkpipeErrors()); err != nil {
		cleanupWorkflowBootstrapFailure(tmuxSession, lock, stderr, implementerRuntime, reviewerRuntime)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	fmt.Fprintf(stdout, "Attaching implement-with-reviewer tmux session %q (implementer=%s, reviewer=%s)\n", sessionName, parsed.implementer, parsed.reviewer)
	implementerDone := logWorkflowMkpipeErrors(implementerRuntime.MkpipeErrors(), stderr)
	reviewerDone := logWorkflowMkpipeErrors(reviewerRuntime.MkpipeErrors(), stderr)

	attachErr := tmuxSession.Attach(stdin, stdout, stderr)
	stopErr := stopWorkflowMkpipe(implementerRuntime)
	if err := stopWorkflowMkpipe(reviewerRuntime); stopErr == nil {
		stopErr = err
	}
	if implementerDone != nil {
		<-implementerDone
	}
	if reviewerDone != nil {
		<-reviewerDone
	}
	logWorkflowLockCleanup(lock, stderr)

	if attachErr != nil {
		fmt.Fprintln(stderr, attachErr.Error())
		return 1
	}
	if stopErr != nil {
		fmt.Fprintln(stderr, stopErr.Error())
		return 1
	}
	return 0
}

func parseArgs(args []string, stderr io.Writer) (workflowArgs, int, bool) {
	flagSet := flag.NewFlagSet(workflowProgramName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	implementer := flagSet.String("implementer", "", "implementer backend")
	flagSet.StringVar(implementer, "i", "", "implementer backend")
	reviewer := flagSet.String("reviewer", "", "reviewer backend")
	flagSet.StringVar(reviewer, "r", "", "reviewer backend")

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return workflowArgs{}, 0, false
		}
		return workflowArgs{}, 2, false
	}

	if strings.TrimSpace(*implementer) == "" {
		fmt.Fprintln(stderr, "missing required --implementer")
		return workflowArgs{}, 2, false
	}
	if strings.TrimSpace(*reviewer) == "" {
		fmt.Fprintln(stderr, "missing required --reviewer")
		return workflowArgs{}, 2, false
	}

	implementerBackend, err := parseBackend(*implementer)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return workflowArgs{}, 2, false
	}
	reviewerBackend, err := parseBackend(*reviewer)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return workflowArgs{}, 2, false
	}

	switch flagSet.NArg() {
	case 0:
		fmt.Fprintln(stderr, "missing required prompt")
		return workflowArgs{}, 2, false
	case 1:
	default:
		fmt.Fprintf(stderr, "unexpected extra prompt arguments: %s\n", strings.Join(flagSet.Args()[1:], " "))
		return workflowArgs{}, 2, false
	}

	prompt := flagSet.Arg(0)
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(stderr, "invalid prompt: must not be empty")
		return workflowArgs{}, 2, false
	}

	return workflowArgs{
		implementer: implementerBackend,
		reviewer:    reviewerBackend,
		prompt:      prompt,
	}, 0, true
}

func parseBackend(raw string) (backendName, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(backendCodex):
		return backendCodex, nil
	case string(backendClaude):
		return backendClaude, nil
	case string(backendCursor):
		return backendCursor, nil
	default:
		return "", fmt.Errorf("unsupported backend %q: must be one of codex|claude|cursor", raw)
	}
}

func bootstrapWorkflowMkpipeError(channels ...<-chan error) error {
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		select {
		case err, ok := <-ch:
			if ok {
				return err
			}
		default:
		}
	}
	return nil
}

func logWorkflowMkpipeErrors(errs <-chan error, stderr io.Writer) <-chan struct{} {
	done := make(chan struct{})
	if errs == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		for err := range errs {
			fmt.Fprintln(workflowStderrOrDiscard(stderr), err.Error())
		}
	}()
	return done
}

func stopWorkflowMkpipe(runtime workflowRuntime) error {
	if runtime == nil {
		return nil
	}
	return runtime.StopMkpipe()
}

func cleanupWorkflowBootstrapFailure(session workflowTmuxSession, lock workflowLock, stderr io.Writer, runtimes ...workflowRuntime) {
	for _, runtime := range runtimes {
		_ = stopWorkflowMkpipe(runtime)
	}
	if session != nil {
		_ = session.Close()
	}
	logWorkflowLockCleanup(lock, stderr)
}

func logWorkflowLockCleanup(lock workflowLock, stderr io.Writer) {
	if lock == nil {
		return
	}
	if err := lock.Release(); err != nil {
		fmt.Fprintf(workflowStderrOrDiscard(stderr), "lock cleanup failed: %v\n", err)
	}
}

func workflowStderrOrDiscard(stderr io.Writer) io.Writer {
	if stderr == nil {
		return io.Discard
	}
	return stderr
}
