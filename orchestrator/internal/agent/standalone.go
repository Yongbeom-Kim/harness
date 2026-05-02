package agent

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
)

type Locker interface {
	Acquire() error
	Release() error
}

type StandaloneConfig struct {
	ProgramName        string
	DefaultSessionName string
	SuccessLabel       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	Lock               Locker
	NewLock            func() (Locker, error)
	NewAgent           func(string) StandaloneAgent
	OpenSession        func(string) (tmux.TmuxSessionLike, error)
}

type StandaloneAgent interface {
	Start() error
	WaitUntilReady() error
	SessionName() string
	Close() error
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
	if cfg.NewAgent == nil {
		fmt.Fprintln(cfg.Stderr, "agent constructor is not configured")
		return 1
	}
	agent := cfg.NewAgent(parsed.sessionName)
	if agent == nil {
		fmt.Fprintln(cfg.Stderr, "agent constructor returned nil")
		return 1
	}
	if err := agent.Start(); err != nil {
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}
	if err := agent.WaitUntilReady(); err != nil {
		_ = agent.Close()
		fmt.Fprintln(cfg.Stderr, err.Error())
		return 1
	}

	if parsed.attach {
		openSession := cfg.OpenSession
		if openSession == nil {
			openSession = func(sessionName string) (tmux.TmuxSessionLike, error) {
				return tmux.OpenTmuxSession(sessionName)
			}
		}
		session, err := openSession(agent.SessionName())
		if err != nil {
			fmt.Fprintln(cfg.Stderr, err.Error())
			return 1
		}
		if err := session.Attach(cfg.Stdin, cfg.Stdout, cfg.Stderr); err != nil {
			fmt.Fprintln(cfg.Stderr, err.Error())
			return 1
		}
		return 0
	}

	fmt.Fprintf(cfg.Stdout, "Launched %s in tmux session %q\n", cfg.SuccessLabel, agent.SessionName())
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
