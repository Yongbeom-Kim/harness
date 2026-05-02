package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildLaunchCommandPrependsResolvedAgentBinAfterAgentrc(t *testing.T) {
	agentBin := setupAgentBin(t)

	resolved, err := resolveAgentBinDir()
	if err != nil {
		t.Fatalf("resolveAgentBinDir: %v", err)
	}
	got := buildShellScript(resolved, "codex")

	agentrcIndex := strings.Index(got, `. "$HOME/.agentrc"`)
	pathIndex := strings.Index(got, `export PATH='`+agentBin+`':"$PATH"`)
	sttyIndex := strings.Index(got, `stty -echo`)
	commandIndex := strings.Index(got, `'codex'`)
	if agentrcIndex < 0 || pathIndex < 0 || sttyIndex < 0 || commandIndex < 0 {
		t.Fatalf("launch command missing required parts: %q", got)
	}
	if !(agentrcIndex < pathIndex && pathIndex < sttyIndex && sttyIndex < commandIndex) {
		t.Fatalf("launch command parts are out of order: %q", got)
	}
}

func TestBuildLaunchCommandUsesAbsoluteAgentBinPath(t *testing.T) {
	agentBin := setupAgentBin(t)

	got, err := BuildLaunchCommand("codex")
	if err != nil {
		t.Fatalf("BuildLaunchCommand: %v", err)
	}

	if !filepath.IsAbs(agentBin) {
		t.Fatalf("test setup produced non-absolute agent bin: %q", agentBin)
	}
	if !strings.Contains(got, agentBin) {
		t.Fatalf("launch command should include absolute agent bin %q: %q", agentBin, got)
	}
	if strings.Contains(got, "~/.agent-bin") {
		t.Fatalf("launch command should not emit literal ~/.agent-bin: %q", got)
	}
}

func TestBuildLaunchCommandQuotesCommandAndArgs(t *testing.T) {
	agentBin := setupAgentBin(t)

	resolved, err := resolveAgentBinDir()
	if err != nil {
		t.Fatalf("resolveAgentBinDir: %v", err)
	}
	got := buildShellScript(resolved, "codex", "one two", "it's")

	for _, want := range []string{"'codex'", "'one two'", `'it'"'"'s'`} {
		if !strings.Contains(got, want) {
			t.Fatalf("launch command missing quoted fragment %q: %q", want, got)
		}
	}
	if !strings.Contains(got, agentBin) {
		t.Fatalf("launch command should include agent bin %q: %q", agentBin, got)
	}
}

func TestBuildLaunchCommandAllowsCustomDirectoryWithoutInspectingContents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := mkdir(filepath.Join(home, ".agent-bin")); err != nil {
		t.Fatalf("mkdir agent bin: %v", err)
	}

	if _, err := BuildLaunchCommand("codex"); err != nil {
		t.Fatalf("BuildLaunchCommand should accept any directory: %v", err)
	}
}

func TestBuildLaunchCommandFailsWhenAgentBinMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := BuildLaunchCommand("codex")
	if err == nil {
		t.Fatal("expected missing ~/.agent-bin to fail")
	}
	if !strings.Contains(err.Error(), "~/.agent-bin") {
		t.Fatalf("error should name ~/.agent-bin: %v", err)
	}
}

func TestBuildLaunchCommandFailsWhenAgentBinIsNotDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := writeFile(filepath.Join(home, ".agent-bin"), "not a directory"); err != nil {
		t.Fatalf("write agent bin file: %v", err)
	}

	_, err := BuildLaunchCommand("codex")
	if err == nil {
		t.Fatal("expected non-directory ~/.agent-bin to fail")
	}
	if !strings.Contains(err.Error(), "~/.agent-bin") {
		t.Fatalf("error should name ~/.agent-bin: %v", err)
	}
}

func setupAgentBin(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentBin := filepath.Join(home, ".agent-bin")
	if err := mkdir(agentBin); err != nil {
		t.Fatalf("mkdir agent bin: %v", err)
	}
	return agentBin
}

func mkdir(path string) error {
	return os.Mkdir(path, 0o755)
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
