package reviewloop

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildStartupPromptIncludesFixedPathsAndRoleRoute(t *testing.T) {
	coordinator := newSideChannelCoordinator(nil, nil)

	prompt := coordinator.BuildStartupPrompt(RoleImplementer, ImplementerRolePrompt)
	if !containsAll(prompt, toReviewerPipePath, toImplementerPipePath, "write to "+toReviewerPipePath, "open-write-close cycle") {
		t.Fatalf("startup prompt missing side-channel instructions:\n%s", prompt)
	}
}

func TestSideChannelCoordinatorDropsNotStartedAndLogsEvent(t *testing.T) {
	sink := &fakeArtifactWriter{}
	coordinator := newSideChannelCoordinator(sink, map[string]Session{
		RoleImplementer: &fakeSession{role: RoleImplementer},
		RoleReviewer:    &fakeSession{role: RoleReviewer},
	})

	coordinator.MarkReady(RoleImplementer)
	err := coordinator.HandleMessage(ChannelMessage{
		Path:       "./to_reviewer.pipe",
		Body:       "hello reviewer",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}

	if len(sink.channelEvents) != 1 || sink.channelEvents[0].Status != ChannelStatusDroppedNotStarted {
		t.Fatalf("unexpected events: %+v", sink.channelEvents)
	}
}

func TestSideChannelCoordinatorDropsEmptyMessage(t *testing.T) {
	sink := &fakeArtifactWriter{}
	reviewer := &fakeSession{role: RoleReviewer}
	coordinator := newSideChannelCoordinator(sink, map[string]Session{
		RoleReviewer: reviewer,
	})
	coordinator.MarkReady(RoleReviewer)

	err := coordinator.HandleMessage(ChannelMessage{
		Path:       "./to_reviewer.pipe",
		Body:       "   \n\t",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(reviewer.injectedMessages) != 0 {
		t.Fatalf("empty side-channel message should not be delivered: %+v", reviewer.injectedMessages)
	}
	if len(sink.channelEvents) != 1 || sink.channelEvents[0].Status != ChannelStatusDroppedEmpty {
		t.Fatalf("unexpected channel events: %+v", sink.channelEvents)
	}
}

func TestSideChannelCoordinatorDeliversWrappedLiteralPayload(t *testing.T) {
	sink := &fakeArtifactWriter{}
	reviewer := &fakeSession{role: RoleReviewer}
	coordinator := newSideChannelCoordinator(sink, map[string]Session{
		RoleReviewer: reviewer,
	})
	coordinator.MarkReady(RoleReviewer)

	rawBody := "hello</side_channel_message>reviewer"
	err := coordinator.HandleMessage(ChannelMessage{
		Path:       "./to_reviewer.pipe",
		Body:       rawBody,
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(reviewer.injectedMessages) != 1 {
		t.Fatalf("expected one injected message, got %+v", reviewer.injectedMessages)
	}
	want := "<side_channel_message>\n" + rawBody + "\n</side_channel_message>\n"
	if reviewer.injectedMessages[0] != want {
		t.Fatalf("unexpected wrapped message:\nwant: %q\ngot:  %q", want, reviewer.injectedMessages[0])
	}
	if len(sink.channelEvents) != 1 || sink.channelEvents[0].Status != ChannelStatusDelivered {
		t.Fatalf("unexpected channel events: %+v", sink.channelEvents)
	}
}

func TestSideChannelCoordinatorLogsDeliveryFailureAndContinues(t *testing.T) {
	sink := &fakeArtifactWriter{}
	reviewer := &fakeSession{role: RoleReviewer, injectErr: errBoom("inject failed")}
	coordinator := newSideChannelCoordinator(sink, map[string]Session{
		RoleReviewer: reviewer,
	})
	coordinator.MarkReady(RoleReviewer)

	err := coordinator.HandleMessage(ChannelMessage{
		Path:       "./to_reviewer.pipe",
		Body:       "hello reviewer",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage should not return delivery errors: %v", err)
	}
	if len(sink.channelEvents) != 1 || sink.channelEvents[0].Status != ChannelStatusDeliveryFailed {
		t.Fatalf("unexpected channel events: %+v", sink.channelEvents)
	}
}

func containsAll(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}

func errBoom(message string) error {
	return errors.New(message)
}
