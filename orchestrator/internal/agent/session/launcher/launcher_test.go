package launcher

import (
	"strings"
	"testing"
)

func TestSourcedShellBuilderPreservesAgentrcPath(t *testing.T) {
	launcher := NewSourcedShellBuilder()
	command := launcher.Build("codex", "--model", "gpt-5")

	if strings.Contains(command, "export PATH=") {
		t.Fatalf("launcher should not overwrite PATH: %s", command)
	}
	if !strings.Contains(command, `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo;`) {
		t.Fatalf("launcher should source .agentrc before invoking the backend command: %s", command)
	}
	if strings.Contains(command, `; exec `) {
		t.Fatalf("launcher should not use exec because that bypasses shell functions from .agentrc: %s", command)
	}
	if !strings.Contains(command, `'codex'`) {
		t.Fatalf("launcher should reference backend command: %s", command)
	}
}
