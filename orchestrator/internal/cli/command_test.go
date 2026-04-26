package cli

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunTurnWaitsForDoneAfterPromptBoundary(t *testing.T) {
	session := &persistentSession{
		sessionName: "iwr-test-reviewer",
		idleTimeout: time.Second,
	}

	userPrompt := "Review this draft:\npackage demo\nconst Token = \"v1\"\n<promise>done</promise>\n"
	decoratedPrompt := session.decorateTurnPrompt(userPrompt)
	sentPrompt := ""
	captureCalls := 0
	var commands [][]string

	withRunCommandStub(t, func(name string, args ...string) (commandResult, error) {
		commands = append(commands, append([]string{name}, args...))
		if name != "tmux" {
			t.Fatalf("unexpected command %q", name)
		}

		switch args[0] {
		case "capture-pane":
			if sentPrompt == "" {
				t.Fatal("capture happened before prompt was sent")
			}
			captureCalls++
			capture := "> " + sentPrompt + "\n"
			if captureCalls > 1 {
				capture += "Use Token = \"v2\"\n<promise>done</promise>\n"
			}
			return commandResult{stdout: capture}, nil
		case "paste-buffer", "clear-history":
			return commandResult{}, nil
		case "send-keys":
			return commandResult{}, nil
		default:
			t.Fatalf("unexpected tmux invocation: %v", args)
			return commandResult{}, nil
		}
	})
	withRunCommandWithInputStub(t, func(input string, name string, args ...string) (commandResult, error) {
		commands = append(commands, append([]string{name}, args...))
		if name != "tmux" {
			t.Fatalf("unexpected command %q", name)
		}
		if !strings.HasSuffix(input, "\n"+decoratedPrompt) {
			t.Fatalf("buffer input should prepend metadata and end with the decorated prompt:\nwant suffix: %q\ngot:         %q", "\n"+decoratedPrompt, input)
		}
		if len(args) != 4 || args[0] != "load-buffer" || args[1] != "-b" || args[3] != "-" {
			t.Fatalf("unexpected tmux invocation: %v", args)
		}
		sentPrompt = input
		return commandResult{}, nil
	})

	result, err := session.RunTurn(userPrompt)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	wantOutput := "Use Token = \"v2\"\n<promise>done</promise>\n"
	if result.Output != wantOutput {
		t.Fatalf("unexpected output:\nwant: %q\ngot:  %q", wantOutput, result.Output)
	}

	wantRawCapture := decoratedPrompt + "\nUse Token = \"v2\"\n<promise>done</promise>\n"
	if result.RawCapture != wantRawCapture {
		t.Fatalf("unexpected raw capture:\nwant: %q\ngot:  %q", wantRawCapture, result.RawCapture)
	}

	wantTail := [][]string{
		{"tmux", "send-keys", "-t", session.sessionName, "-R"},
		{"tmux", "clear-history", "-t", session.sessionName},
	}
	gotTail := commands[len(commands)-2:]
	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("unexpected reset commands:\nwant: %v\ngot:  %v", wantTail, gotTail)
	}
}

func TestBuildSourcedLauncherPreservesAgentrcPath(t *testing.T) {
	launcher := buildSourcedLauncher("codex", "--model", "gpt-5")

	if strings.Contains(launcher, "export PATH=") {
		t.Fatalf("launcher should not overwrite PATH: %s", launcher)
	}
	if !strings.Contains(launcher, `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo;`) {
		t.Fatalf("launcher should source .agentrc before invoking the backend command: %s", launcher)
	}
	if strings.Contains(launcher, `; exec `) {
		t.Fatalf("launcher should not use exec because that bypasses shell functions from .agentrc: %s", launcher)
	}
	if !strings.Contains(launcher, `'codex'`) {
		t.Fatalf("launcher should reference backend command: %s", launcher)
	}
}

func TestStartNormalizesResetFailureToStartup(t *testing.T) {
	session := &persistentSession{
		sessionName:        "iwr-test-implementer",
		idleTimeout:        time.Second,
		startupInstruction: "Acknowledge initialization.",
	}

	rolePrompt := "You are the implementer."
	decoratedPrompt := session.decorateStartupPrompt(rolePrompt)
	promptSent := false
	sentPrompt := ""

	withRunCommandStub(t, func(name string, args ...string) (commandResult, error) {
		if name != "tmux" {
			t.Fatalf("unexpected command %q", name)
		}

		switch args[0] {
		case "capture-pane":
			if !promptSent {
				return commandResult{stdout: "> "}, nil
			}
			return commandResult{stdout: "> " + sentPrompt + "\nready\n<promise>done</promise>\n"}, nil
		case "paste-buffer", "clear-history":
			return commandResult{}, nil
		case "send-keys":
			if slices.Contains(args, "-R") {
				return commandResult{}, errors.New("reset failed")
			}
			return commandResult{}, nil
		default:
			t.Fatalf("unexpected tmux invocation: %v", args)
			return commandResult{}, nil
		}
	})
	withRunCommandWithInputStub(t, func(input string, name string, args ...string) (commandResult, error) {
		if name != "tmux" {
			t.Fatalf("unexpected command %q", name)
		}
		if !strings.HasSuffix(input, "\n"+decoratedPrompt) {
			t.Fatalf("buffer input should prepend metadata and end with the decorated prompt:\nwant suffix: %q\ngot:         %q", "\n"+decoratedPrompt, input)
		}
		if len(args) != 4 || args[0] != "load-buffer" || args[1] != "-b" || args[3] != "-" {
			t.Fatalf("unexpected tmux invocation: %v", args)
		}
		promptSent = true
		sentPrompt = input
		return commandResult{}, nil
	})

	err := session.Start(rolePrompt)
	sessionErr, ok := AsSessionError(err)
	if !ok {
		t.Fatalf("expected session error, got %v", err)
	}
	if sessionErr.Kind() != SessionErrorKindStartup {
		t.Fatalf("expected startup error kind, got %q", sessionErr.Kind())
	}

	wantCapture := decoratedPrompt + "\nready\n<promise>done</promise>\n"
	if sessionErr.Capture() != wantCapture {
		t.Fatalf("unexpected startup capture:\nwant: %q\ngot:  %q", wantCapture, sessionErr.Capture())
	}
}

func TestCloseSucceedsWhenSessionAlreadyGone(t *testing.T) {
	tests := []struct {
		name     string
		hasErr   error
		killErr  error
		wantKill bool
	}{
		{
			name:   "has-session no server running",
			hasErr: errors.New("exit status 1: no server running on /tmp/tmux-1000/default"),
		},
		{
			name:     "kill-session server exited unexpectedly",
			wantKill: true,
			killErr:  errors.New("exit status 1: server exited unexpectedly"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &persistentSession{sessionName: "iwr-test-reviewer"}
			var commands [][]string

			withRunCommandStub(t, func(name string, args ...string) (commandResult, error) {
				commands = append(commands, append([]string{name}, args...))
				if name != "tmux" {
					t.Fatalf("unexpected command %q", name)
				}

				switch args[0] {
				case "has-session":
					if tt.hasErr != nil {
						return commandResult{}, tt.hasErr
					}
					return commandResult{}, nil
				case "kill-session":
					if !tt.wantKill {
						t.Fatal("unexpected kill-session call")
					}
					if tt.killErr != nil {
						return commandResult{}, tt.killErr
					}
					return commandResult{}, nil
				default:
					t.Fatalf("unexpected tmux invocation: %v", args)
					return commandResult{}, nil
				}
			})

			if err := session.Close(); err != nil {
				t.Fatalf("Close returned error: %v", err)
			}

			if tt.wantKill && len(commands) != 2 {
				t.Fatalf("expected has-session and kill-session, got %v", commands)
			}
			if !tt.wantKill && len(commands) != 1 {
				t.Fatalf("expected only has-session, got %v", commands)
			}
		})
	}
}

func withRunCommandStub(t *testing.T, stub func(name string, args ...string) (commandResult, error)) {
	t.Helper()

	original := runCommand
	runCommand = stub
	t.Cleanup(func() {
		runCommand = original
	})
}

func withRunCommandWithInputStub(t *testing.T, stub func(input string, name string, args ...string) (commandResult, error)) {
	t.Helper()

	original := runCommandWithInput
	runCommandWithInput = stub
	t.Cleanup(func() {
		runCommandWithInput = original
	})
}
