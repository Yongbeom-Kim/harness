package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/cli"
)

const (
	approvedMarker          = "<promise>APPROVED</promise>"
	defaultMaxIterations    = 10
	implementerSystemPrompt = "You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file."
	reviewerSystemPrompt    = "You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with exactly: <promise>APPROVED</promise> - nothing else. Otherwise respond with specific, actionable feedback only. No praise, no filler."
)

type toolFactory func(name string) (cli.CliTool, error)

var errEmptyTask = errors.New("task from stdin must not be empty")

type runnerConfig struct {
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	getenv  func(string) string
	factory toolFactory
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
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		getenv: os.Getenv,
		factory: func(name string) (cli.CliTool, error) {
			return newCliTool(name)
		},
	}))
}

func run(args []string, cfg runnerConfig) int {
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

	implementerTool, err := cfg.factory(*implementer)
	if err != nil {
		fmt.Fprintln(cfg.stderr, err.Error())
		return 2
	}
	reviewerTool, err := cfg.factory(*reviewer)
	if err != nil {
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

	fmt.Fprintf(cfg.stdout, "Implementer : %s\n", *implementer)
	fmt.Fprintf(cfg.stdout, "Reviewer    : %s\n", *reviewer)
	fmt.Fprintf(cfg.stdout, "Task        : %s\n", task)

	implementation, ok := invokeAgent(cfg, bannerTitle(0, "IMPLEMENTER", *implementer), implementerTool, implementerSystemPrompt, task)
	if !ok {
		return 1
	}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		reviewPrompt := fmt.Sprintf("Task given to implementer:\n%s\n\nImplementation:\n%s", task, implementation)
		review, approved, ok := reviewIteration(cfg, iteration, *reviewer, reviewerTool, reviewPrompt)
		if !ok {
			return 1
		}
		if approved {
			fmt.Fprintln(cfg.stdout)
			fmt.Fprintf(cfg.stdout, "Approved after %d review round(s).\n", iteration)
			fmt.Fprintln(cfg.stdout)
			fmt.Fprintln(cfg.stdout, "Final implementation")
			fmt.Fprintln(cfg.stdout, implementation)
			return 0
		}

		rewritePrompt := fmt.Sprintf("Original task:\n%s\n\nYour previous implementation:\n%s\n\nReviewer feedback:\n%s\n\nRewrite addressing all feedback.", task, implementation, review)
		implementation, ok = invokeAgent(cfg, bannerTitle(iteration, "IMPLEMENTER", *implementer), implementerTool, implementerSystemPrompt, rewritePrompt)
		if !ok {
			return 1
		}
	}

	fmt.Fprintln(cfg.stdout)
	fmt.Fprintf(cfg.stdout, "Did not converge after %d iterations.\n", maxIterations)
	return 1
}

func reviewIteration(cfg runnerConfig, iteration int, reviewerName string, reviewerTool cli.CliTool, prompt string) (review string, approved bool, ok bool) {
	review, ok = invokeAgent(cfg, bannerTitle(iteration, "REVIEWER", reviewerName), reviewerTool, reviewerSystemPrompt, prompt)
	if !ok {
		return "", false, false
	}
	return review, strings.Contains(review, approvedMarker), true
}

func invokeAgent(cfg runnerConfig, banner string, tool cli.CliTool, systemPrompt string, prompt string) (string, bool) {
	printBanner(cfg.stdout, banner)
	stdout, stderr, err := tool.SendMessage(cli.CliToolSendMessageOptions{
		Message: fmt.Sprintf("%s\n\n%s", systemPrompt, prompt),
	})
	if stdout != "" {
		fmt.Fprint(cfg.stdout, stdout)
		if !strings.HasSuffix(stdout, "\n") {
			fmt.Fprintln(cfg.stdout)
		}
	}
	if stderr != "" {
		fmt.Fprint(cfg.stderr, stderr)
		if !strings.HasSuffix(stderr, "\n") {
			fmt.Fprintln(cfg.stderr)
		}
	}
	if err != nil {
		fmt.Fprintf(cfg.stderr, "agent invocation failed: %v\n", err)
		return "", false
	}
	return stdout, true
}

func printBanner(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "--- %s ---\n", title)
}

func bannerTitle(iteration int, role string, backend string) string {
	return fmt.Sprintf("iter %d - %s (%s)", iteration, role, backend)
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

func newCliTool(name string) (cli.CliTool, error) {
	switch name {
	case "codex":
		return cli.NewCodexCliTool(nil), nil
	case "claude":
		return cli.NewClaudeCliTool(nil), nil
	default:
		return nil, fmt.Errorf("unknown backend: %s (expected codex or claude)", name)
	}
}
