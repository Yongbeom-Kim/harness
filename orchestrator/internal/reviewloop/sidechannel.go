package reviewloop

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	toReviewerPipePath    = "./to_reviewer.pipe"
	toImplementerPipePath = "./to_implementer.pipe"
)

type sideChannelCoordinator struct {
	sink     ArtifactSink
	sessions map[string]Session

	mu    sync.RWMutex
	ready map[string]bool
}

type channelRoute struct {
	sourceRole      string
	destinationRole string
}

func newSideChannelCoordinator(sink ArtifactSink, sessions map[string]Session) *sideChannelCoordinator {
	return &sideChannelCoordinator{
		sink:     sink,
		sessions: sessions,
		ready:    make(map[string]bool),
	}
}

func (c *sideChannelCoordinator) BuildStartupPrompt(role string, basePrompt string) string {
	instructions := buildSideChannelInstructions(role)
	if instructions == "" {
		return basePrompt
	}
	if strings.TrimSpace(basePrompt) == "" {
		return instructions
	}
	return strings.TrimSpace(basePrompt) + "\n\n" + instructions
}

func (c *sideChannelCoordinator) MarkReady(role string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready[role] = true
}

func (c *sideChannelCoordinator) HandleMessage(msg ChannelMessage) error {
	route, err := routeForChannelPath(msg.Path)
	if err != nil {
		return err
	}

	event := ChannelEvent{
		At:              channelEventTime(msg.ReceivedAt),
		SourceRole:      route.sourceRole,
		DestinationRole: route.destinationRole,
		ChannelPath:     msg.Path,
		RawBody:         msg.Body,
	}

	if strings.TrimSpace(msg.Body) == "" {
		event.Status = ChannelStatusDroppedEmpty
		return c.appendEvent(event)
	}

	if !c.isReady(route.destinationRole) {
		event.Status = ChannelStatusDroppedNotStarted
		return c.appendEvent(event)
	}

	session := c.sessions[route.destinationRole]
	if session == nil {
		return fmt.Errorf("missing side-channel session for role %s", route.destinationRole)
	}

	if err := session.InjectSideChannel(wrapSideChannelMessage(msg.Body)); err != nil {
		event.Status = ChannelStatusDeliveryFailed
		if appendErr := c.appendEvent(event); appendErr != nil {
			return appendErr
		}
		return nil
	}

	event.Status = ChannelStatusDelivered
	return c.appendEvent(event)
}

func (c *sideChannelCoordinator) appendEvent(event ChannelEvent) error {
	if c == nil || c.sink == nil {
		return nil
	}
	return c.sink.AppendChannelEvent(event)
}

func (c *sideChannelCoordinator) isReady(role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready[role]
}

func buildSideChannelInstructions(role string) string {
	channelPath := writableChannelForRole(role)
	if channelPath == "" {
		return ""
	}
	return fmt.Sprintf(
		"Side-channel capability:\n- Fixed FIFO paths in this working directory:\n  - %s\n  - %s\n- To message the other role from this session, write to %s.\n- One side-channel message is one writer open-write-close cycle.\n- Write the full message body, then close the writer.",
		toReviewerPipePath,
		toImplementerPipePath,
		channelPath,
	)
}

func writableChannelForRole(role string) string {
	switch role {
	case RoleImplementer:
		return toReviewerPipePath
	case RoleReviewer:
		return toImplementerPipePath
	default:
		return ""
	}
}

func routeForChannelPath(path string) (channelRoute, error) {
	switch filepath.Base(path) {
	case filepath.Base(toReviewerPipePath):
		return channelRoute{sourceRole: RoleImplementer, destinationRole: RoleReviewer}, nil
	case filepath.Base(toImplementerPipePath):
		return channelRoute{sourceRole: RoleReviewer, destinationRole: RoleImplementer}, nil
	default:
		return channelRoute{}, fmt.Errorf("unknown side-channel path %q", path)
	}
}

func wrapSideChannelMessage(body string) string {
	var builder strings.Builder
	builder.WriteString("<side_channel_message>\n")
	builder.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		builder.WriteString("\n")
	}
	builder.WriteString("</side_channel_message>\n")
	return builder.String()
}

func channelEventTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
