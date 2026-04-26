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

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/cli"
	"github.com/Yongbeom-Kim/harness/orchestrator/internal/implementwithreviewer"
)

const (
	defaultMaxIterations = 10
	defaultIdleTimeout   = 30 * time.Minute
)

var errEmptyTask = errors.New("task from stdin must not be empty")

type runFunc func(context.Context, implementwithreviewer.RunConfig) error

type runnerConfig struct {
	stdin           io.Reader
	stdout          io.Writer
	stderr          io.Writer
	getenv          func(string) string
	validateBackend func(string) error
	run             runFunc
}

type stringFlag struct {
	value string
	set   bool
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
	os.Exit(run(os.Args[1:], runnerConfig{
		stdin:           os.Stdin,
		stdout:          os.Stdout,
		stderr:          os.Stderr,
		getenv:          os.Getenv,
		validateBackend: cli.ValidateBackend,
		run:             implementwithreviewer.Run,
	}))
}

func run(args []string, cfg runnerConfig) int {
	cfg = defaultRunnerConfig(cfg)

	flagSet := flag.NewFlagSet("implement-with-reviewer", flag.ContinueOnError)
	flagSet.SetOutput(cfg.stderr)

	implementer := flagSet.String("implementer", "", "implementer backend")
	reviewer := flagSet.String("reviewer", "", "reviewer backend")
	var maxIterationsFlag stringFlag
	flagSet.Var(&maxIterationsFlag, "max-iterations", "maximum review iterations")

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flagSet.NArg() != 0 {
		fmt.Fprintf(cfg.stderr, "unexpected positional arguments: %s\n", strings.Join(flagSet.Args(), " "))
		return 2
	}

	if *implementer == "" {
		fmt.Fprintln(cfg.stderr, "missing required flag: --implementer")
		return 2
	}
	if *reviewer == "" {
		fmt.Fprintln(cfg.stderr, "missing required flag: --reviewer")
		return 2
	}

	maxIterations, err := resolveMaxIterations(maxIterationsFlag, cfg.getenv)
	if err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 2
	}

	if err := cfg.validateBackend(*implementer); err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 2
	}
	if err := cfg.validateBackend(*reviewer); err != nil {
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

	err = cfg.run(context.Background(), implementwithreviewer.RunConfig{
		Task:              task,
		Implementer:       *implementer,
		Reviewer:          *reviewer,
		MaxIterations:     maxIterations,
		IdleTimeout:       defaultIdleTimeout,
		Stdout:            cfg.stdout,
		Stderr:            cfg.stderr,
		NewSession:        cli.NewSession,
		NewArtifactWriter: implementwithreviewer.NewArtifactWriter,
		NewRunID:          implementwithreviewer.NewRunID,
	})
	if err != nil {
		if exitErr, ok := implementwithreviewer.AsExitError(err); ok {
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

func defaultRunnerConfig(cfg runnerConfig) runnerConfig {
	if cfg.stdin == nil {
		cfg.stdin = os.Stdin
	}
	if cfg.stdout == nil {
		cfg.stdout = os.Stdout
	}
	if cfg.stderr == nil {
		cfg.stderr = os.Stderr
	}
	if cfg.getenv == nil {
		cfg.getenv = os.Getenv
	}
	if cfg.validateBackend == nil {
		cfg.validateBackend = cli.ValidateBackend
	}
	if cfg.run == nil {
		cfg.run = implementwithreviewer.Run
	}
	return cfg
}

func resolveMaxIterations(flagValue stringFlag, getenv func(string) string) (int, error) {
	if flagValue.set {
		return parsePositiveInt(flagValue.value, "--max-iterations")
	}
	if envValue := getenv("MAX_ITERATIONS"); envValue != "" {
		return parsePositiveInt(envValue, "MAX_ITERATIONS")
	}
	return defaultMaxIterations, nil
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
	task := strings.TrimRight(string(messageBytes), "\n")
	if strings.TrimSpace(task) == "" {
		return "", errEmptyTask
	}
	return task, nil
}
