package main

import (
	"strings"
	"testing"
)

func TestExecuteTurnSlicesCaptureFromMarker(t *testing.T) {
	capture := "old turn\n<iwr:active>\nprompt echo\nanswer\n<promise>done</promise>\nside noise"
	got := extractTurnCapture(capture, "<iwr:active>")
	if strings.Contains(got, "old turn") {
		t.Fatalf("capture was not sliced from marker: %q", got)
	}
	if !strings.Contains(got, "answer") {
		t.Fatalf("capture missing active turn: %q", got)
	}
}

func TestDecorateTurnPromptIncludesRoleContractAndDoneInstruction(t *testing.T) {
	got := decorateTurnPrompt("role contract", "body", "<marker>")
	for _, want := range []string{"<marker>", "role contract", "body", completionInstruction} {
		if !strings.Contains(got, want) {
			t.Fatalf("decorated prompt missing %q:\n%s", want, got)
		}
	}
}

func TestSanitizeTurnCaptureRemovesDoneMarker(t *testing.T) {
	turn := preparedTurn{PromptBody: "prompt"}
	got := sanitizeTurnCapture("prompt\nanswer\n<promise>done</promise>\n", turn)
	if got != "answer\n" {
		t.Fatalf("unexpected sanitized capture: %q", got)
	}
}

func TestIsApprovedDetectsPromiseMarker(t *testing.T) {
	if !isApproved("looks good <promise>APPROVED</promise>") {
		t.Fatal("expected approval marker to be detected")
	}
}
