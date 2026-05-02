package main

import (
	"strings"
	"testing"
)

func TestBuildImplementerPromptIncludesFixedFifoPaths(t *testing.T) {
	contract := buildRoleContract(roleImplementer)
	for _, want := range []string{implementerRolePrompt, toReviewerPipePath, toImplementerPipePath, "write to ./to_reviewer.pipe"} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract missing %q:\n%s", want, contract)
		}
	}
}

func TestPromptBuildersKeepWorkflowShapes(t *testing.T) {
	if got := BuildImplementerPrompt("task"); got != "task" {
		t.Fatalf("unexpected implementer prompt: %q", got)
	}
	reviewer := BuildReviewerPrompt("task", "impl")
	if !strings.Contains(reviewer, "Task given to implementer:\ntask") || !strings.Contains(reviewer, "Implementation:\nimpl") {
		t.Fatalf("unexpected reviewer prompt: %q", reviewer)
	}
	rewrite := BuildRewritePrompt("task", "impl", "review")
	if !strings.Contains(rewrite, "Reviewer feedback:\nreview") || !strings.Contains(rewrite, "Rewrite addressing all feedback.") {
		t.Fatalf("unexpected rewrite prompt: %q", rewrite)
	}
}
