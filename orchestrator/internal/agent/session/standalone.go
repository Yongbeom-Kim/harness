package session

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/launcher"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/tmux"
)

type Locker interface {
	Acquire() error
	Release() error
}

type StandaloneConfig struct {
	ProgramName        string
	DefaultSessionName string
	LaunchCommand      string
	SuccessLabel       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	OpenSession        func(sessionName string) (tmux.TmuxSessionLike, error)
	Launcher           launcher.Builder
	Lock               Locker
	NewLock            func() (Locker, error)
}

type standaloneArgs struct {
	sessionName string
	attach      bool
}

func RunStandalone(args []string, cfg StandaloneConfig) int {
	parsed, exitCode, ok := parseStandaloneArgs(args, cfg)
	if !ok {
		return exitCode
	}

	lock, err := resolveStandaloneLock(cfg)
	if err != nil {
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}
	if err := lock.Acquire(); err != nil {
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}
	defer lock.Release()

	return runStandaloneLaunch(cfg, parsed)
}

func parseStandaloneArgs(args []string, cfg StandaloneConfig) (standaloneArgs, int, bool) {
	flagSet := flag.NewFlagSet(cfg.ProgramName, flag.ContinueOnError)
	flagSet.SetOutput(cfg.Stderr)

	sessionName := flagSet.String("session", cfg.DefaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return standaloneArgs{}, 0, false
		}
		return standaloneArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(cfg.Stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return standaloneArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(cfg.Stderr, "invalid --session: must not be empty")
		return standaloneArgs{}, 2, false
	}

	return standaloneArgs{
		sessionName: *sessionName,
		attach:      *attach,
	}, 0, true
}

func runStandaloneLaunch(cfg StandaloneConfig, parsed standaloneArgs) int {
	openSession, launchBuilder, err := resolveLaunchDependencies(cfg)
	if err != nil {
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}

	session, pane, err := launchSession(openSession, launchBuilder, parsed.sessionName, cfg.LaunchCommand)
	if err != nil {
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}
	if pane == nil {
		fmt.Fprintln(cfg.Stderr, "tmux pane was not created")
		return 1
	}

	if parsed.attach {
		if err := session.Attach(cfg.Stdin, cfg.Stdout, cfg.Stderr); err != nil {
			fmt.Fprintln(cfg.Stderr, err.Error())
			return 1
		}
		return 0
	}

	fmt.Fprintf(cfg.Stdout, "Launched %s in tmux session %q\n", cfg.SuccessLabel, session.AttachTarget())
	return 0
}

func resolveStandaloneLock(cfg StandaloneConfig) (Locker, error) {
	if cfg.Lock != nil {
		return cfg.Lock, nil
	}
	if cfg.NewLock == nil {
		return nil, errors.New("lock is not configured")
	}
	return cfg.NewLock()
}

func resolveLaunchDependencies(cfg StandaloneConfig) (func(string) (tmux.TmuxSessionLike, error), launcher.Builder, error) {
	switch {
	case cfg.OpenSession == nil:
		return nil, nil, errors.New("session opener is not configured")
	case cfg.Launcher == nil:
		return nil, nil, errors.New("launcher is not configured")
	default:
		return cfg.OpenSession, cfg.Launcher, nil
	}
}
