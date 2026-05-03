package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func BuildLaunchCommand(command string, args ...string) (string, error) {
	agentBin, err := resolveAgentBinDir()
	if err != nil {
		return "", err
	}
	return "bash -lc " + shellQuote(buildShellScript(agentBin, command, args...)), nil
}

func resolveAgentBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve ~ for ~/.agent-bin: %w", err)
	}
	agentBin := filepath.Join(home, ".agent-bin")
	absAgentBin, err := filepath.Abs(agentBin)
	if err != nil {
		return "", fmt.Errorf("resolve ~/.agent-bin: %w", err)
	}
	info, err := os.Stat(absAgentBin)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("~/.agent-bin must exist and be a directory: %s", absAgentBin)
		}
		return "", fmt.Errorf("stat ~/.agent-bin: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("~/.agent-bin must be a directory: %s", absAgentBin)
	}
	return absAgentBin, nil
}

func buildShellScript(agentBin, command string, args ...string) string {
	quotedCommand := make([]string, 0, 1+len(args))
	quotedCommand = append(quotedCommand, shellQuote(command))
	for _, arg := range args {
		quotedCommand = append(quotedCommand, shellQuote(arg))
	}
	return `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; ` +
		`export PATH=` + shellQuote(agentBin) + `:"$PATH"; ` +
		`stty -echo; ` + strings.Join(quotedCommand, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
