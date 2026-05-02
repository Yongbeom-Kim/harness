package main

import (
	"strings"
	"testing"
)

type sideChannelFakeAgent struct {
	prompts []string
}

func (a *sideChannelFakeAgent) Start() error          { return nil }
func (a *sideChannelFakeAgent) WaitUntilReady() error { return nil }
func (a *sideChannelFakeAgent) SendPrompt(prompt string) error {
	a.prompts = append(a.prompts, prompt)
	return nil
}
func (a *sideChannelFakeAgent) Capture() (string, error) { return "", nil }
func (a *sideChannelFakeAgent) SessionName() string      { return "session" }
func (a *sideChannelFakeAgent) Close() error             { return nil }

func TestHandleChannelMessageUsesUndecoratedSendPrompt(t *testing.T) {
	reviewer := &sideChannelFakeAgent{}
	coordinator := newSideChannelCoordinator(nil, map[string]workflowAgent{roleReviewer: reviewer})
	coordinator.MarkReady(roleReviewer)

	if err := handleChannelMessage(coordinator, channelMessage{Path: toReviewerPipePath, Body: "hello"}); err != nil {
		t.Fatalf("handleChannelMessage: %v", err)
	}
	if len(reviewer.prompts) != 1 {
		t.Fatalf("expected one side-channel prompt, got %d", len(reviewer.prompts))
	}
	if !strings.Contains(reviewer.prompts[0], "<side_channel_message>") {
		t.Fatalf("message was not wrapped: %q", reviewer.prompts[0])
	}
	if strings.Contains(reviewer.prompts[0], "<promise>done</promise>") || strings.Contains(reviewer.prompts[0], "<iwr:") {
		t.Fatalf("side-channel prompt should not be turn-decorated: %q", reviewer.prompts[0])
	}
}

func TestHandleChannelMessageDropsBeforeReady(t *testing.T) {
	reviewer := &sideChannelFakeAgent{}
	coordinator := newSideChannelCoordinator(nil, map[string]workflowAgent{roleReviewer: reviewer})
	if err := handleChannelMessage(coordinator, channelMessage{Path: toReviewerPipePath, Body: "hello"}); err != nil {
		t.Fatalf("handleChannelMessage: %v", err)
	}
	if len(reviewer.prompts) != 0 {
		t.Fatalf("message should be dropped before ready: %#v", reviewer.prompts)
	}
}
