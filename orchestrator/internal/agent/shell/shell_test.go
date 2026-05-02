package shell

import (
	"strings"
	"testing"
)

func TestBuildLaunchCommandSourcesAgentrcAndQuotesArgs(t *testing.T) {
	got := BuildLaunchCommand("codex", "one two", "it's")
	if !strings.Contains(got, `. "$HOME/.agentrc"`) {
		t.Fatalf("launch command should source agentrc: %q", got)
	}
	if !strings.Contains(got, "one two") || !strings.Contains(got, `"'"'`) {
		t.Fatalf("launch command did not quote args: %q", got)
	}
}
