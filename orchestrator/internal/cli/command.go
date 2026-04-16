package cli

import "os/exec"

func execWithSourcedEnv(command string, args ...string) *exec.Cmd {
	bashArgs := []string{"-lc", `. "$HOME/.agentrc" && "$@"`, "bash", command}
	bashArgs = append(bashArgs, args...)
	return execCommand("bash", bashArgs...)
}
