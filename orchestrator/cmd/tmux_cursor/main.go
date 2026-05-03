package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime"
	runtimetmux "github.com/Yongbeom-Kim/harness/orchestrator/internal/agentruntime/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

const (
	cursorProgramName        = "tmux_cursor"
	cursorDefaultSessionName = "cursor"
	cursorSuccessLabel       = "Cursor"
)

type cursorLaunchArgs struct {
	sessionName   string
	attach        bool
	mkpipeEnabled bool
	mkpipePath    string
}

type cursorDeps struct {
	newLock        func() (cursorLock, error)
	newTmuxSession func(string) (cursorTmuxSession, error)
	newRuntime     func(cursorTmuxSession, runtimetmux.TmuxPaneLike, agentruntime.Config) cursorRuntime
}

type cursorLock interface {
	Acquire() error
	Release() error
}

type cursorTmuxSession interface {
	Name() string
	Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	Close() error
	NewPane() (runtimetmux.TmuxPaneLike, error)
}

type cursorRuntime interface {
	SessionName() string
	Start() (agentruntime.StartInfo, error)
	MkpipeErrors() <-chan error
	StopMkpipe() error
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cursorDeps{
		newLock: func() (cursorLock, error) {
			return dirlock.NewInCurrentDirectory()
		},
		newTmuxSession: func(name string) (cursorTmuxSession, error) {
			return runtimetmux.NewTmuxSession(name)
		},
		newRuntime: func(session cursorTmuxSession, pane runtimetmux.TmuxPaneLike, config agentruntime.Config) cursorRuntime {
			return agentruntime.NewCursor(session, pane, config)
		},
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps cursorDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.newLock == nil {
		fmt.Fprintln(stderr, "cursor lock constructor is not configured")
		return 1
	}
	if deps.newTmuxSession == nil {
		fmt.Fprintln(stderr, "cursor tmux session constructor is not configured")
		return 1
	}
	if deps.newRuntime == nil {
		fmt.Fprintln(stderr, "cursor runtime constructor is not configured")
		return 1
	}

	lock, err := deps.newLock()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if lock == nil {
		fmt.Fprintln(stderr, "cursor lock constructor returned nil")
		return 1
	}
	if err := lock.Acquire(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	tmuxSession, err := deps.newTmuxSession(parsed.sessionName)
	if err != nil {
		logLockCleanup(lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	pane, err := tmuxSession.NewPane()
	if err != nil {
		cleanupBootstrapFailure(tmuxSession, lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	config := agentruntime.Config{SessionName: parsed.sessionName}
	if parsed.mkpipeEnabled {
		config.Mkpipe = &agentruntime.MkpipeConfig{Path: parsed.mkpipePath}
	}
	rt := deps.newRuntime(tmuxSession, pane, config)
	if rt == nil {
		cleanupBootstrapFailure(tmuxSession, lock, stderr)
		fmt.Fprintln(stderr, "cursor runtime constructor returned nil")
		return 1
	}

	startInfo, err := rt.Start()
	if err != nil {
		cleanupBootstrapFailure(tmuxSession, lock, stderr)
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if !parsed.attach {
		if err := lock.Release(); err != nil {
			_ = tmuxSession.Close()
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "Launched %s in tmux session %q\n", cursorSuccessLabel, rt.SessionName())
		return 0
	}

	var mkpipeDone <-chan struct{}
	if parsed.mkpipeEnabled {
		if startInfo.Mkpipe == nil || startInfo.Mkpipe.Path == "" {
			_ = rt.StopMkpipe()
			cleanupBootstrapFailure(tmuxSession, lock, stderr)
			fmt.Fprintln(stderr, "cursor runtime did not expose mkpipe path")
			return 1
		}
		if err := bootstrapMkpipeError(rt.MkpipeErrors()); err != nil {
			_ = rt.StopMkpipe()
			cleanupBootstrapFailure(tmuxSession, lock, stderr)
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "Attaching Cursor tmux session %q with mkpipe %q\n", rt.SessionName(), startInfo.Mkpipe.Path)
		mkpipeDone = logMkpipeErrors(rt.MkpipeErrors(), stderr)
	}

	attachErr := tmuxSession.Attach(stdin, stdout, stderr)

	if parsed.mkpipeEnabled {
		if err := rt.StopMkpipe(); err != nil && attachErr == nil {
			attachErr = err
		}
		if mkpipeDone != nil {
			<-mkpipeDone
		}
	}
	logLockCleanup(lock, stderr)

	if attachErr != nil {
		fmt.Fprintln(stderr, attachErr.Error())
		return 1
	}
	return 0
}

func parseArgs(args []string, stderr io.Writer) (cursorLaunchArgs, int, bool) {
	cleanArgs, mkpipeEnabled, mkpipePath, err := extractMkpipeArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return cursorLaunchArgs{}, 2, false
	}

	flagSet := flag.NewFlagSet(cursorProgramName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	sessionName := flagSet.String("session", cursorDefaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(cleanArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cursorLaunchArgs{}, 0, false
		}
		return cursorLaunchArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return cursorLaunchArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(stderr, "invalid --session: must not be empty")
		return cursorLaunchArgs{}, 2, false
	}
	if mkpipeEnabled && !*attach {
		fmt.Fprintln(stderr, "invalid --mkpipe: requires --attach")
		return cursorLaunchArgs{}, 2, false
	}

	return cursorLaunchArgs{
		sessionName:   *sessionName,
		attach:        *attach,
		mkpipeEnabled: mkpipeEnabled,
		mkpipePath:    mkpipePath,
	}, 0, true
}

func extractMkpipeArgs(args []string) ([]string, bool, string, error) {
	cleanArgs := make([]string, 0, len(args))
	mkpipeEnabled := false
	mkpipePath := ""

	for i := 0; i < len(args); i++ {
		if args[i] != "--mkpipe" {
			cleanArgs = append(cleanArgs, args[i])
			continue
		}
		if mkpipeEnabled {
			return nil, false, "", fmt.Errorf("invalid --mkpipe: may be provided at most once")
		}
		mkpipeEnabled = true
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			mkpipePath = args[i+1]
			i++
		}
	}

	return cleanArgs, mkpipeEnabled, mkpipePath, nil
}

func bootstrapMkpipeError(errs <-chan error) error {
	if errs == nil {
		return nil
	}
	select {
	case err, ok := <-errs:
		if !ok {
			return nil
		}
		return err
	default:
		return nil
	}
}

func logMkpipeErrors(errs <-chan error, stderr io.Writer) <-chan struct{} {
	done := make(chan struct{})
	if errs == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		for err := range errs {
			fmt.Fprintln(stderrOrDiscard(stderr), err.Error())
		}
	}()
	return done
}

func cleanupBootstrapFailure(session cursorTmuxSession, lock cursorLock, stderr io.Writer) {
	if session != nil {
		_ = session.Close()
	}
	logLockCleanup(lock, stderr)
}

func logLockCleanup(lock cursorLock, stderr io.Writer) {
	if lock == nil {
		return
	}
	if err := lock.Release(); err != nil {
		fmt.Fprintf(stderrOrDiscard(stderr), "lock cleanup failed: %v\n", err)
	}
}

func stderrOrDiscard(stderr io.Writer) io.Writer {
	if stderr == nil {
		return io.Discard
	}
	return stderr
}
