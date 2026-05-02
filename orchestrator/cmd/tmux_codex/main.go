package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/tmux"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/mkpipe"
)

const (
	codexProgramName        = "tmux_codex"
	codexDefaultSessionName = "codex"
	codexSuccessLabel       = "Codex"
)

type codexLaunchArgs struct {
	sessionName   string
	attach        bool
	mkpipeEnabled bool
	mkpipePath    string
}

type codexDeps struct {
	newAgent      func(string) agentpkg.Agent
	newLock       func() (dirlock.Locker, error)
	openSession   func(string) (tmux.TmuxSessionLike, error)
	startMkpipe   func(mkpipe.Config) (mkpipe.Listener, error)
	signalContext func() (context.Context, context.CancelFunc)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, codexDeps{
		newAgent: func(sessionName string) agentpkg.Agent {
			return agentpkg.NewCodexAgent(sessionName)
		},
		newLock: func() (dirlock.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
		openSession: func(sessionName string) (tmux.TmuxSessionLike, error) {
			return tmux.OpenTmuxSession(sessionName)
		},
		startMkpipe: mkpipe.Start,
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps codexDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.newLock == nil {
		fmt.Fprintln(stderr, "codex lock constructor is not configured")
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
		fmt.Fprintln(stderr, "codex agent constructor is not configured")
		return 1
	}
	agent := deps.newAgent(parsed.sessionName)
	if agent == nil {
		fmt.Fprintln(stderr, "codex agent constructor returned nil")
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
		var listener mkpipe.Listener
		cleanup := func() {}
		if parsed.mkpipeEnabled {
			if deps.startMkpipe == nil {
				_ = agent.Close()
				fmt.Fprintln(stderr, "codex mkpipe starter is not configured")
				return 1
			}
			var err error
			listener, err = deps.startMkpipe(mkpipe.Config{
				SessionName:     agent.SessionName(),
				DefaultBasename: codexDefaultSessionName,
				RequestedPath:   parsed.mkpipePath,
			})
			if err != nil {
				_ = agent.Close()
				fmt.Fprintln(stderr, err.Error())
				return 1
			}

			signalContext := deps.signalContext
			if signalContext == nil {
				signalContext = func() (context.Context, context.CancelFunc) {
					return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				}
			}
			ctx, stop := signalContext()
			defer stop()

			var cleanupOnce sync.Once
			cleanup = func() {
				cleanupOnce.Do(func() {
					if err := listener.Close(); err != nil {
						fmt.Fprintf(stderr, "mkpipe cleanup failed: %v\n", err)
					}
				})
			}
			go func() {
				<-ctx.Done()
				cleanup()
			}()
			go func() {
				for prompt := range listener.Messages() {
					if err := agent.SendPrompt(prompt); err != nil {
						fmt.Fprintf(stderr, "mkpipe delivery failed: %v\n", err)
					}
				}
			}()
			go func() {
				for err := range listener.Errors() {
					fmt.Fprintf(stderr, "mkpipe listener error: %v\n", err)
				}
			}()
		}

		if deps.openSession == nil {
			cleanup()
			if parsed.mkpipeEnabled {
				_ = agent.Close()
			}
			fmt.Fprintln(stderr, "codex session opener is not configured")
			return 1
		}
		session, err := deps.openSession(agent.SessionName())
		if err != nil {
			cleanup()
			if parsed.mkpipeEnabled {
				_ = agent.Close()
			}
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if parsed.mkpipeEnabled {
			fmt.Fprintf(stdout, "Attaching Codex tmux session %q with mkpipe %q\n", agent.SessionName(), listener.Path())
		}
		if err := session.Attach(stdin, stdout, stderr); err != nil {
			cleanup()
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		cleanup()
		return 0
	}

	fmt.Fprintf(stdout, "Launched %s in tmux session %q\n", codexSuccessLabel, agent.SessionName())
	return 0
}

func parseArgs(args []string, stderr io.Writer) (codexLaunchArgs, int, bool) {
	cleanArgs, mkpipeEnabled, mkpipePath, err := extractMkpipeArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return codexLaunchArgs{}, 2, false
	}

	flagSet := flag.NewFlagSet(codexProgramName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	sessionName := flagSet.String("session", codexDefaultSessionName, "tmux session name")
	attach := flagSet.Bool("attach", false, "attach to the tmux session after launch")

	if err := flagSet.Parse(cleanArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return codexLaunchArgs{}, 0, false
		}
		return codexLaunchArgs{}, 2, false
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return codexLaunchArgs{}, 2, false
	}
	if strings.TrimSpace(*sessionName) == "" {
		fmt.Fprintln(stderr, "invalid --session: must not be empty")
		return codexLaunchArgs{}, 2, false
	}
	if mkpipeEnabled && !*attach {
		fmt.Fprintln(stderr, "invalid --mkpipe: requires --attach")
		return codexLaunchArgs{}, 2, false
	}

	return codexLaunchArgs{
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
