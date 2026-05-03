package backend

import (
	"errors"
	"testing"
	"time"
)

type recordingPane struct {
	sent        []string
	captures    []string
	captureErr  error
	captureCall int
}

func (p *recordingPane) SendText(text string) error {
	p.sent = append(p.sent, text)
	return nil
}

func (p *recordingPane) Capture() (string, error) {
	if p.captureErr != nil {
		return "", p.captureErr
	}
	if len(p.captures) == 0 {
		return "", nil
	}
	if p.captureCall >= len(p.captures) {
		return p.captures[len(p.captures)-1], nil
	}
	got := p.captures[p.captureCall]
	p.captureCall++
	return got, nil
}

func (p *recordingPane) Close() error {
	return nil
}

func TestCodexDefaultsLaunchPromptAndReadyMatcher(t *testing.T) {
	var b Backend = Codex{}
	if got := b.DefaultSessionName(); got != "codex" {
		t.Fatalf("DefaultSessionName() = %q, want codex", got)
	}
	pane := &recordingPane{}
	err := b.Launch(pane, func(command string, args ...string) (string, error) {
		if command != "codex" || len(args) != 0 {
			t.Fatalf("build command = %q %v, want codex []", command, args)
		}
		return "launch codex", nil
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if err := b.SendPrompt(pane, "hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got := pane.sent; len(got) != 2 || got[0] != "launch codex" || got[1] != "hello" {
		t.Fatalf("pane sent = %v, want launch then prompt", got)
	}
	now := time.Unix(0, 0)
	pane.captures = []string{"OpenAI Codex\n› ", "OpenAI Codex\n› "}
	if err := b.WaitUntilReady(pane, testReadinessOptions(&now)); err != nil {
		t.Fatalf("WaitUntilReady() error = %v", err)
	}

	now = time.Unix(0, 0)
	pane.captures = []string{"Sign in with ChatGPT\n› "}
	pane.captureCall = 0
	err = b.WaitUntilReady(pane, shortReadinessOptions(&now))
	if err == nil {
		t.Fatal("expected signin prompt to be not ready")
	}
}

func TestClaudeDefaultsLaunchPromptAndReadyMatcher(t *testing.T) {
	var b Backend = Claude{}
	if got := b.DefaultSessionName(); got != "claude" {
		t.Fatalf("DefaultSessionName() = %q, want claude", got)
	}
	pane := &recordingPane{}
	err := b.Launch(pane, func(command string, args ...string) (string, error) {
		if command != "claude" || len(args) != 0 {
			t.Fatalf("build command = %q %v, want claude []", command, args)
		}
		return "launch claude", nil
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if err := b.SendPrompt(pane, "hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got := pane.sent; len(got) != 2 || got[0] != "launch claude" || got[1] != "hello" {
		t.Fatalf("pane sent = %v, want launch then prompt", got)
	}
	now := time.Unix(0, 0)
	pane.captures = []string{"Claude is ready", "Claude is ready"}
	if err := b.WaitUntilReady(pane, testReadinessOptions(&now)); err != nil {
		t.Fatalf("WaitUntilReady() error = %v", err)
	}

	now = time.Unix(0, 0)
	pane.captures = []string{"Do you trust the files in this folder?"}
	pane.captureCall = 0
	err = b.WaitUntilReady(pane, shortReadinessOptions(&now))
	if err == nil {
		t.Fatal("expected trust prompt to be not ready")
	}
}

func TestCursorDefaultsLaunchPromptAndReadyMatcher(t *testing.T) {
	var b Backend = Cursor{}
	if got := b.DefaultSessionName(); got != "cursor" {
		t.Fatalf("DefaultSessionName() = %q, want cursor", got)
	}
	pane := &recordingPane{}
	err := b.Launch(pane, func(command string, args ...string) (string, error) {
		if command != "agent" || len(args) != 0 {
			t.Fatalf("build command = %q %v, want agent []", command, args)
		}
		return "launch agent", nil
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if err := b.SendPrompt(pane, "hello"); err != nil {
		t.Fatalf("SendPrompt() error = %v", err)
	}
	if got := pane.sent; len(got) != 2 || got[0] != "launch agent" || got[1] != "hello" {
		t.Fatalf("pane sent = %v, want launch then prompt", got)
	}

	now := time.Unix(0, 0)
	pane.captures = []string{"Cursor Agent", "Cursor Agent"}
	if err := b.WaitUntilReady(pane, testReadinessOptions(&now)); err != nil {
		t.Fatalf("WaitUntilReady() error = %v", err)
	}
}

func TestCursorReadyRejectsRepresentativeInterstitials(t *testing.T) {
	cases := []string{
		"Log in to continue",
		"Sign in",
		"Authentication required",
		"Do you trust this folder?",
		"Setup your environment",
		"Press Enter to continue",
	}
	for _, capture := range cases {
		if cursorReady(capture) {
			t.Fatalf("cursorReady(%q) = true, want false", capture)
		}
	}
}

func TestCursorInterstitialsStayNotReadyUntilTimeout(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &recordingPane{captures: []string{"Sign in to Cursor"}}

	err := Cursor{}.WaitUntilReady(pane, shortReadinessOptions(&now))
	if err == nil {
		t.Fatal("expected timeout")
	}
	var readinessErr *ReadinessError
	if !errors.As(err, &readinessErr) || readinessErr.Capture != "Sign in to Cursor" {
		t.Fatalf("error = %#v, want ReadinessError with latest interstitial capture", err)
	}
}

func TestWaitUntilReadyReturnsLatestCaptureOnTimeout(t *testing.T) {
	now := time.Unix(0, 0)
	pane := &recordingPane{captures: []string{"boot"}}

	err := Codex{}.WaitUntilReady(pane, shortReadinessOptions(&now))
	if err == nil {
		t.Fatal("expected timeout")
	}
	var readinessErr *ReadinessError
	if !errors.As(err, &readinessErr) || readinessErr.Capture != "boot" {
		t.Fatalf("error = %#v, want ReadinessError with latest capture", err)
	}
}

func testReadinessOptions(now *time.Time) ReadinessOptions {
	return ReadinessOptions{
		ReadyTimeout: time.Second,
		QuietPeriod:  10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		Now:          func() time.Time { return *now },
		Sleep:        func(d time.Duration) { *now = now.Add(d) },
	}
}

func shortReadinessOptions(now *time.Time) ReadinessOptions {
	opts := testReadinessOptions(now)
	opts.ReadyTimeout = 20 * time.Millisecond
	return opts
}
