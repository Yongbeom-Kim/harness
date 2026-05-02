package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactWriterPreservesLegacyRunLayout(t *testing.T) {
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	writer, err := newArtifactWriter("run-1")
	if err != nil {
		t.Fatalf("newArtifactWriter: %v", err)
	}
	if err := writer.WriteMetadata(runMetadata{RunID: "run-1", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	if err := writer.AppendTransition(stateTransition{State: "run_started"}); err != nil {
		t.Fatalf("AppendTransition: %v", err)
	}
	if err := writer.AppendChannelEvent(channelEvent{ChannelPath: toReviewerPipePath, Status: channelStatusDroppedEmpty}); err != nil {
		t.Fatalf("AppendChannelEvent: %v", err)
	}
	if err := writer.WriteCapture(successCaptureName(1, roleReviewer), "capture"); err != nil {
		t.Fatalf("WriteCapture: %v", err)
	}
	if err := writer.WriteResult(runResult{RunID: "run-1", Status: resultStatusApproved, CompletedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}

	for _, path := range []string{
		"log/runs/run-1/metadata.json",
		"log/runs/run-1/state-transitions.jsonl",
		"log/runs/run-1/channel-events.jsonl",
		"log/runs/run-1/captures/iter-1-reviewer.txt",
		"log/runs/run-1/result.json",
	} {
		if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}

	data, err := os.ReadFile(filepath.FromSlash("log/runs/run-1/result.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"status": "approved"`) {
		t.Fatalf("result did not preserve JSON schema: %s", string(data))
	}
}
