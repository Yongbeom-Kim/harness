package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/session"
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
	newSession func(session.Config) cursorSession
}

type cursorSession interface {
	SessionName() string
	Start() error
	Attach(session.AttachOptions) error
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cursorDeps{
		newSession: func(config session.Config) cursorSession {
			return session.NewCursor(config)
		},
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps cursorDeps) int {
	parsed, exitCode, ok := parseArgs(args, stderr)
	if !ok {
		return exitCode
	}

	if deps.newSession == nil {
		fmt.Fprintln(stderr, "cursor session constructor is not configured")
		return 1
	}
	config := session.Config{
		SessionName: parsed.sessionName,
		LockPolicy:  session.CurrentDirectoryLockPolicy(),
	}
	if parsed.mkpipeEnabled {
		config.Mkpipe = &session.MkpipeConfig{Path: parsed.mkpipePath}
	}
	sess := deps.newSession(config)
	if sess == nil {
		fmt.Fprintln(stderr, "cursor session constructor returned nil")
		return 1
	}
	if parsed.attach {
		err := sess.Attach(session.AttachOptions{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
			BeforeAttach: func(info session.AttachInfo) {
				if parsed.mkpipeEnabled {
					fmt.Fprintf(stdout, "Attaching Cursor tmux session %q with mkpipe %q\n", info.SessionName, info.MkpipePath)
				}
			},
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		return 0
	}

	if err := sess.Start(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintf(stdout, "Launched %s in tmux session %q\n", cursorSuccessLabel, sess.SessionName())
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
