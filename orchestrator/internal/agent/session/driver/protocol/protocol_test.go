package protocol

import (
	"strings"
	"testing"
)

func TestPrepareTurnExtractsAndSanitizesCapture(t *testing.T) {
	turnProtocol := NewPromiseDoneProtocol()
	prepared := turnProtocol.PrepareTurn("Review this draft:\npackage demo\nconst Token = \"v1\"\n<promise>done</promise>\n")

	capture := "> " + prepared.Prompt + "\nUse Token = \"v2\"\n<promise>done</promise>\n"
	rawCapture := turnProtocol.ExtractTurnCapture(capture, prepared)
	output := turnProtocol.SanitizeTurnCapture(rawCapture, prepared)

	if strings.Contains(rawCapture, prepared.Marker) {
		t.Fatalf("marker should be removed from raw capture: %q", rawCapture)
	}
	if !strings.Contains(rawCapture, "Review this draft:") {
		t.Fatalf("raw capture should retain the prompt body: %q", rawCapture)
	}
	if output != "Use Token = \"v2\"\n<promise>done</promise>\n" {
		t.Fatalf("unexpected sanitized output: %q", output)
	}
}

func TestIsTurnCompleteRequiresExactPromiseLine(t *testing.T) {
	turnProtocol := NewPromiseDoneProtocol()
	prepared := turnProtocol.PrepareTurn("task")

	if !turnProtocol.IsTurnComplete("task\n\nFinish your response with exactly <promise>done</promise>.\n\nok\n<promise>done</promise>\n", prepared) {
		t.Fatal("expected final exact completion line to be accepted")
	}
	if turnProtocol.IsTurnComplete("task\n\nFinish your response with exactly <promise>done</promise>.\n\nok\n<promise>done</promise>\nmore output\n", prepared) {
		t.Fatal("expected sentinel before later output to be rejected")
	}
	if turnProtocol.IsTurnComplete("ok <promise>done</promise> trailing", prepared) {
		t.Fatal("expected inline completion marker to be rejected")
	}
}

func TestSanitizeTurnCaptureDoesNotSplitOnQuotedInstructionText(t *testing.T) {
	turnProtocol := NewPromiseDoneProtocol()
	prepared := turnProtocol.PrepareTurn("task")
	rawCapture := prepared.PromptBody + "\nThe literal text is: Finish your response with exactly <promise>done</promise>.\n<promise>done</promise>\n"

	output := turnProtocol.SanitizeTurnCapture(rawCapture, prepared)
	if !strings.Contains(output, "The literal text is: Finish your response with exactly <promise>done</promise>.") {
		t.Fatalf("quoted instruction text should be preserved in output: %q", output)
	}
}
