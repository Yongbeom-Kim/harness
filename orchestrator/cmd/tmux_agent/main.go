package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/cli"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/tmux"
)

const defaultSessionName = "agent"

type openSessionFunc func(string) (tmux.TmuxSessionLike, error)

type runnerConfig struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	openSession openSessionFunc
	buildLaunch func(string, ...string) string
}

type RunnerOption func(*runnerConfig)

type parsedArgs struct {
	sessionName string
	attach      bool
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	parsed, exitCode, ok := parseArgs(args, os.Stderr)
	if !ok {
		return exitCode
	}

	return runAgent(NewRunnerConfig(), parsed)
}

func parseArgs(args []string, stderr io.Writer) (parsedArgs, int, bool) {
	flagSet := flag.NewFlagSet("agent", flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	sessionName := flagSet.String("session", defaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return parsedArgs{}, 0, false
		}
		return parsedArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return parsedArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(stderr, "invalid --session: must not be empty")
		return parsedArgs{}, 2, false
	}

	return parsedArgs{
		sessionName: *sessionName,
		attach:      *attach,
	}, 0, true
}

func runAgent(cfg runnerConfig, parsed parsedArgs) int {
	session, err := cfg.openSession(parsed.sessionName)
	if err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}

	pane, err := session.NewPane()
	if err != nil {
		_ = session.Close()
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}

	if err := pane.SendText(cfg.buildLaunch("agent")); err != nil {
		_ = session.Close()
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}
	if parsed.attach {
		if err := session.Attach(cfg.stdin, cfg.stdout, cfg.stderr); err != nil {
			fmt.Fprintln(cfg.stderr, err.Error())
			return 1
		}
		return 0
	}

	fmt.Fprintf(cfg.stdout, "Launched Agent in tmux session %q\n", session.AttachTarget())
	return 0
}

func NewRunnerConfig(options ...RunnerOption) runnerConfig {
	cfg := runnerConfig{
		stdin:       os.Stdin,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		openSession: openTmuxSession,
		buildLaunch: cli.BuildSourcedLauncher,
	}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}
	return cfg
}

func WithStdin(stdin io.Reader) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.stdin = stdin
	}
}

func WithStdout(stdout io.Writer) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.stdout = stdout
	}
}

func WithStderr(stderr io.Writer) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.stderr = stderr
	}
}

func WithOpenSession(openSession openSessionFunc) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.openSession = openSession
	}
}

func WithBuildLaunch(buildLaunch func(string, ...string) string) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.buildLaunch = buildLaunch
	}
}

func openTmuxSession(name string) (tmux.TmuxSessionLike, error) {
	return tmux.NewTmuxSession(name)
}
