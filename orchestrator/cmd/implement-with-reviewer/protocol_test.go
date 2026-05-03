package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReviewerPromptIncludesImplementerPipeAndWaitInstruction(t *testing.T) {
	prompt := buildReviewerPrompt("ship it", "/abs/impl.pipe", "implement-with-reviewer-123")
	if !strings.Contains(prompt, markerImplementationReady) {
		t.Fatal("missing implementation-ready marker")
	}
	if !strings.Contains(prompt, "/abs/impl.pipe") {
		t.Fatal("missing implementer pipe path")
	}
	if !strings.Contains(strings.ToLower(prompt), "wait for the implementer") {
		t.Fatal("missing wait instruction")
	}
}

func TestBuildPromptsDescribeApprovalAndBlockedProtocolRules(t *testing.T) {
	implementerPrompt := buildImplementerPrompt("ship it", "/abs/reviewer.pipe", "implement-with-reviewer-123")
	reviewerPrompt := buildReviewerPrompt("ship it", "/abs/impl.pipe", "implement-with-reviewer-123")

	for _, prompt := range []string{implementerPrompt, reviewerPrompt} {
		if !strings.Contains(prompt, markerApproved) || !strings.Contains(strings.ToLower(prompt), "idle") {
			t.Fatalf("prompt missing approval idle rule: %q", prompt)
		}
		if !strings.Contains(prompt, markerBlocked) || !strings.Contains(strings.ToLower(prompt), "ask") {
			t.Fatalf("prompt missing blocked peer-first rule: %q", prompt)
		}
	}
}

func TestGenerateSessionNameUsesWorkflowPrefixAndTmuxSafeSuffix(t *testing.T) {
	name := generateSessionName(time.Date(2026, time.May, 3, 12, 34, 56, 789, time.UTC))
	if !strings.HasPrefix(name, workflowSessionPrefix) {
		t.Fatalf("prefix = %q, want %q", name, workflowSessionPrefix)
	}
	if len(name) <= len(workflowSessionPrefix) {
		t.Fatalf("suffix should not be empty: %q", name)
	}
	if strings.ContainsAny(name, " :/\t\n") {
		t.Fatalf("session name must be tmux-safe: %q", name)
	}
}
