package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/dirlock"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/filechannel"
)

const (
	defaultMaxIterations = 10
	defaultIdleTimeout   = 30 * time.Minute
)

var errEmptyTask = errors.New("task from stdin must not be empty")

type runFunc func(context.Context, runConfig) error

type runnerConfig struct {
	stdin                          io.Reader
	stdout                         io.Writer
	stderr                         io.Writer
	getenv                         func(string) string
	validateBackend                func(string) error
	newAgent                       func(string, string) (workflowAgent, error)
	newArtifactWriter              func(string) (artifactSink, error)
	newCommunicationChannelManager func(channelConfig) (channelManager, error)
	newRunID                       func() (string, error)
	run                            runFunc
	lock                           dirlock.Locker
	newLock                        func() (dirlock.Locker, error)
}

type stringFlag struct {
	value string
	set   bool
}

type parsedArgs struct {
	implementer   string
	reviewer      string
	maxIterations int
}

func (f *stringFlag) String() string {
	return f.value
}

func (f *stringFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], newRunnerConfig()))
}

func run(args []string, cfg runnerConfig) int {
	parsed, exitCode, ok := parseArgs(args, cfg.stderr, cfg.getenv)
	if !ok {
		return exitCode
	}

	lock, err := resolveLock(cfg)
	if err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}
	if err := lock.Acquire(); err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}
	defer lock.Release()

	return runImplementWithReviewer(cfg, parsed)
}

func parseArgs(args []string, stderr io.Writer, getenv func(string) string) (parsedArgs, int, bool) {
	if getenv == nil {
		getenv = os.Getenv
	}

	flagSet := flag.NewFlagSet("implement-with-reviewer", flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	implementer := flagSet.String("implementer", "", "implementer backend")
	reviewer := flagSet.String("reviewer", "", "reviewer backend")
	var maxIterationsFlag stringFlag
	flagSet.Var(&maxIterationsFlag, "max-iterations", "maximum review iterations")

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

	if *implementer == "" {
		fmt.Fprintln(stderr, "missing required flag: --implementer")
		return parsedArgs{}, 2, false
	}
	if *reviewer == "" {
		fmt.Fprintln(stderr, "missing required flag: --reviewer")
		return parsedArgs{}, 2, false
	}

	maxIterations := defaultMaxIterations
	var err error
	if maxIterationsFlag.set {
		maxIterations, err = parsePositiveInt(maxIterationsFlag.value, "--max-iterations")
	} else if envValue := getenv("MAX_ITERATIONS"); envValue != "" {
		maxIterations, err = parsePositiveInt(envValue, "MAX_ITERATIONS")
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return parsedArgs{}, 2, false
	}

	return parsedArgs{
		implementer:   *implementer,
		reviewer:      *reviewer,
		maxIterations: maxIterations,
	}, 0, true
}

func runImplementWithReviewer(cfg runnerConfig, parsed parsedArgs) int {
	if err := cfg.validateBackend(parsed.implementer); err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 2
	}
	if err := cfg.validateBackend(parsed.reviewer); err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 2
	}

	task, err := readTask(cfg.stdin)
	if err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		if errors.Is(err, errEmptyTask) {
			return 2
		}
		return 1
	}

	err = cfg.run(context.Background(), runConfig{
		Task:                           task,
		Implementer:                    parsed.implementer,
		Reviewer:                       parsed.reviewer,
		MaxIterations:                  parsed.maxIterations,
		IdleTimeout:                    defaultIdleTimeout,
		Stdout:                         cfg.stdout,
		Stderr:                         cfg.stderr,
		NewAgent:                       cfg.newAgent,
		NewArtifactWriter:              cfg.newArtifactWriter,
		NewCommunicationChannelManager: cfg.newCommunicationChannelManager,
		NewRunID:                       cfg.newRunID,
	})

	if err != nil {
		if exitErr, ok := asExitError(err); ok {
			if !exitErr.Silent() {
				fmt.Fprintln(cfg.stderr, err.Error())
			}
			return exitErr.Code()
		}
		fmt.Fprintln(cfg.stderr, err.Error())
		return 1
	}

	return 0
}

func newRunnerConfig() runnerConfig {
	return runnerConfig{
		stdin:                          os.Stdin,
		stdout:                         os.Stdout,
		stderr:                         os.Stderr,
		getenv:                         os.Getenv,
		validateBackend:                agentpkg.ValidateBackend,
		newAgent:                       newAgentForBackend,
		newArtifactWriter:              newArtifactWriter,
		newCommunicationChannelManager: newWorkflowChannelManager,
		newRunID:                       newRunID,
		run:                            runWorkflow,
		newLock: func() (dirlock.Locker, error) {
			return dirlock.NewInCurrentDirectory()
		},
	}
}

func newAgentForBackend(name string, sessionName string) (workflowAgent, error) {
	switch name {
	case "codex":
		return agentpkg.NewCodexAgent(sessionName), nil
	case "claude":
		return agentpkg.NewClaudeAgent(sessionName), nil
	default:
		return nil, agentpkg.UnknownBackendError(name)
	}
}

type workflowChannelManagerAdapter struct {
	inner    filechannel.ChannelManager
	messages chan channelMessage
	errors   chan error
}

func newWorkflowChannelManager(cfg channelConfig) (channelManager, error) {
	inner, err := filechannel.NewFIFOManager(filechannel.FIFOConfig{Path: cfg.Path})
	if err != nil {
		return nil, err
	}

	manager := &workflowChannelManagerAdapter{
		inner:    inner,
		messages: make(chan channelMessage, 1),
		errors:   make(chan error, 1),
	}
	go manager.forward()
	return manager, nil
}

func (m *workflowChannelManagerAdapter) Messages() <-chan channelMessage {
	return m.messages
}

func (m *workflowChannelManagerAdapter) Errors() <-chan error {
	return m.errors
}

func (m *workflowChannelManagerAdapter) Stop() error {
	return m.inner.Stop()
}

func (m *workflowChannelManagerAdapter) Remove() error {
	return m.inner.Remove()
}

func (m *workflowChannelManagerAdapter) forward() {
	defer close(m.messages)
	defer close(m.errors)

	messages := m.inner.Messages()
	errs := m.inner.Errors()
	for messages != nil || errs != nil {
		select {
		case msg, ok := <-messages:
			if !ok {
				messages = nil
				continue
			}
			m.messages <- channelMessage{
				Path:       msg.Path,
				Body:       msg.Body,
				ReceivedAt: msg.ReceivedAt,
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			m.errors <- adaptChannelError(err)
		}
	}
}

func adaptChannelError(err error) error {
	if err == nil {
		return nil
	}

	var readerErr *filechannel.ReaderError
	if errors.As(err, &readerErr) && readerErr != nil {
		return &channelReaderError{
			Path: readerErr.Path,
			Err:  readerErr.Err,
		}
	}
	return err
}

func resolveLock(cfg runnerConfig) (dirlock.Locker, error) {
	if cfg.lock != nil {
		return cfg.lock, nil
	}
	if cfg.newLock == nil {
		return nil, errors.New("lock is not configured")
	}
	return cfg.newLock()
}

func parsePositiveInt(raw string, source string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %q must be a positive integer", source, raw)
	}
	if value < 1 {
		return 0, fmt.Errorf("invalid %s: %q must be >= 1", source, raw)
	}
	return value, nil
}

func readTask(r io.Reader) (string, error) {
	messageBytes, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read stdin: %w", err)
	}
	task := strings.TrimRight(string(messageBytes), "\r\n")
	if strings.TrimSpace(task) == "" {
		return "", errEmptyTask
	}
	return task, nil
}
