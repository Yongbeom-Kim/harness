package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
)

const (
	claudeProgramName        = "tmux_claude"
	claudeDefaultSessionName = "claude"
	claudeSuccessLabel       = "Claude"
)

type claudeLaunchArgs struct {
	sessionName string
	attach      bool
}

type claudeDeps struct {
	newAgent    func(string) agentpkg.Agent
	newLock     func() (dirlock.Locker, error)
	openSession func(string) (tmux.TmuxSessionLike, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, claudeDeps{
		newAgent: func(sessionName string) agentpkg.Agent {
			return agentpkg.NewClaudeAgent(sessionName)
		},
		newLock: func() (dirlock.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
		openSession: func(sessionName string) (tmux.TmuxSessionLike, error) {
			return tmux.OpenTmuxSession(sessionName)
		},
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps claudeDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.newLock == nil {
		fmt.Fprintln(stderr, "claude lock constructor is not configured")
		return 1
	}
	lock, err := deps.newLock()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := lock.Acquire(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer lock.Release()

	if deps.newAgent == nil {
		fmt.Fprintln(stderr, "claude agent constructor is not configured")
		return 1
	}
	agent := deps.newAgent(parsed.sessionName)
	if agent == nil {
		fmt.Fprintln(stderr, "claude agent constructor returned nil")
		return 1
	}
	if err := agent.Start(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := agent.WaitUntilReady(); err != nil {
		_ = agent.Close()
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if parsed.attach {
		if deps.openSession == nil {
			fmt.Fprintln(stderr, "claude session opener is not configured")
			return 1
		}
		session, err := deps.openSession(agent.SessionName())
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := session.Attach(stdin, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "Launched %s in tmux session %q\n", claudeSuccessLabel, agent.SessionName())
	return 0
}

func parseArgs(args []string, stderr io.Writer) (claudeLaunchArgs, int, bool) {
	flagSet := flag.NewFlagSet(claudeProgramName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	sessionName := flagSet.String("session", claudeDefaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return claudeLaunchArgs{}, 0, false
		}
		return claudeLaunchArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return claudeLaunchArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(stderr, "invalid --session: must not be empty")
		return claudeLaunchArgs{}, 2, false
	}

	return claudeLaunchArgs{
		sessionName: *sessionName,
		attach:      *attach,
	}, 0, true
}
